package daemon

import (
	"slices"
	"testing"
	"time"
)

func TestProcessManager_AgentTracking(t *testing.T) {
	manager := NewProcessManager("/tmp/logs", nil)

	idleSlot := &AgentSlot{
		AgentID:     "agent-idle",
		TmuxSession: "tmux-idle",
		CreatedAt:   time.Now(),
		Status:      AgentStatusIdle,
		AgentType:   AgentTypeClaude,
	}
	busySlot := &AgentSlot{
		AgentID:     "agent-busy",
		TmuxSession: "tmux-busy",
		CreatedAt:   time.Now(),
		Status:      AgentStatusBusy,
		AgentType:   AgentTypeCodex,
	}

	manager.agents[idleSlot.AgentID] = idleSlot
	manager.agents[busySlot.AgentID] = busySlot

	if got := manager.GetTmuxSession("agent-idle"); got != "tmux-idle" {
		t.Errorf("GetTmuxSession = %q, want %q", got, "tmux-idle")
	}
	if got := manager.GetTmuxSession("missing"); got != "" {
		t.Errorf("GetTmuxSession(missing) = %q, want empty", got)
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
	manager := NewProcessManager("/tmp/logs", nil)

	manager.agents["agent-1"] = &AgentSlot{
		AgentID:     "agent-1",
		TmuxSession: "tmux-missing",
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
