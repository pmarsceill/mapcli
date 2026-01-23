package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProcessManager manages spawned Claude Code agent slots using tmux sessions
type ProcessManager struct {
	mu               sync.RWMutex
	agents           map[string]*AgentSlot
	eventCh          chan *mapv1.Event
	logsDir          string
	lastAssigned     string // ID of last agent assigned a task (for round-robin)
	onAgentAvailable func() // callback when an agent becomes available
}

// AgentSlot represents an agent running in a tmux session
type AgentSlot struct {
	AgentID      string
	WorktreePath string
	TmuxSession  string // tmux session name
	CreatedAt    time.Time
	Status       string // "idle", "busy"
	CurrentTask  string // current task ID if busy
	AgentType    string // "claude" or "codex"

	mu sync.Mutex
}

// AgentSlot status constants
const (
	AgentStatusIdle = "idle"
	AgentStatusBusy = "busy"
)

// Agent type constants
const (
	AgentTypeClaude = "claude"
	AgentTypeCodex  = "codex"
)

// tmux session prefix to avoid conflicts
const tmuxPrefix = "map-agent-"

// NewProcessManager creates a new process manager
func NewProcessManager(logsDir string, eventCh chan *mapv1.Event) *ProcessManager {
	return &ProcessManager{
		agents:  make(map[string]*AgentSlot),
		eventCh: eventCh,
		logsDir: logsDir,
	}
}

// SetOnAgentAvailable sets a callback that is invoked when an agent becomes available.
// This is used to trigger processing of pending tasks.
func (m *ProcessManager) SetOnAgentAvailable(callback func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAgentAvailable = callback
}

// CreateSlot creates a new agent with a tmux session running claude or codex
// agentType should be "claude" (default) or "codex"
// If skipPermissions is true, the agent is started with permission-bypassing flags
func (m *ProcessManager) CreateSlot(agentID, workdir, agentType string, skipPermissions bool) (*AgentSlot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Default to claude if not specified
	if agentType == "" {
		agentType = AgentTypeClaude
	}

	// Check if agent already exists
	if _, exists := m.agents[agentID]; exists {
		return nil, fmt.Errorf("agent %s already exists", agentID)
	}

	// Check if tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux not found in PATH: %w", err)
	}

	// Determine CLI binary and flags based on agent type
	var cliBinary, cliCmd string
	switch agentType {
	case AgentTypeCodex:
		if _, err := exec.LookPath("codex"); err != nil {
			return nil, fmt.Errorf("codex CLI not found in PATH: %w", err)
		}
		cliBinary = "codex"
		if skipPermissions {
			cliCmd = "codex --dangerously-bypass-approvals-and-sandbox"
		} else {
			cliCmd = "codex"
		}
	default: // claude
		if _, err := exec.LookPath("claude"); err != nil {
			return nil, fmt.Errorf("claude CLI not found in PATH: %w", err)
		}
		cliBinary = "claude"
		if skipPermissions {
			cliCmd = "claude --dangerously-skip-permissions"
		} else {
			cliCmd = "claude"
		}
	}

	tmuxSession := tmuxPrefix + agentID

	// Create tmux session with the agent CLI running in it
	cmd := exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "-c", workdir, cliCmd)
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Configure tmux session for better resilience
	// - mouse: enable scrolling
	// - remain-on-exit: keep pane open if agent exits (prevents accidental Ctrl+C from killing session)
	// - @map_cli_cmd: store the CLI command for respawn keybinding
	// - bind R: respawn the agent with Ctrl+b R
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "mouse", "on").Run()
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "remain-on-exit", "on").Run()
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "@map_cli_cmd", cliCmd).Run()
	_ = exec.Command("tmux", "bind-key", "-t", tmuxSession, "R", "respawn-pane", "-k", cliCmd).Run()

	// Add agent ID to the status-right for easy identification
	statusRight := fmt.Sprintf(" [%s] %%H %%H:%%M %%d-%%b-%%y", agentID)
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "status-right", statusRight).Run()

	// Apply a subtle theme (neutral grays that work on both dark and light terminals)
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "status-style", "bg=colour240,fg=colour255").Run()
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "status-left-style", "bg=colour243,fg=colour255").Run()
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "status-right-style", "bg=colour243,fg=colour255").Run()
	_ = exec.Command("tmux", "set-option", "-t", tmuxSession, "window-status-current-style", "bg=colour245,fg=colour232,bold").Run()

	slot := &AgentSlot{
		AgentID:      agentID,
		WorktreePath: workdir,
		TmuxSession:  tmuxSession,
		CreatedAt:    time.Now(),
		Status:       AgentStatusIdle,
		AgentType:    agentType,
	}

	m.agents[agentID] = slot

	// Capture callback before unlocking
	callback := m.onAgentAvailable

	// Emit connected event
	m.emitAgentEvent(slot, true)

	log.Printf("created %s agent %s with tmux session %s (workdir: %s)", cliBinary, agentID, tmuxSession, workdir)

	// Notify that an agent is available (for pending task processing)
	if callback != nil {
		go callback()
	}

	return slot, nil
}

// ExecuteTask sends a task to the agent's tmux session
func (m *ProcessManager) ExecuteTask(ctx context.Context, agentID string, taskID string, description string, scopePaths []string) (string, error) {
	m.mu.RLock()
	slot, exists := m.agents[agentID]
	m.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("agent %s not found", agentID)
	}

	// Try to acquire the slot
	slot.mu.Lock()
	if slot.Status == AgentStatusBusy {
		slot.mu.Unlock()
		return "", fmt.Errorf("agent %s is busy", agentID)
	}
	slot.Status = AgentStatusBusy
	slot.CurrentTask = taskID
	tmuxSession := slot.TmuxSession
	slot.mu.Unlock()

	// Ensure we release the slot when done and notify about availability
	defer func() {
		slot.mu.Lock()
		slot.Status = AgentStatusIdle
		slot.CurrentTask = ""
		slot.mu.Unlock()

		// Notify that an agent is available (for pending task processing)
		m.mu.RLock()
		callback := m.onAgentAvailable
		m.mu.RUnlock()
		if callback != nil {
			go callback()
		}
	}()

	log.Printf("agent %s executing task %s via tmux", agentID, taskID)

	// Build the prompt with task ID prefix for agent introspection
	prompt := fmt.Sprintf("[Task ID: %s]\n\n%s", taskID, description)
	if len(scopePaths) > 0 {
		prompt = fmt.Sprintf("%s\n\nScope/files: %s", prompt, strings.Join(scopePaths, ", "))
	}

	// Send the prompt to the tmux session
	// Replace newlines with spaces to keep as single-line input for the CLI
	singleLinePrompt := strings.ReplaceAll(prompt, "\n", " ")
	singleLinePrompt = strings.ReplaceAll(singleLinePrompt, "  ", " ") // collapse double spaces

	// Use tmux send-keys with -l (literal) flag to send text, then Enter separately
	// This ensures the text is sent exactly as-is without tmux interpreting special chars
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, "-l", singleLinePrompt)
	if err := cmd.Run(); err != nil {
		log.Printf("agent %s task %s failed to send text: %v", agentID, taskID, err)
		return "", fmt.Errorf("failed to send task to tmux: %w", err)
	}

	// Wait for the pasted text to be processed by the terminal
	// Long text may show as "[Pasted text #1 +N lines]" and need confirmation
	time.Sleep(300 * time.Millisecond)

	// Send Enter key to confirm/submit the prompt
	// For long pastes, this confirms the paste; for short text, this submits
	cmd = exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, "Enter")
	if err := cmd.Run(); err != nil {
		log.Printf("agent %s task %s failed to send Enter: %v", agentID, taskID, err)
		return "", fmt.Errorf("failed to submit task to tmux: %w", err)
	}

	log.Printf("agent %s task %s sent to tmux session", agentID, taskID)

	// Note: With tmux, we don't wait for completion or capture output
	// The user interacts directly with the session
	return "Task sent to agent's tmux session. Use 'map agent watch' to interact.", nil
}

// GetTmuxSession returns the tmux session name for an agent
func (m *ProcessManager) GetTmuxSession(agentID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if slot, ok := m.agents[agentID]; ok {
		return slot.TmuxSession
	}
	return ""
}

// HasTmuxSession checks if a tmux session exists for the agent
func (m *ProcessManager) HasTmuxSession(agentID string) bool {
	m.mu.RLock()
	slot, exists := m.agents[agentID]
	m.mu.RUnlock()

	if !exists {
		return false
	}

	// Check if tmux session actually exists
	cmd := exec.Command("tmux", "has-session", "-t", slot.TmuxSession)
	return cmd.Run() == nil
}

// FindAvailableAgent finds an idle agent slot using round-robin selection
func (m *ProcessManager) FindAvailableAgent() *AgentSlot {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.agents) == 0 {
		return nil
	}

	// Get sorted list of agent IDs for consistent ordering
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Find starting index (after lastAssigned)
	startIdx := 0
	if m.lastAssigned != "" {
		for i, id := range ids {
			if id == m.lastAssigned {
				startIdx = (i + 1) % len(ids)
				break
			}
		}
	}

	// Round-robin: check agents starting from startIdx
	for i := 0; i < len(ids); i++ {
		idx := (startIdx + i) % len(ids)
		slot := m.agents[ids[idx]]
		slot.mu.Lock()
		if slot.Status == AgentStatusIdle {
			m.lastAssigned = slot.AgentID
			slot.mu.Unlock()
			return slot
		}
		slot.mu.Unlock()
	}
	return nil
}

// Remove removes an agent slot and kills its tmux session
func (m *ProcessManager) Remove(agentID string) {
	m.mu.Lock()
	slot, exists := m.agents[agentID]
	if exists {
		delete(m.agents, agentID)
	}
	m.mu.Unlock()

	if exists {
		// Kill the tmux session
		cmd := exec.Command("tmux", "kill-session", "-t", slot.TmuxSession)
		if err := cmd.Run(); err != nil {
			log.Printf("warning: failed to kill tmux session %s: %v", slot.TmuxSession, err)
		}

		m.emitAgentEvent(slot, false)
		log.Printf("removed agent %s and killed tmux session %s", agentID, slot.TmuxSession)
	}
}

// Get retrieves an agent slot by ID
func (m *ProcessManager) Get(agentID string) *AgentSlot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[agentID]
}

// GetLogFile returns empty string - tmux sessions don't use log files
func (m *ProcessManager) GetLogFile(agentID string) string {
	return ""
}

// GetLogsDir returns the logs directory path
func (m *ProcessManager) GetLogsDir() string {
	return m.logsDir
}

// List returns all agent slots
func (m *ProcessManager) List() []*AgentSlot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*AgentSlot, 0, len(m.agents))
	for _, slot := range m.agents {
		result = append(result, slot)
	}
	return result
}

// ListIdle returns IDs of idle agents
func (m *ProcessManager) ListIdle() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var idle []string
	for id, slot := range m.agents {
		slot.mu.Lock()
		if slot.Status == AgentStatusIdle {
			idle = append(idle, id)
		}
		slot.mu.Unlock()
	}
	return idle
}

// ListRunning returns all agent IDs (for worktree cleanup compatibility)
func (m *ProcessManager) ListRunning() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	running := make(map[string]bool)
	for id := range m.agents {
		running[id] = true
	}
	return running
}

// KillAll removes all agent slots and kills their tmux sessions
func (m *ProcessManager) KillAll() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.Remove(id)
	}
	return nil
}

// ToProto converts an AgentSlot to its proto representation
func (slot *AgentSlot) ToProto() *mapv1.SpawnedAgentInfo {
	slot.mu.Lock()
	defer slot.mu.Unlock()

	return &mapv1.SpawnedAgentInfo{
		AgentId:      slot.AgentID,
		WorktreePath: slot.WorktreePath,
		Pid:          0,
		Status:       GetTmuxPaneTitle(slot.TmuxSession),
		CreatedAt:    timestamppb.New(slot.CreatedAt),
		LogFile:      slot.TmuxSession, // Repurpose LogFile to show tmux session
		AgentType:    slot.AgentType,
	}
}

// emitAgentEvent sends an agent lifecycle event
func (m *ProcessManager) emitAgentEvent(slot *AgentSlot, connected bool) {
	if m.eventCh == nil {
		return
	}

	message := fmt.Sprintf("agent %s disconnected", slot.AgentID)
	if connected {
		message = fmt.Sprintf("agent %s connected (tmux: %s)", slot.AgentID, slot.TmuxSession)
	}

	event := &mapv1.Event{
		Timestamp: timestamppb.Now(),
		Payload: &mapv1.Event_Status{
			Status: &mapv1.StatusEvent{
				Message: message,
			},
		},
	}

	select {
	case m.eventCh <- event:
	default:
		// Channel full, drop event
	}
}

// Spawn creates a slot and optionally sends an initial prompt
// agentType should be "claude" (default) or "codex"
// If skipPermissions is true, the agent is started with permission-bypassing flags
func (m *ProcessManager) Spawn(agentID, workdir, prompt, agentType string, skipPermissions bool) (*AgentSlot, error) {
	slot, err := m.CreateSlot(agentID, workdir, agentType, skipPermissions)
	if err != nil {
		return nil, err
	}

	// If a prompt was provided, send it to the tmux session
	if prompt != "" {
		// Give the agent a moment to start up
		time.Sleep(500 * time.Millisecond)

		// Replace newlines with spaces to keep as single-line input
		singleLinePrompt := strings.ReplaceAll(prompt, "\n", " ")
		singleLinePrompt = strings.ReplaceAll(singleLinePrompt, "  ", " ")

		// Send text with -l (literal) flag, then Enter separately
		cmd := exec.Command("tmux", "send-keys", "-t", slot.TmuxSession, "-l", singleLinePrompt)
		if err := cmd.Run(); err != nil {
			log.Printf("warning: failed to send initial prompt text to %s: %v", agentID, err)
		} else {
			// Wait for pasted text to be processed (long text shows as collapsed paste)
			time.Sleep(300 * time.Millisecond)

			// Send Enter to confirm/submit
			cmd = exec.Command("tmux", "send-keys", "-t", slot.TmuxSession, "Enter")
			if err := cmd.Run(); err != nil {
				log.Printf("warning: failed to send Enter to %s: %v", agentID, err)
			} else {
				log.Printf("sent initial prompt to agent %s", agentID)
			}
		}
	}

	return slot, nil
}

// ListTmuxSessions returns all map agent tmux sessions (including orphaned ones)
func ListTmuxSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		// No sessions is not an error
		return nil, nil
	}

	var sessions []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(line, tmuxPrefix) {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// GetTmuxSessionDir returns the working directory of a tmux session
func GetTmuxSessionDir(sessionName string) string {
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{pane_current_path}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetTmuxPaneTitle returns the pane title of a tmux session (used as status display)
func GetTmuxPaneTitle(sessionName string) string {
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{pane_title}")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	title := strings.TrimSpace(string(output))
	if title == "" {
		return "idle"
	}
	return title
}

// IsTmuxPaneDead checks if the pane's process has exited (remain-on-exit keeps pane open)
func IsTmuxPaneDead(sessionName string) bool {
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{pane_dead}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}

// RespawnInPane respawns the agent process in a dead tmux pane
func (m *ProcessManager) RespawnInPane(agentID string, skipPermissions bool) error {
	m.mu.RLock()
	slot, exists := m.agents[agentID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// Check if session exists
	checkCmd := exec.Command("tmux", "has-session", "-t", slot.TmuxSession)
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("tmux session %s not found", slot.TmuxSession)
	}

	// Check if pane is dead
	if !IsTmuxPaneDead(slot.TmuxSession) {
		return fmt.Errorf("agent %s pane is still running - cannot respawn", agentID)
	}

	// Determine CLI command based on agent type
	var cliCmd string
	agentType := slot.AgentType
	if agentType == "" {
		agentType = AgentTypeClaude
	}

	switch agentType {
	case AgentTypeCodex:
		if skipPermissions {
			cliCmd = "codex --dangerously-bypass-approvals-and-sandbox"
		} else {
			cliCmd = "codex"
		}
	default: // claude
		if skipPermissions {
			cliCmd = "claude --dangerously-skip-permissions"
		} else {
			cliCmd = "claude"
		}
	}

	cmd := exec.Command("tmux", "respawn-pane", "-t", slot.TmuxSession, "-k", cliCmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to respawn %s in pane: %w", agentType, err)
	}

	log.Printf("respawned %s in agent %s", agentType, agentID)
	return nil
}
