package daemon

import (
	"os/exec"
	"slices"
	"testing"
	"time"
)

// mockMultiplexer implements Multiplexer interface for testing
type mockMultiplexer struct{}

func (m *mockMultiplexer) CreateSession(name, workdir, command string) error { return nil }
func (m *mockMultiplexer) KillSession(name string) error                     { return nil }
func (m *mockMultiplexer) HasSession(name string) bool                       { return true }
func (m *mockMultiplexer) ListSessions(prefix string) ([]string, error)      { return nil, nil }
func (m *mockMultiplexer) SendText(sessionName, text string) error           { return nil }
func (m *mockMultiplexer) SendEnter(sessionName string) error                { return nil }
func (m *mockMultiplexer) RespawnPane(sessionName, command string) error     { return nil }
func (m *mockMultiplexer) GetPaneWorkdir(sessionName string) string          { return "" }
func (m *mockMultiplexer) GetPaneTitle(sessionName string) string            { return "mock" }
func (m *mockMultiplexer) IsPaneDead(sessionName string) bool                { return false }
func (m *mockMultiplexer) AttachCommand(sessionName string) *exec.Cmd        { return nil }
func (m *mockMultiplexer) ConfigureSession(sessionName string, opts SessionOptions) error {
	return nil
}
func (m *mockMultiplexer) Name() string { return "mock" }

func TestProcessManager_AgentTracking(t *testing.T) {
	manager := NewProcessManager("/tmp/logs", nil, &mockMultiplexer{})

	idleSlot := &AgentSlot{
		AgentID:     "agent-idle",
		SessionName: "session-idle",
		CreatedAt:   time.Now(),
		Status:      AgentStatusIdle,
		AgentType:   AgentTypeClaude,
	}
	busySlot := &AgentSlot{
		AgentID:     "agent-busy",
		SessionName: "session-busy",
		CreatedAt:   time.Now(),
		Status:      AgentStatusBusy,
		AgentType:   AgentTypeCodex,
	}

	manager.agents[idleSlot.AgentID] = idleSlot
	manager.agents[busySlot.AgentID] = busySlot

	if got := manager.GetSessionName("agent-idle"); got != "session-idle" {
		t.Errorf("GetSessionName = %q, want %q", got, "session-idle")
	}
	if got := manager.GetSessionName("missing"); got != "" {
		t.Errorf("GetSessionName(missing) = %q, want empty", got)
	}

	slot := manager.FindAvailableAgent()
	if slot == nil || slot.AgentID != "agent-idle" {
		t.Fatalf("FindAvailableAgent returned %+v, want agent-idle", slot)
	}

	idle := manager.ListIdle()
	if len(idle) != 1 || !slices.Contains(idle, "agent-idle") {
		t.Errorf("ListIdle = %v, want [agent-idle]", idle)
	}

	running := manager.ListRunning()
	if len(running) != 2 || !running["agent-idle"] || !running["agent-busy"] {
		t.Errorf("ListRunning = %v, want both agents", running)
	}

	if got := manager.GetLogsDir(); got != "/tmp/logs" {
		t.Errorf("GetLogsDir = %q, want %q", got, "/tmp/logs")
	}
	if got := manager.GetLogFile("agent-idle"); got != "" {
		t.Errorf("GetLogFile = %q, want empty", got)
	}
}

func TestProcessManager_Remove(t *testing.T) {
	manager := NewProcessManager("/tmp/logs", nil, &mockMultiplexer{})

	manager.agents["agent-1"] = &AgentSlot{
		AgentID:     "agent-1",
		SessionName: "session-missing",
		CreatedAt:   time.Now(),
		Status:      AgentStatusIdle,
	}

	manager.Remove("agent-1")

	if manager.Get("agent-1") != nil {
		t.Error("Remove should delete agent from manager")
	}
	if len(manager.List()) != 0 {
		t.Errorf("List returned %d agents, want 0", len(manager.List()))
	}
}
