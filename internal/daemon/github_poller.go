package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GitHubPoller polls GitHub issues for new comments and delivers them to agents
type GitHubPoller struct {
	store     *Store
	processes *ProcessManager
	eventCh   chan *mapv1.Event

	mu       sync.Mutex
	stop     chan struct{}
	interval time.Duration
}

// ghCommentAuthor represents the author of a GitHub comment
type ghCommentAuthor struct {
	Login string `json:"login"`
}

// ghComment represents a GitHub issue comment
type ghComment struct {
	ID        string          `json:"id"` // GraphQL node ID (e.g., "IC_kwDOPqDJoM7iErGE")
	Body      string          `json:"body"`
	Author    ghCommentAuthor `json:"author"`
	CreatedAt string          `json:"createdAt"`
}

// ghIssueComments is the response from gh issue view --json comments
type ghIssueComments struct {
	Comments []ghComment `json:"comments"`
}

// ghIssueState is the response from gh issue view --json state
type ghIssueState struct {
	State string `json:"state"` // "OPEN" or "CLOSED"
}

// inputRequestPrefix is the prefix we use when posting questions to GitHub
const inputRequestPrefix = "**My agent needs more input:**"

// tmuxPasteDelay is the delay after sending text to tmux before sending Enter
// This allows long pastes to be processed before submission
const tmuxPasteDelay = 1 * time.Second

// tmuxEnterDelay is the delay between Enter key presses
// Long pastes show as "[Pasted text #1 +N lines]" and need Enter to expand, then another to submit
const tmuxEnterDelay = 500 * time.Millisecond

// NewGitHubPoller creates a new GitHub poller
func NewGitHubPoller(store *Store, processes *ProcessManager, eventCh chan *mapv1.Event) *GitHubPoller {
	return &GitHubPoller{
		store:     store,
		processes: processes,
		eventCh:   eventCh,
		stop:      make(chan struct{}),
		interval:  30 * time.Second,
	}
}

// Start begins the polling loop
func (p *GitHubPoller) Start() {
	go p.pollLoop()
}

// Stop stops the polling loop
func (p *GitHubPoller) Stop() {
	close(p.stop)
}

func (p *GitHubPoller) pollLoop() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Do an immediate poll on start
	p.poll()

	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.poll()
		}
	}
}

func (p *GitHubPoller) poll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Get all tasks waiting for input and check for responses
	waitingTasks, err := p.store.ListTasksWaitingInput()
	if err != nil {
		log.Printf("github poller: failed to list waiting tasks: %v", err)
	} else {
		for _, task := range waitingTasks {
			p.checkTaskForResponse(task)
		}
	}

	// Get all in_progress tasks with GitHub sources and check if issues are closed
	inProgressTasks, err := p.store.ListTasksInProgressWithGitHub()
	if err != nil {
		log.Printf("github poller: failed to list in_progress tasks: %v", err)
	} else {
		for _, task := range inProgressTasks {
			p.checkTaskForClosedIssue(task)
		}
	}
}

func (p *GitHubPoller) checkTaskForResponse(task *TaskRecord) {
	// Fetch comments from GitHub
	comments, err := p.fetchGitHubComments(task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber)
	if err != nil {
		log.Printf("github poller: failed to fetch comments for %s/%s#%d: %v",
			task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, err)
		return
	}

	// Find new human comments (not our bot comments) since waiting_input_since
	var newComment *ghComment
	for i := len(comments) - 1; i >= 0; i-- {
		c := &comments[i]

		// Parse comment creation time
		createdAt, err := time.Parse(time.RFC3339, c.CreatedAt)
		if err != nil {
			continue
		}

		// Skip comments before we started waiting
		if createdAt.Before(task.WaitingInputSince) {
			continue
		}

		// Skip our own bot comments (those with the input request prefix)
		if strings.HasPrefix(c.Body, inputRequestPrefix) {
			continue
		}

		// Skip if we've already processed this comment
		if task.LastCommentID != "" && c.ID == task.LastCommentID {
			continue
		}

		// Found a new human comment
		newComment = c
		break
	}

	if newComment == nil {
		return
	}

	log.Printf("github poller: found new comment on %s/%s#%d from %s",
		task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, newComment.Author.Login)

	// Deliver response to agent's tmux session
	if err := p.deliverResponseToAgent(task, newComment.Body); err != nil {
		log.Printf("github poller: failed to deliver response to agent: %v", err)
		return
	}

	// Update task status back to in_progress
	if err := p.store.ClearTaskWaitingInput(task.TaskID, newComment.ID); err != nil {
		log.Printf("github poller: failed to update task status: %v", err)
		return
	}

	// Emit event
	p.emitInputReceivedEvent(task)

	log.Printf("github poller: delivered response to agent %s for task %s", task.AssignedTo, task.TaskID)
}

func (p *GitHubPoller) fetchGitHubComments(owner, repo string, issueNumber int) ([]ghComment, error) {
	args := []string{
		"issue", "view", strconv.Itoa(issueNumber),
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "comments",
	}

	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh issue view failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh issue view failed: %w", err)
	}

	var result ghIssueComments
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse comments: %w", err)
	}

	return result.Comments, nil
}

func (p *GitHubPoller) checkTaskForClosedIssue(task *TaskRecord) {
	state, err := p.fetchGitHubIssueState(task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber)
	if err != nil {
		log.Printf("github poller: failed to fetch issue state for %s/%s#%d: %v",
			task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, err)
		return
	}

	if state == "CLOSED" {
		log.Printf("github poller: issue %s/%s#%d is closed, marking task %s as completed",
			task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, task.TaskID)

		// Mark the task as completed
		if err := p.store.UpdateTaskStatus(task.TaskID, "completed"); err != nil {
			log.Printf("github poller: failed to mark task %s as completed: %v", task.TaskID, err)
			return
		}

		// Emit completion event
		p.emitTaskCompletedEvent(task)
	}
}

func (p *GitHubPoller) fetchGitHubIssueState(owner, repo string, issueNumber int) (string, error) {
	args := []string{
		"issue", "view", strconv.Itoa(issueNumber),
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "state",
	}

	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gh issue view failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("gh issue view failed: %w", err)
	}

	var result ghIssueState
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse issue state: %w", err)
	}

	return result.State, nil
}

func (p *GitHubPoller) emitTaskCompletedEvent(task *TaskRecord) {
	if p.eventCh == nil {
		return
	}

	event := &mapv1.Event{
		EventId:   uuid.New().String(),
		Type:      mapv1.EventType_EVENT_TYPE_TASK_COMPLETED,
		Timestamp: timestamppb.Now(),
		Payload: &mapv1.Event_Task{
			Task: &mapv1.TaskEvent{
				TaskId:    task.TaskID,
				NewStatus: mapv1.TaskStatus_TASK_STATUS_COMPLETED,
				AgentId:   task.AssignedTo,
			},
		},
	}

	select {
	case p.eventCh <- event:
	default:
	}
}

func (p *GitHubPoller) deliverResponseToAgent(task *TaskRecord, response string) error {
	if task.AssignedTo == "" {
		return fmt.Errorf("task has no assigned agent")
	}

	tmuxSession := p.processes.GetTmuxSession(task.AssignedTo)
	if tmuxSession == "" {
		return fmt.Errorf("agent %s has no tmux session", task.AssignedTo)
	}

	// Format the response message
	message := fmt.Sprintf("User response to your question:\n\n%s", response)

	// Replace newlines for single-line tmux input
	singleLineMessage := strings.ReplaceAll(message, "\n", " ")
	singleLineMessage = strings.ReplaceAll(singleLineMessage, "  ", " ")

	// Send to tmux session
	cmd := exec.Command("tmux", "send-keys", "-t", tmuxSession, "-l", singleLineMessage)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send response text: %w", err)
	}

	// Wait for pasted text to be processed (long text shows as collapsed paste)
	time.Sleep(tmuxPasteDelay)

	// Send Enter twice for long pastes:
	// 1st Enter: confirms/expands the collapsed paste preview
	// 2nd Enter: submits the prompt to the CLI
	cmd = exec.Command("tmux", "send-keys", "-t", tmuxSession, "Enter")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send first Enter: %w", err)
	}

	// Wait for paste to expand before sending second Enter
	time.Sleep(tmuxEnterDelay)

	cmd = exec.Command("tmux", "send-keys", "-t", tmuxSession, "Enter")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send second Enter: %w", err)
	}

	return nil
}

func (p *GitHubPoller) emitInputReceivedEvent(task *TaskRecord) {
	if p.eventCh == nil {
		return
	}

	event := &mapv1.Event{
		EventId:   uuid.New().String(),
		Type:      mapv1.EventType_EVENT_TYPE_TASK_INPUT_RECEIVED,
		Timestamp: timestamppb.Now(),
		Payload: &mapv1.Event_Task{
			Task: &mapv1.TaskEvent{
				TaskId:    task.TaskID,
				NewStatus: mapv1.TaskStatus_TASK_STATUS_IN_PROGRESS,
				AgentId:   task.AssignedTo,
			},
		},
	}

	select {
	case p.eventCh <- event:
	default:
	}
}

// PostQuestionToGitHub posts an input request comment to a GitHub issue
func PostQuestionToGitHub(owner, repo string, issueNumber int, question string) error {
	body := fmt.Sprintf("%s %s", inputRequestPrefix, question)

	args := []string{
		"issue", "comment", strconv.Itoa(issueNumber),
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--body", body,
	}

	out, err := exec.Command("gh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh issue comment failed: %s: %s", err, string(out))
	}

	return nil
}
