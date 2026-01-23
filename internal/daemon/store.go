package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides SQLite-backed persistence for the daemon
type Store struct {
	db *sql.DB
}

// TaskRecord represents a task in the database
type TaskRecord struct {
	TaskID      string
	Description string
	ScopePaths  []string
	Status      string
	AssignedTo  string
	Result      string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// GitHub issue tracking
	GitHubOwner          string
	GitHubRepo           string
	GitHubIssueNumber    int
	LastCommentID        string
	WaitingInputQuestion string
	WaitingInputSince    time.Time
}

// EventRecord represents an event in the database
type EventRecord struct {
	EventID   string
	Type      string
	Payload   string
	CreatedAt time.Time
}

// SpawnedAgentRecord represents a spawned agent in the database
type SpawnedAgentRecord struct {
	AgentID      string
	WorktreePath string
	PID          int
	Branch       string
	Prompt       string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
	task_id TEXT PRIMARY KEY,
	description TEXT NOT NULL,
	scope_paths TEXT,
	status TEXT DEFAULT 'pending',
	assigned_to TEXT,
	result TEXT,
	error TEXT,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	github_owner TEXT,
	github_repo TEXT,
	github_issue_number INTEGER,
	last_comment_id TEXT,
	waiting_input_question TEXT,
	waiting_input_since INTEGER
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assigned_to ON tasks(assigned_to);
CREATE INDEX IF NOT EXISTS idx_tasks_github ON tasks(github_owner, github_repo, github_issue_number);

CREATE TABLE IF NOT EXISTS events (
	event_id TEXT PRIMARY KEY,
	type TEXT NOT NULL,
	payload TEXT,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);

CREATE TABLE IF NOT EXISTS spawned_agents (
	agent_id TEXT PRIMARY KEY,
	worktree_path TEXT,
	pid INTEGER,
	branch TEXT,
	prompt TEXT,
	status TEXT DEFAULT 'running',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_spawned_agents_status ON spawned_agents(status);
`

// NewStore creates a new SQLite store
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "mapd.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	// Initialize schema
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Run migrations for existing databases
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate adds new columns to existing databases
func (s *Store) migrate() error {
	migrations := []string{
		"ALTER TABLE tasks ADD COLUMN github_owner TEXT",
		"ALTER TABLE tasks ADD COLUMN github_repo TEXT",
		"ALTER TABLE tasks ADD COLUMN github_issue_number INTEGER",
		"ALTER TABLE tasks ADD COLUMN last_comment_id TEXT",
		"ALTER TABLE tasks ADD COLUMN waiting_input_question TEXT",
		"ALTER TABLE tasks ADD COLUMN waiting_input_since INTEGER",
	}

	for _, m := range migrations {
		// Ignore errors - column may already exist
		_, _ = s.db.Exec(m)
	}

	// Ensure index exists
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_tasks_github ON tasks(github_owner, github_repo, github_issue_number)")

	return nil
}

// --- Task Operations ---

// CreateTask creates a new task
func (s *Store) CreateTask(task *TaskRecord) error {
	paths, err := json.Marshal(task.ScopePaths)
	if err != nil {
		return fmt.Errorf("marshal scope paths: %w", err)
	}

	var waitingInputSince int64
	if !task.WaitingInputSince.IsZero() {
		waitingInputSince = task.WaitingInputSince.Unix()
	}

	_, err = s.db.Exec(`
		INSERT INTO tasks (task_id, description, scope_paths, status, assigned_to, result, error, created_at, updated_at,
			github_owner, github_repo, github_issue_number, last_comment_id, waiting_input_question, waiting_input_since)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.TaskID, task.Description, string(paths), task.Status, task.AssignedTo,
		task.Result, task.Error, task.CreatedAt.Unix(), task.UpdatedAt.Unix(),
		task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, task.LastCommentID,
		task.WaitingInputQuestion, waitingInputSince)

	return err
}

// GetTask retrieves a task by ID
func (s *Store) GetTask(taskID string) (*TaskRecord, error) {
	row := s.db.QueryRow(`
		SELECT task_id, description, scope_paths, status, assigned_to, result, error, created_at, updated_at,
			github_owner, github_repo, github_issue_number, last_comment_id, waiting_input_question, waiting_input_since
		FROM tasks WHERE task_id = ?
	`, taskID)

	return s.scanTask(row)
}

// ListTasks retrieves tasks with optional filters
func (s *Store) ListTasks(statusFilter, agentFilter string, limit int) ([]*TaskRecord, error) {
	query := `SELECT task_id, description, scope_paths, status, assigned_to, result, error, created_at, updated_at,
		github_owner, github_repo, github_issue_number, last_comment_id, waiting_input_question, waiting_input_since
		FROM tasks WHERE 1=1`
	args := []any{}

	if statusFilter != "" {
		query += " AND status = ?"
		args = append(args, statusFilter)
	}
	if agentFilter != "" {
		query += " AND assigned_to = ?"
		args = append(args, agentFilter)
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []*TaskRecord
	for rows.Next() {
		task, err := s.scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	return tasks, rows.Err()
}

// UpdateTask updates a task
func (s *Store) UpdateTask(task *TaskRecord) error {
	paths, err := json.Marshal(task.ScopePaths)
	if err != nil {
		return fmt.Errorf("marshal scope paths: %w", err)
	}

	var waitingInputSince int64
	if !task.WaitingInputSince.IsZero() {
		waitingInputSince = task.WaitingInputSince.Unix()
	}

	_, err = s.db.Exec(`
		UPDATE tasks SET description = ?, scope_paths = ?, status = ?, assigned_to = ?,
			result = ?, error = ?, updated_at = ?,
			github_owner = ?, github_repo = ?, github_issue_number = ?, last_comment_id = ?,
			waiting_input_question = ?, waiting_input_since = ?
		WHERE task_id = ?
	`, task.Description, string(paths), task.Status, task.AssignedTo,
		task.Result, task.Error, task.UpdatedAt.Unix(),
		task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, task.LastCommentID,
		task.WaitingInputQuestion, waitingInputSince, task.TaskID)

	return err
}

// UpdateTaskStatus updates a task's status
func (s *Store) UpdateTaskStatus(taskID, status string) error {
	_, err := s.db.Exec(`
		UPDATE tasks SET status = ?, updated_at = ? WHERE task_id = ?
	`, status, time.Now().Unix(), taskID)
	return err
}

// AssignTask assigns a task to an agent
func (s *Store) AssignTask(taskID, instanceID string) error {
	_, err := s.db.Exec(`
		UPDATE tasks SET assigned_to = ?, status = 'accepted', updated_at = ? WHERE task_id = ?
	`, instanceID, time.Now().Unix(), taskID)
	return err
}

// ListTasksWaitingInput returns tasks with status=waiting_input that have GitHub sources
func (s *Store) ListTasksWaitingInput() ([]*TaskRecord, error) {
	rows, err := s.db.Query(`
		SELECT task_id, description, scope_paths, status, assigned_to, result, error, created_at, updated_at,
			github_owner, github_repo, github_issue_number, last_comment_id, waiting_input_question, waiting_input_since
		FROM tasks
		WHERE status = 'waiting_input' AND github_owner != '' AND github_repo != '' AND github_issue_number > 0
		ORDER BY waiting_input_since ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []*TaskRecord
	for rows.Next() {
		task, err := s.scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// SetTaskWaitingInput updates a task to waiting_input status with the question
func (s *Store) SetTaskWaitingInput(taskID, question string) error {
	now := time.Now()
	_, err := s.db.Exec(`
		UPDATE tasks SET status = 'waiting_input', waiting_input_question = ?, waiting_input_since = ?, updated_at = ?
		WHERE task_id = ?
	`, question, now.Unix(), now.Unix(), taskID)
	return err
}

// ClearTaskWaitingInput clears the waiting input state and returns task to in_progress
func (s *Store) ClearTaskWaitingInput(taskID, lastCommentID string) error {
	now := time.Now()
	_, err := s.db.Exec(`
		UPDATE tasks SET status = 'in_progress', waiting_input_question = '', waiting_input_since = 0,
			last_comment_id = ?, updated_at = ?
		WHERE task_id = ?
	`, lastCommentID, now.Unix(), taskID)
	return err
}

// GetTaskByAgentID finds the in_progress or waiting_input task assigned to an agent
func (s *Store) GetTaskByAgentID(agentID string) (*TaskRecord, error) {
	row := s.db.QueryRow(`
		SELECT task_id, description, scope_paths, status, assigned_to, result, error, created_at, updated_at,
			github_owner, github_repo, github_issue_number, last_comment_id, waiting_input_question, waiting_input_since
		FROM tasks
		WHERE assigned_to = ? AND status IN ('in_progress', 'waiting_input')
		ORDER BY updated_at DESC LIMIT 1
	`, agentID)
	return s.scanTask(row)
}

// GetAgentByWorktreePath finds the agent assigned to a worktree path
func (s *Store) GetAgentByWorktreePath(worktreePath string) (*SpawnedAgentRecord, error) {
	row := s.db.QueryRow(`
		SELECT agent_id, worktree_path, pid, branch, prompt, status, created_at, updated_at
		FROM spawned_agents WHERE worktree_path = ?
	`, worktreePath)
	return s.scanSpawnedAgent(row)
}

func (s *Store) scanTask(row *sql.Row) (*TaskRecord, error) {
	var task TaskRecord
	var pathsJSON string
	var assignedTo, result, taskError sql.NullString
	var githubOwner, githubRepo, lastCommentID, waitingInputQuestion sql.NullString
	var githubIssueNumber sql.NullInt64
	var createdAt, updatedAt, waitingInputSince int64

	err := row.Scan(&task.TaskID, &task.Description, &pathsJSON, &task.Status,
		&assignedTo, &result, &taskError, &createdAt, &updatedAt,
		&githubOwner, &githubRepo, &githubIssueNumber, &lastCommentID,
		&waitingInputQuestion, &waitingInputSince)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal([]byte(pathsJSON), &task.ScopePaths); err != nil {
		task.ScopePaths = []string{}
	}
	task.AssignedTo = assignedTo.String
	task.Result = result.String
	task.Error = taskError.String
	task.CreatedAt = time.Unix(createdAt, 0)
	task.UpdatedAt = time.Unix(updatedAt, 0)
	task.GitHubOwner = githubOwner.String
	task.GitHubRepo = githubRepo.String
	task.GitHubIssueNumber = int(githubIssueNumber.Int64)
	task.LastCommentID = lastCommentID.String
	task.WaitingInputQuestion = waitingInputQuestion.String
	if waitingInputSince > 0 {
		task.WaitingInputSince = time.Unix(waitingInputSince, 0)
	}

	return &task, nil
}

func (s *Store) scanTaskRow(rows *sql.Rows) (*TaskRecord, error) {
	var task TaskRecord
	var pathsJSON string
	var assignedTo, result, taskError sql.NullString
	var githubOwner, githubRepo, lastCommentID, waitingInputQuestion sql.NullString
	var githubIssueNumber sql.NullInt64
	var createdAt, updatedAt, waitingInputSince int64

	err := rows.Scan(&task.TaskID, &task.Description, &pathsJSON, &task.Status,
		&assignedTo, &result, &taskError, &createdAt, &updatedAt,
		&githubOwner, &githubRepo, &githubIssueNumber, &lastCommentID,
		&waitingInputQuestion, &waitingInputSince)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(pathsJSON), &task.ScopePaths); err != nil {
		task.ScopePaths = []string{}
	}
	task.AssignedTo = assignedTo.String
	task.Result = result.String
	task.Error = taskError.String
	task.CreatedAt = time.Unix(createdAt, 0)
	task.UpdatedAt = time.Unix(updatedAt, 0)
	task.GitHubOwner = githubOwner.String
	task.GitHubRepo = githubRepo.String
	task.GitHubIssueNumber = int(githubIssueNumber.Int64)
	task.LastCommentID = lastCommentID.String
	task.WaitingInputQuestion = waitingInputQuestion.String
	if waitingInputSince > 0 {
		task.WaitingInputSince = time.Unix(waitingInputSince, 0)
	}

	return &task, nil
}

// --- Event Operations ---

// CreateEvent stores a new event
func (s *Store) CreateEvent(event *EventRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO events (event_id, type, payload, created_at)
		VALUES (?, ?, ?, ?)
	`, event.EventID, event.Type, event.Payload, event.CreatedAt.Unix())
	return err
}

// ListRecentEvents retrieves recent events
func (s *Store) ListRecentEvents(limit int) ([]*EventRecord, error) {
	rows, err := s.db.Query(`
		SELECT event_id, type, payload, created_at
		FROM events ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []*EventRecord
	for rows.Next() {
		var event EventRecord
		var createdAt int64
		if err := rows.Scan(&event.EventID, &event.Type, &event.Payload, &createdAt); err != nil {
			return nil, err
		}
		event.CreatedAt = time.Unix(createdAt, 0)
		events = append(events, &event)
	}

	return events, rows.Err()
}

// --- Stats ---

// GetStats returns aggregate statistics
func (s *Store) GetStats() (pendingTasks, activeTasks int, err error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status = 'pending'`)
	if err = row.Scan(&pendingTasks); err != nil {
		return
	}

	row = s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status IN ('accepted', 'in_progress')`)
	err = row.Scan(&activeTasks)
	return
}

// --- Spawned Agent Operations ---

// CreateSpawnedAgent creates a new spawned agent record
func (s *Store) CreateSpawnedAgent(agent *SpawnedAgentRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO spawned_agents (agent_id, worktree_path, pid, branch, prompt, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, agent.AgentID, agent.WorktreePath, agent.PID, agent.Branch, agent.Prompt, agent.Status,
		agent.CreatedAt.Unix(), agent.UpdatedAt.Unix())
	return err
}

// GetSpawnedAgent retrieves a spawned agent by ID
func (s *Store) GetSpawnedAgent(agentID string) (*SpawnedAgentRecord, error) {
	row := s.db.QueryRow(`
		SELECT agent_id, worktree_path, pid, branch, prompt, status, created_at, updated_at
		FROM spawned_agents WHERE agent_id = ?
	`, agentID)

	return s.scanSpawnedAgent(row)
}

// ListSpawnedAgents retrieves all spawned agents, optionally filtered by status
func (s *Store) ListSpawnedAgents(statusFilter string) ([]*SpawnedAgentRecord, error) {
	var rows *sql.Rows
	var err error

	if statusFilter != "" {
		rows, err = s.db.Query(`
			SELECT agent_id, worktree_path, pid, branch, prompt, status, created_at, updated_at
			FROM spawned_agents WHERE status = ? ORDER BY created_at DESC
		`, statusFilter)
	} else {
		rows, err = s.db.Query(`
			SELECT agent_id, worktree_path, pid, branch, prompt, status, created_at, updated_at
			FROM spawned_agents ORDER BY created_at DESC
		`)
	}

	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var agents []*SpawnedAgentRecord
	for rows.Next() {
		agent, err := s.scanSpawnedAgentRow(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}

	return agents, rows.Err()
}

// UpdateSpawnedAgentStatus updates a spawned agent's status
func (s *Store) UpdateSpawnedAgentStatus(agentID, status string) error {
	_, err := s.db.Exec(`
		UPDATE spawned_agents SET status = ?, updated_at = ? WHERE agent_id = ?
	`, status, time.Now().Unix(), agentID)
	return err
}

// DeleteSpawnedAgent removes a spawned agent record
func (s *Store) DeleteSpawnedAgent(agentID string) error {
	_, err := s.db.Exec(`DELETE FROM spawned_agents WHERE agent_id = ?`, agentID)
	return err
}

func (s *Store) scanSpawnedAgent(row *sql.Row) (*SpawnedAgentRecord, error) {
	var agent SpawnedAgentRecord
	var worktreePath, branch, prompt sql.NullString
	var createdAt, updatedAt int64

	err := row.Scan(&agent.AgentID, &worktreePath, &agent.PID, &branch, &prompt,
		&agent.Status, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	agent.WorktreePath = worktreePath.String
	agent.Branch = branch.String
	agent.Prompt = prompt.String
	agent.CreatedAt = time.Unix(createdAt, 0)
	agent.UpdatedAt = time.Unix(updatedAt, 0)

	return &agent, nil
}

func (s *Store) scanSpawnedAgentRow(rows *sql.Rows) (*SpawnedAgentRecord, error) {
	var agent SpawnedAgentRecord
	var worktreePath, branch, prompt sql.NullString
	var createdAt, updatedAt int64

	err := rows.Scan(&agent.AgentID, &worktreePath, &agent.PID, &branch, &prompt,
		&agent.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	agent.WorktreePath = worktreePath.String
	agent.Branch = branch.String
	agent.Prompt = prompt.String
	agent.CreatedAt = time.Unix(createdAt, 0)
	agent.UpdatedAt = time.Unix(updatedAt, 0)

	return &agent, nil
}
