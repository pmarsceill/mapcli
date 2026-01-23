package daemon

import (
	"os"
	"os/exec"
)

// Multiplexer interface abstracts terminal multiplexer operations (tmux, zellij)
type Multiplexer interface {
	// Session lifecycle
	CreateSession(name, workdir, command string) error
	KillSession(name string) error
	HasSession(name string) bool
	ListSessions(prefix string) ([]string, error)

	// Session interaction
	SendText(sessionName, text string) error
	SendEnter(sessionName string) error
	RespawnPane(sessionName, command string) error

	// Session info
	GetPaneWorkdir(sessionName string) string
	GetPaneTitle(sessionName string) string
	IsPaneDead(sessionName string) bool

	// Attachment (returns command to exec)
	AttachCommand(sessionName string) *exec.Cmd

	// Configuration
	ConfigureSession(sessionName string, opts SessionOptions) error

	// Identification
	Name() string // "tmux" or "zellij"
}

// SessionOptions contains configuration options for multiplexer sessions
type SessionOptions struct {
	AgentID        string
	MouseEnabled   bool
	StatusBarLabel string
	CLICommand     string // The CLI command used to respawn (e.g., "claude --dangerously-skip-permissions")
}

// MultiplexerType represents supported multiplexer types
type MultiplexerType string

const (
	MultiplexerTmux   MultiplexerType = "tmux"
	MultiplexerZellij MultiplexerType = "zellij"
)

// NewMultiplexer creates a multiplexer instance based on the specified type
func NewMultiplexer(muxType MultiplexerType) (Multiplexer, error) {
	switch muxType {
	case MultiplexerZellij:
		return NewZellijMultiplexer()
	default:
		return NewTmuxMultiplexer()
	}
}

// GetMultiplexerType determines multiplexer type from environment or returns default
func GetMultiplexerType() MultiplexerType {
	if mux := os.Getenv("MAP_MULTIPLEXER"); mux != "" {
		switch mux {
		case "zellij":
			return MultiplexerZellij
		case "tmux":
			return MultiplexerTmux
		}
	}
	return MultiplexerTmux // default
}
