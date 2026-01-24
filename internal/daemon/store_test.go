package daemon

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "mapd-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	store, err := NewStore(tempDir)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("create store: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tempDir)
	}

	return store, cleanup
}

func TestNewStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mapd-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify database file was created
	dbPath := filepath.Join(tempDir, "mapd.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestNewStore_CreatesDataDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mapd-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Use a nested path that doesn't exist
	dataDir := filepath.Join(tempDir, "nested", "data", "dir")

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Error("data directory was not created")
	}
}

// --- Task Operations Tests ---

func TestCreateTask(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	task := &TaskRecord{
		TaskID:      "task-123",
		Description: "Test task description",
		ScopePaths:  []string{"/path/one", "/path/two"},
		Status:      "pending",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := store.CreateTask(task)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	retrieved, err := store.GetTask("task-123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetTask returned nil")
	}

	if retrieved.Description != "Test task description" {
		t.Errorf("Description = %q, want %q", retrieved.Description, "Test task description")
	}
	if len(retrieved.ScopePaths) != 2 {
		t.Errorf("ScopePaths len = %d, want 2", len(retrieved.ScopePaths))
	}
	if retrieved.Status != "pending" {
		t.Errorf("Status = %q, want %q", retrieved.Status, "pending")
	}
}

func TestGetTask_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	task, err := store.GetTask("nonexistent")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestListTasks(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()

	tasks := []*TaskRecord{
		{TaskID: "task-1", Description: "Task 1", Status: "pending", CreatedAt: now, UpdatedAt: now},
		{TaskID: "task-2", Description: "Task 2", Status: "pending", CreatedAt: now.Add(time.Second), UpdatedAt: now},
		{TaskID: "task-3", Description: "Task 3", Status: "completed", AssignedTo: "agent-1", CreatedAt: now.Add(2 * time.Second), UpdatedAt: now},
	}

	for _, task := range tasks {
		if err := store.CreateTask(task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	// List all
	all, err := store.ListTasks("", "", "", 0)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListTasks returned %d tasks, want 3", len(all))
	}

	// Filter by status
	pending, err := store.ListTasks("pending", "", "", 0)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("ListTasks(pending) returned %d tasks, want 2", len(pending))
	}

	// Filter by agent
	agentTasks, err := store.ListTasks("", "agent-1", "", 0)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(agentTasks) != 1 {
		t.Errorf("ListTasks(agent-1) returned %d tasks, want 1", len(agentTasks))
	}

	// With limit
	limited, err := store.ListTasks("", "", "", 2)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("ListTasks(limit=2) returned %d tasks, want 2", len(limited))
	}
}

func TestUpdateTask(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	task := &TaskRecord{
		TaskID:      "task-123",
		Description: "Original description",
		Status:      "pending",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// Update the task
	task.Status = "completed"
	task.Result = "Task completed successfully"
	task.UpdatedAt = now.Add(time.Hour)

	if err := store.UpdateTask(task); err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	retrieved, err := store.GetTask("task-123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if retrieved.Status != "completed" {
		t.Errorf("Status = %q, want %q", retrieved.Status, "completed")
	}
	if retrieved.Result != "Task completed successfully" {
		t.Errorf("Result = %q, want %q", retrieved.Result, "Task completed successfully")
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	task := &TaskRecord{
		TaskID:    "task-123",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if err := store.UpdateTaskStatus("task-123", "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	retrieved, err := store.GetTask("task-123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if retrieved.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", retrieved.Status, "in_progress")
	}
}

func TestAssignTask(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	task := &TaskRecord{
		TaskID:    "task-123",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if err := store.AssignTask("task-123", "instance-456"); err != nil {
		t.Fatalf("AssignTask failed: %v", err)
	}

	retrieved, err := store.GetTask("task-123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if retrieved.AssignedTo != "instance-456" {
		t.Errorf("AssignedTo = %q, want %q", retrieved.AssignedTo, "instance-456")
	}
	if retrieved.Status != "accepted" {
		t.Errorf("Status = %q, want %q", retrieved.Status, "accepted")
	}
}

// --- Event Operations Tests ---

func TestCreateEvent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	event := &EventRecord{
		EventID:   "event-123",
		Type:      "AGENT_CONNECTED",
		Payload:   `{"agent_id":"test-agent"}`,
		CreatedAt: time.Now(),
	}

	err := store.CreateEvent(event)
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}

	events, err := store.ListRecentEvents(10)
	if err != nil {
		t.Fatalf("ListRecentEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("ListRecentEvents returned %d events, want 1", len(events))
	}

	if events[0].EventID != "event-123" {
		t.Errorf("EventID = %q, want %q", events[0].EventID, "event-123")
	}
}

func TestListRecentEvents_Limit(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	for i := 0; i < 10; i++ {
		event := &EventRecord{
			EventID:   string(rune('A' + i)),
			Type:      "TEST_EVENT",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if err := store.CreateEvent(event); err != nil {
			t.Fatalf("CreateEvent failed: %v", err)
		}
	}

	events, err := store.ListRecentEvents(5)
	if err != nil {
		t.Fatalf("ListRecentEvents failed: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("ListRecentEvents(5) returned %d events, want 5", len(events))
	}
}

// --- Stats Tests ---

func TestGetStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()

	// Create tasks
	tasks := []*TaskRecord{
		{TaskID: "task-1", Status: "pending", CreatedAt: now, UpdatedAt: now},
		{TaskID: "task-2", Status: "pending", CreatedAt: now, UpdatedAt: now},
		{TaskID: "task-3", Status: "in_progress", CreatedAt: now, UpdatedAt: now},
		{TaskID: "task-4", Status: "accepted", CreatedAt: now, UpdatedAt: now},
		{TaskID: "task-5", Status: "completed", CreatedAt: now, UpdatedAt: now},
	}
	for _, task := range tasks {
		if err := store.CreateTask(task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	pendingTasks, activeTasks, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if pendingTasks != 2 {
		t.Errorf("pendingTasks = %d, want 2", pendingTasks)
	}
	if activeTasks != 2 {
		t.Errorf("activeTasks = %d, want 2", activeTasks)
	}
}

// --- Spawned Agent Operations Tests ---

func TestSpawnedAgentCRUD(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	agent := &SpawnedAgentRecord{
		AgentID:      "spawned-123",
		WorktreePath: "/path/to/worktree",
		PID:          12345,
		Branch:       "main",
		Prompt:       "Fix the bug",
		Status:       "running",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Create
	if err := store.CreateSpawnedAgent(agent); err != nil {
		t.Fatalf("CreateSpawnedAgent failed: %v", err)
	}

	// Read
	retrieved, err := store.GetSpawnedAgent("spawned-123")
	if err != nil {
		t.Fatalf("GetSpawnedAgent failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetSpawnedAgent returned nil")
	}

	if retrieved.WorktreePath != "/path/to/worktree" {
		t.Errorf("WorktreePath = %q, want %q", retrieved.WorktreePath, "/path/to/worktree")
	}
	if retrieved.PID != 12345 {
		t.Errorf("PID = %d, want 12345", retrieved.PID)
	}

	// Update status
	if err := store.UpdateSpawnedAgentStatus("spawned-123", "stopped"); err != nil {
		t.Fatalf("UpdateSpawnedAgentStatus failed: %v", err)
	}

	retrieved, err = store.GetSpawnedAgent("spawned-123")
	if err != nil {
		t.Fatalf("GetSpawnedAgent failed: %v", err)
	}
	if retrieved.Status != "stopped" {
		t.Errorf("Status = %q, want %q", retrieved.Status, "stopped")
	}

	// Delete
	if err := store.DeleteSpawnedAgent("spawned-123"); err != nil {
		t.Fatalf("DeleteSpawnedAgent failed: %v", err)
	}

	retrieved, err = store.GetSpawnedAgent("spawned-123")
	if err != nil {
		t.Fatalf("GetSpawnedAgent failed: %v", err)
	}
	if retrieved != nil {
		t.Error("expected nil after delete")
	}
}

func TestListSpawnedAgents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	now := time.Now()
	agents := []*SpawnedAgentRecord{
		{AgentID: "spawned-1", Status: "running", CreatedAt: now, UpdatedAt: now},
		{AgentID: "spawned-2", Status: "running", CreatedAt: now, UpdatedAt: now},
		{AgentID: "spawned-3", Status: "stopped", CreatedAt: now, UpdatedAt: now},
	}

	for _, a := range agents {
		if err := store.CreateSpawnedAgent(a); err != nil {
			t.Fatalf("CreateSpawnedAgent failed: %v", err)
		}
	}

	// List all
	all, err := store.ListSpawnedAgents("", "")
	if err != nil {
		t.Fatalf("ListSpawnedAgents failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListSpawnedAgents returned %d agents, want 3", len(all))
	}

	// Filter by status
	running, err := store.ListSpawnedAgents("running", "")
	if err != nil {
		t.Fatalf("ListSpawnedAgents failed: %v", err)
	}
	if len(running) != 2 {
		t.Errorf("ListSpawnedAgents(running) returned %d agents, want 2", len(running))
	}
}

// TestNewStore_MigratesLegacySchema verifies that NewStore can open a database
// created with an older schema version and successfully migrate it.
// This prevents regressions where new schema elements reference columns
// that don't exist until migrations run.
func TestNewStore_MigratesLegacySchema(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mapd-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "mapd.db")

	// Create a database with the "legacy" schema (before GitHub columns were added)
	legacySchema := `
CREATE TABLE IF NOT EXISTS tasks (
	task_id TEXT PRIMARY KEY,
	description TEXT NOT NULL,
	scope_paths TEXT,
	status TEXT DEFAULT 'pending',
	assigned_to TEXT,
	result TEXT,
	error TEXT,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assigned_to ON tasks(assigned_to);

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

	// Create and populate the legacy database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	if _, err := db.Exec(legacySchema); err != nil {
		_ = db.Close()
		t.Fatalf("create legacy schema: %v", err)
	}

	// Insert a task using the old schema
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO tasks (task_id, description, scope_paths, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "legacy-task", "A task from before migration", "[]", "pending", now, now)
	if err != nil {
		_ = db.Close()
		t.Fatalf("insert legacy task: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	// Now open the database with NewStore - this should migrate successfully
	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed to migrate legacy database: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Verify the legacy task is still accessible
	task, err := store.GetTask("legacy-task")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task == nil {
		t.Fatal("legacy task not found after migration")
	}
	if task.Description != "A task from before migration" {
		t.Errorf("Description = %q, want %q", task.Description, "A task from before migration")
	}

	// Verify that new columns exist and work by creating a task with GitHub metadata
	newTask := &TaskRecord{
		TaskID:            "new-task",
		Description:       "A task with GitHub metadata",
		Status:            "pending",
		GitHubOwner:       "testowner",
		GitHubRepo:        "testrepo",
		GitHubIssueNumber: 42,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if err := store.CreateTask(newTask); err != nil {
		t.Fatalf("CreateTask with GitHub metadata failed: %v", err)
	}

	// Verify the GitHub metadata was stored correctly
	retrieved, err := store.GetTask("new-task")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if retrieved.GitHubOwner != "testowner" {
		t.Errorf("GitHubOwner = %q, want %q", retrieved.GitHubOwner, "testowner")
	}
	if retrieved.GitHubRepo != "testrepo" {
		t.Errorf("GitHubRepo = %q, want %q", retrieved.GitHubRepo, "testrepo")
	}
	if retrieved.GitHubIssueNumber != 42 {
		t.Errorf("GitHubIssueNumber = %d, want 42", retrieved.GitHubIssueNumber)
	}
}
