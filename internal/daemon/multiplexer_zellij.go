package daemon

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// ZellijMultiplexer implements the Multiplexer interface using Zellij
type ZellijMultiplexer struct{}

// NewZellijMultiplexer creates a new Zellij multiplexer
func NewZellijMultiplexer() (*ZellijMultiplexer, error) {
	if _, err := exec.LookPath("zellij"); err != nil {
		return nil, fmt.Errorf("zellij not found in PATH: %w", err)
	}
	return &ZellijMultiplexer{}, nil
}

// Name returns the multiplexer name
func (z *ZellijMultiplexer) Name() string {
	return "zellij"
}

// CreateSession creates a new Zellij session
// Zellij doesn't have a direct equivalent to tmux's new-session with a command,
// so we create a session and then run the command in it
func (z *ZellijMultiplexer) CreateSession(name, workdir, command string) error {
	// Create a detached Zellij session with the specified working directory
	// Using: zellij -s NAME options --default-cwd DIR -- CMD
	cmd := exec.Command("zellij", "-s", name, "options", "--default-cwd", workdir, "--", command)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}
	return nil
}

// KillSession terminates a Zellij session
func (z *ZellijMultiplexer) KillSession(name string) error {
	cmd := exec.Command("zellij", "kill-session", name)
	return cmd.Run()
}

// HasSession checks if a Zellij session exists
func (z *ZellijMultiplexer) HasSession(name string) bool {
	sessions, err := z.ListSessions("")
	if err != nil {
		return false
	}
	return slices.Contains(sessions, name)
}

// ListSessions returns all Zellij sessions with the given prefix
func (z *ZellijMultiplexer) ListSessions(prefix string) ([]string, error) {
	cmd := exec.Command("zellij", "list-sessions", "--short")
	output, err := cmd.Output()
	if err != nil {
		// No sessions is not an error
		return nil, nil
	}

	var sessions []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if prefix == "" || strings.HasPrefix(line, prefix) {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// SendText sends text to a Zellij session
func (z *ZellijMultiplexer) SendText(sessionName, text string) error {
	// zellij -s NAME action write-chars TEXT
	cmd := exec.Command("zellij", "-s", sessionName, "action", "write-chars", text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send text to zellij: %w", err)
	}
	return nil
}

// SendEnter sends an Enter keypress to a Zellij session
func (z *ZellijMultiplexer) SendEnter(sessionName string) error {
	// zellij -s NAME action write 10 (10 is the ASCII code for newline/Enter)
	cmd := exec.Command("zellij", "-s", sessionName, "action", "write", "10")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send Enter to zellij: %w", err)
	}
	return nil
}

// RespawnPane respawns the pane with a new command
// Zellij doesn't have direct pane respawn like tmux, so we close and reopen
func (z *ZellijMultiplexer) RespawnPane(sessionName, command string) error {
	// Zellij doesn't have a direct equivalent to tmux's respawn-pane
	// We can try to run a new command in the session
	// Using: zellij -s NAME run -- CMD
	cmd := exec.Command("zellij", "-s", sessionName, "run", "--", command)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to respawn pane in zellij: %w", err)
	}
	return nil
}

// GetPaneWorkdir returns the working directory
// Zellij doesn't expose this directly, so we return empty string
func (z *ZellijMultiplexer) GetPaneWorkdir(sessionName string) string {
	// Zellij doesn't have a direct way to query pane working directory
	return ""
}

// GetPaneTitle returns the pane title
// Zellij handles this differently than tmux
func (z *ZellijMultiplexer) GetPaneTitle(sessionName string) string {
	// Zellij doesn't have a direct equivalent to query pane title
	return "zellij"
}

// IsPaneDead checks if the pane's process has exited
// Zellij doesn't have a direct equivalent to tmux's pane_dead
func (z *ZellijMultiplexer) IsPaneDead(sessionName string) bool {
	// Zellij doesn't expose pane dead status directly
	// We can check if the session still exists
	return !z.HasSession(sessionName)
}

// AttachCommand returns an exec.Cmd that attaches to the session
func (z *ZellijMultiplexer) AttachCommand(sessionName string) *exec.Cmd {
	return exec.Command("zellij", "attach", sessionName)
}

// ConfigureSession applies configuration options to a Zellij session
// Zellij uses config files rather than runtime options, so this is limited
func (z *ZellijMultiplexer) ConfigureSession(sessionName string, opts SessionOptions) error {
	// Zellij configuration is primarily done through config files
	// Runtime configuration options are limited compared to tmux
	// Most styling and behavior is set in the Zellij config file (~/.config/zellij/config.kdl)
	return nil
}
