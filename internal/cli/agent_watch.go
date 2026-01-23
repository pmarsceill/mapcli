package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/pmarsceill/mapcli/internal/daemon"
	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"github.com/spf13/cobra"
)

var agentWatchCmd = &cobra.Command{
	Use:   "watch [agent-id]",
	Short: "Attach to an agent's terminal session",
	Long: `Attach to a spawned agent's terminal multiplexer session for full interactivity.

You can accept tools, approve changes, and interact with Claude directly.

For tmux (default):
  - Ctrl+B d    Detach from session (keeps agent running)
  - Ctrl+B n    Next session (if multiple agents)
  - Ctrl+B p    Previous session
  - Ctrl+B s    List all sessions

For Zellij (when multiplexer=zellij):
  - Ctrl+O d    Detach from session
  - Alt+n/p     Next/previous pane

If no agent-id is specified, attaches to the first available agent.

Use --all to view multiple agents in a tiled layout (up to 6 agents, tmux only).`,
	RunE: runAgentWatch,
}

var watchAllFlag bool

func init() {
	agentCmd.AddCommand(agentWatchCmd)
	agentWatchCmd.Flags().BoolVarP(&watchAllFlag, "all", "a", false, "View all agents in a tiled tmux layout (up to 6)")
}

func runAgentWatch(cmd *cobra.Command, args []string) error {
	// Detect multiplexer type from config
	muxType := daemon.MultiplexerType(getMultiplexer())

	// Check if the multiplexer is available
	muxBinary := string(muxType)
	muxPath, err := exec.LookPath(muxBinary)
	if err != nil {
		return fmt.Errorf("%s not found in PATH - required for agent watch", muxBinary)
	}

	c, err := client.New(getSocketPath())
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get list of spawned agents
	agents, err := c.ListSpawnedAgents(ctx)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	if len(agents) == 0 {
		return fmt.Errorf("no spawned agents found - create one with 'map agent create'")
	}

	// Handle --all flag for tiled view (tmux only for now)
	if watchAllFlag {
		if muxType != daemon.MultiplexerTmux {
			return fmt.Errorf("--all flag is only supported with tmux multiplexer")
		}
		return runAgentWatchAll(agents)
	}

	// Find target agent
	var targetSession string
	var targetAgent string

	if len(args) > 0 {
		// Find agent by ID (supports partial match)
		targetID := args[0]
		for _, a := range agents {
			if a.GetAgentId() == targetID || strings.HasPrefix(a.GetAgentId(), targetID) {
				targetAgent = a.GetAgentId()
				targetSession = a.GetLogFile() // LogFile field repurposed to hold session name
				break
			}
		}
		if targetSession == "" {
			return fmt.Errorf("agent %s not found", targetID)
		}
	} else {
		// Use first agent
		targetAgent = agents[0].GetAgentId()
		targetSession = agents[0].GetLogFile()
	}

	// Create multiplexer instance for session operations
	mux, err := daemon.NewMultiplexer(muxType)
	if err != nil {
		return fmt.Errorf("init multiplexer: %w", err)
	}

	// Verify session exists
	if !mux.HasSession(targetSession) {
		return fmt.Errorf("%s session %s not found - agent may have crashed", muxType, targetSession)
	}

	// For tmux, check if the pane is dead (claude exited but session preserved)
	if muxType == daemon.MultiplexerTmux && mux.IsPaneDead(targetSession) {
		fmt.Printf("Agent %s pane is dead (claude exited).\n", targetAgent)
		fmt.Print("Respawn claude? [Y/n] ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "" || response == "y" || response == "yes" {
			// Respawn via daemon
			resp, err := c.RespawnAgent(ctx, targetAgent)
			if err != nil {
				return fmt.Errorf("respawn agent: %w", err)
			}
			if !resp.Success {
				return fmt.Errorf("respawn failed: %s", resp.Message)
			}
			fmt.Println("Claude respawned.")
			// Give it a moment to start
			time.Sleep(300 * time.Millisecond)
		}
	}

	fmt.Printf("Attaching to agent %s (%s session: %s)\n", targetAgent, muxType, targetSession)
	fmt.Println()
	if muxType == daemon.MultiplexerTmux {
		fmt.Println("  Ctrl+B d     Detach (keeps agent running)")
		fmt.Println("  Ctrl+C       Interrupts claude (session preserved)")
		fmt.Println("  Ctrl+B n/p   Switch agents")
	} else {
		fmt.Println("  Ctrl+O d     Detach (keeps agent running)")
		fmt.Println("  Ctrl+C       Interrupts claude")
		fmt.Println("  Alt+n/p      Navigate panes")
	}
	fmt.Println()

	// Attach to the session using the multiplexer's attach command
	attachCmd := mux.AttachCommand(targetSession)
	if attachCmd == nil {
		// Fallback to direct command
		attachCmd = exec.Command(muxPath, "attach", "-t", targetSession)
	}
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr

	return attachCmd.Run()
}

const watchAllSessionName = "map-watch-all"
const maxWatchAgents = 6

func runAgentWatchAll(agents []*mapv1.SpawnedAgentInfo) error {
	// Limit to maxWatchAgents agents
	if len(agents) > maxWatchAgents {
		fmt.Printf("Showing first %d of %d agents\n", maxWatchAgents, len(agents))
		agents = agents[:maxWatchAgents]
	}

	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}

	// Verify all agent sessions exist
	var validAgents []*mapv1.SpawnedAgentInfo
	for _, a := range agents {
		checkCmd := exec.Command(tmuxPath, "has-session", "-t", a.GetLogFile())
		if err := checkCmd.Run(); err == nil {
			validAgents = append(validAgents, a)
		}
	}

	if len(validAgents) == 0 {
		return fmt.Errorf("no valid tmux sessions found - agents may have crashed")
	}

	// Configure inner agent sessions: enable mouse and customize status bar
	for _, a := range validAgents {
		_ = exec.Command(tmuxPath, "set-option", "-t", a.GetLogFile(), "mouse", "on").Run()
		// Show agent ID on left side of status bar
		agentLabel := fmt.Sprintf(" %s ", a.GetAgentId())
		_ = exec.Command(tmuxPath, "set-option", "-t", a.GetLogFile(), "status-left-length", "50").Run()
		_ = exec.Command(tmuxPath, "set-option", "-t", a.GetLogFile(), "status-left", agentLabel).Run()
		// Hide right side of status bar (timestamp)
		_ = exec.Command(tmuxPath, "set-option", "-t", a.GetLogFile(), "status-right", "").Run()
		// Hide window list (the "0:fish*" text)
		_ = exec.Command(tmuxPath, "set-window-option", "-t", a.GetLogFile(), "window-status-format", "").Run()
		_ = exec.Command(tmuxPath, "set-window-option", "-t", a.GetLogFile(), "window-status-current-format", "").Run()
	}

	// Kill existing watch-all session if it exists
	_ = exec.Command(tmuxPath, "kill-session", "-t", watchAllSessionName).Run()

	// Create new session with first agent
	// Use TMUX= to allow nested tmux attach
	firstSession := validAgents[0].GetLogFile()
	attachScript := fmt.Sprintf("TMUX= exec tmux attach -t %s", firstSession)
	createCmd := exec.Command(tmuxPath, "new-session", "-d", "-s", watchAllSessionName, "sh", "-c", attachScript)
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("create watch session: %w", err)
	}

	// Hide status bar on outer watch session
	_ = exec.Command(tmuxPath, "set-option", "-t", watchAllSessionName, "status", "off").Run()

	// Add panes for remaining agents
	for i := 1; i < len(validAgents); i++ {
		agentSession := validAgents[i].GetLogFile()
		attachScript := fmt.Sprintf("TMUX= exec tmux attach -t %s", agentSession)

		// Split window and run attach command
		splitCmd := exec.Command(tmuxPath, "split-window", "-t", watchAllSessionName, "sh", "-c", attachScript)
		if err := splitCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to add pane for agent %s: %v\n", validAgents[i].GetAgentId(), err)
			continue
		}

		// Apply tiled layout after each split to keep things balanced
		_ = exec.Command(tmuxPath, "select-layout", "-t", watchAllSessionName, "tiled").Run()
	}

	// Final layout adjustment for 3-per-row arrangement
	// For 4-6 agents, use main-horizontal with proper sizing
	if len(validAgents) >= 4 && len(validAgents) <= 6 {
		// Use tiled which gives a reasonable 2-row layout
		_ = exec.Command(tmuxPath, "select-layout", "-t", watchAllSessionName, "tiled").Run()
	}

	fmt.Printf("Watching %d agents in tiled view\n", len(validAgents))
	fmt.Println("Use Ctrl+B d to detach, Ctrl+B arrow keys to navigate panes")
	fmt.Println()

	// Attach to the watch-all session
	attachCmd := exec.Command(tmuxPath, "attach", "-t", watchAllSessionName)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr

	return attachCmd.Run()
}
