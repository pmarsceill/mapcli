package daemon

import (
	"fmt"
	"os/exec"
	"strings"
)

// TmuxMultiplexer implements the Multiplexer interface using tmux
type TmuxMultiplexer struct{}

// NewTmuxMultiplexer creates a new tmux multiplexer
func NewTmuxMultiplexer() (*TmuxMultiplexer, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux not found in PATH: %w", err)
	}
	return &TmuxMultiplexer{}, nil
}

// Name returns the multiplexer name
func (t *TmuxMultiplexer) Name() string {
	return "tmux"
}

// CreateSession creates a new tmux session
func (t *TmuxMultiplexer) CreateSession(name, workdir, command string) error {
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", workdir, command)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}
	return nil
}

// KillSession terminates a tmux session
func (t *TmuxMultiplexer) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	return cmd.Run()
}

// HasSession checks if a tmux session exists
func (t *TmuxMultiplexer) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// ListSessions returns all tmux sessions with the given prefix
func (t *TmuxMultiplexer) ListSessions(prefix string) ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		// No sessions is not an error
		return nil, nil
	}

	var sessions []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(line, prefix) {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// SendText sends text to a tmux session using literal mode
func (t *TmuxMultiplexer) SendText(sessionName, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "-l", text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send text to tmux: %w", err)
	}
	return nil
}

// SendEnter sends an Enter keypress to a tmux session
func (t *TmuxMultiplexer) SendEnter(sessionName string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "Enter")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to send Enter to tmux: %w", err)
	}
	return nil
}

// RespawnPane respawns the pane with a new command
func (t *TmuxMultiplexer) RespawnPane(sessionName, command string) error {
	cmd := exec.Command("tmux", "respawn-pane", "-t", sessionName, "-k", command)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to respawn pane: %w", err)
	}
	return nil
}

// GetPaneWorkdir returns the current working directory of a tmux pane
func (t *TmuxMultiplexer) GetPaneWorkdir(sessionName string) string {
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{pane_current_path}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetPaneTitle returns the pane title of a tmux session
func (t *TmuxMultiplexer) GetPaneTitle(sessionName string) string {
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

// IsPaneDead checks if the pane's process has exited
func (t *TmuxMultiplexer) IsPaneDead(sessionName string) bool {
	cmd := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{pane_dead}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "1"
}

// AttachCommand returns an exec.Cmd that attaches to the session
func (t *TmuxMultiplexer) AttachCommand(sessionName string) *exec.Cmd {
	return exec.Command("tmux", "attach", "-t", sessionName)
}

// ConfigureSession applies configuration options to a tmux session
func (t *TmuxMultiplexer) ConfigureSession(sessionName string, opts SessionOptions) error {
	// Enable mouse scrolling
	if opts.MouseEnabled {
		_ = exec.Command("tmux", "set-option", "-t", sessionName, "mouse", "on").Run()
	}

	// Enable remain-on-exit to keep pane open if agent exits
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "remain-on-exit", "on").Run()

	// Store the CLI command for respawn keybinding
	if opts.CLICommand != "" {
		_ = exec.Command("tmux", "set-option", "-t", sessionName, "@map_cli_cmd", opts.CLICommand).Run()
		_ = exec.Command("tmux", "bind-key", "-t", sessionName, "R", "respawn-pane", "-k", opts.CLICommand).Run()
	}

	// Add agent ID to the status-right for easy identification
	if opts.AgentID != "" {
		statusRight := fmt.Sprintf(" [%s] %%H %%H:%%M %%d-%%b-%%y", opts.AgentID)
		_ = exec.Command("tmux", "set-option", "-t", sessionName, "status-right", statusRight).Run()
	}

	// Apply a subtle theme (neutral grays that work on both dark and light terminals)
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "status-style", "bg=colour240,fg=colour255").Run()
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "status-left-style", "bg=colour243,fg=colour255").Run()
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "status-right-style", "bg=colour243,fg=colour255").Run()
	_ = exec.Command("tmux", "set-option", "-t", sessionName, "window-status-current-style", "bg=colour245,fg=colour232,bold").Run()

	return nil
}
