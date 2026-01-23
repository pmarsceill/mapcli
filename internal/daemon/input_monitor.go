package daemon

import (
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// InputMonitor watches tmux sessions for agents waiting on user input
// and automatically posts questions to GitHub issues
type InputMonitor struct {
	store     *Store
	processes *ProcessManager
	eventCh   chan *mapv1.Event

	mu       sync.Mutex
	stop     chan struct{}
	interval time.Duration

	// Track pane state to detect when agent becomes idle
	lastContent    map[string]string    // agentID -> last captured content
	lastChangeTime map[string]time.Time // agentID -> when content last changed
	idleThreshold  time.Duration        // how long idle before considered waiting
}

// Patterns that suggest the agent is asking a question
var questionPatterns = []*regexp.Regexp{
	// Common question endings
	regexp.MustCompile(`\?\s*$`),
	// Claude Code specific patterns
	regexp.MustCompile(`(?i)please (choose|select|specify|confirm|provide)`),
	regexp.MustCompile(`(?i)would you like`),
	regexp.MustCompile(`(?i)do you want`),
	regexp.MustCompile(`(?i)should I`),
	regexp.MustCompile(`(?i)which (one|option)`),
	regexp.MustCompile(`(?i)what (should|would)`),
	// Input prompts
	regexp.MustCompile(`\[Y/n\]`),
	regexp.MustCompile(`\[y/N\]`),
	regexp.MustCompile(`\(y/n\)`),
	regexp.MustCompile(`Enter .+:`),
}

// Patterns that indicate the agent is actively working (not waiting)
var activePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)reading|writing|searching|analyzing|processing`),
	regexp.MustCompile(`(?i)running|executing|building|compiling`),
	regexp.MustCompile(`⠋|⠙|⠹|⠸|⠼|⠴|⠦|⠧|⠇|⠏`), // Spinner characters
	regexp.MustCompile(`\.\.\.`), // Ellipsis indicating progress
}

// NewInputMonitor creates a new input monitor
func NewInputMonitor(store *Store, processes *ProcessManager, eventCh chan *mapv1.Event) *InputMonitor {
	return &InputMonitor{
		store:          store,
		processes:      processes,
		eventCh:        eventCh,
		stop:           make(chan struct{}),
		interval:       5 * time.Second,
		lastContent:    make(map[string]string),
		lastChangeTime: make(map[string]time.Time),
		idleThreshold:  10 * time.Second, // Consider waiting if idle for 10s with question
	}
}

// Start begins the monitoring loop
func (m *InputMonitor) Start() {
	go m.monitorLoop()
}

// Stop stops the monitoring loop
func (m *InputMonitor) Stop() {
	close(m.stop)
}

func (m *InputMonitor) monitorLoop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.checkAllAgents()
		}
	}
}

func (m *InputMonitor) checkAllAgents() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get all agents
	agents := m.processes.List()

	for _, agent := range agents {
		m.checkAgent(agent)
	}
}

func (m *InputMonitor) checkAgent(agent *AgentSlot) {
	// Skip if no tmux session
	if agent.TmuxSession == "" {
		return
	}

	// Get the task assigned to this agent
	task, err := m.store.GetTaskByAgentID(agent.AgentID)
	if err != nil || task == nil {
		return
	}

	// Skip if task doesn't have GitHub source
	if task.GitHubOwner == "" || task.GitHubRepo == "" || task.GitHubIssueNumber == 0 {
		return
	}

	// Skip if already waiting for input
	if task.Status == "waiting_input" {
		return
	}

	// Skip if not in progress
	if task.Status != "in_progress" {
		return
	}

	// Capture current tmux pane content
	content := m.captureTmuxContent(agent.TmuxSession)
	if content == "" {
		return
	}

	// Track content changes
	now := time.Now()
	lastContent := m.lastContent[agent.AgentID]
	if content != lastContent {
		m.lastContent[agent.AgentID] = content
		m.lastChangeTime[agent.AgentID] = now
		return // Content changed, not idle yet
	}

	// Check if idle long enough
	lastChange, exists := m.lastChangeTime[agent.AgentID]
	if !exists {
		m.lastChangeTime[agent.AgentID] = now
		return
	}

	idleDuration := now.Sub(lastChange)
	if idleDuration < m.idleThreshold {
		return // Not idle long enough
	}

	// Check if content suggests waiting for input
	if m.isActivelyWorking(content) {
		return // Agent appears to be working
	}

	question := m.extractQuestion(content)
	if question == "" {
		return // No question detected
	}

	log.Printf("input monitor: detected question from agent %s: %s", agent.AgentID, truncateLog(question, 100))

	// Post question to GitHub
	if err := PostQuestionToGitHub(task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, question); err != nil {
		log.Printf("input monitor: failed to post question to GitHub: %v", err)
		return
	}

	// Update task status
	if err := m.store.SetTaskWaitingInput(task.TaskID, question); err != nil {
		log.Printf("input monitor: failed to update task status: %v", err)
		return
	}

	// Reset tracking for this agent
	delete(m.lastContent, agent.AgentID)
	delete(m.lastChangeTime, agent.AgentID)

	// Emit event
	m.emitWaitingInputEvent(task, question)

	log.Printf("input monitor: posted question to %s/%s#%d for task %s",
		task.GitHubOwner, task.GitHubRepo, task.GitHubIssueNumber, task.TaskID)
}

func (m *InputMonitor) captureTmuxContent(session string) string {
	// Capture the visible pane content
	cmd := exec.Command("tmux", "capture-pane", "-t", session, "-p", "-S", "-50")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (m *InputMonitor) isActivelyWorking(content string) bool {
	// Check last few lines for active work patterns
	lines := strings.Split(content, "\n")
	lastLines := lines
	if len(lines) > 10 {
		lastLines = lines[len(lines)-10:]
	}
	recentContent := strings.Join(lastLines, "\n")

	for _, pattern := range activePatterns {
		if pattern.MatchString(recentContent) {
			return true
		}
	}
	return false
}

func (m *InputMonitor) extractQuestion(content string) string {
	lines := strings.Split(content, "\n")

	// Look at the last 20 lines for a question
	startIdx := 0
	if len(lines) > 20 {
		startIdx = len(lines) - 20
	}
	recentLines := lines[startIdx:]

	// Find lines that look like questions
	var questionLines []string
	foundQuestion := false

	for i := len(recentLines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(recentLines[i])
		if line == "" {
			if foundQuestion {
				break // Stop at blank line after finding question
			}
			continue
		}

		// Check if this line matches question patterns
		isQuestion := false
		for _, pattern := range questionPatterns {
			if pattern.MatchString(line) {
				isQuestion = true
				break
			}
		}

		if isQuestion {
			foundQuestion = true
		}

		if foundQuestion {
			// Prepend this line (we're going backwards)
			questionLines = append([]string{line}, questionLines...)
		}

		// Don't go back too far
		if len(questionLines) > 5 {
			break
		}
	}

	if len(questionLines) == 0 {
		return ""
	}

	return strings.Join(questionLines, "\n")
}

func (m *InputMonitor) emitWaitingInputEvent(task *TaskRecord, question string) {
	if m.eventCh == nil {
		return
	}

	event := &mapv1.Event{
		EventId:   uuid.New().String(),
		Type:      mapv1.EventType_EVENT_TYPE_TASK_WAITING_INPUT,
		Timestamp: timestamppb.Now(),
		Payload: &mapv1.Event_Task{
			Task: &mapv1.TaskEvent{
				TaskId:    task.TaskID,
				NewStatus: mapv1.TaskStatus_TASK_STATUS_WAITING_INPUT,
				AgentId:   task.AssignedTo,
			},
		},
	}

	select {
	case m.eventCh <- event:
	default:
	}
}

func truncateLog(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
