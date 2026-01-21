package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage spawned agents (Claude or Codex)",
	Long:  `Commands for spawning, listing, and killing Claude Code or OpenAI Codex agents.`,
}

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Spawn agents (Claude or Codex)",
	Long: `Spawn one or more agents as subprocesses.

Use -a claude (default) for Claude Code agents or -a codex for OpenAI Codex agents.
Each agent can optionally be isolated in its own git worktree for safe
concurrent work in the same repository.`,
	RunE: runAgentCreate,
}

var agentListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List spawned agents",
	Long:    `List all spawned Claude Code agents and their status.`,
	RunE:    runAgentList,
}

var agentKillCmd = &cobra.Command{
	Use:   "kill [agent-id]",
	Short: "Terminate a spawned agent",
	Long: `Terminate a spawned agent by its ID, or kill all agents with --all.

Examples:
  map agent kill claude-abc123     # Kill a specific agent
  map agent kill -a                # Kill all agents
  map agent kill --all --force     # Force kill all agents`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentKill,
}

var agentRespawnCmd = &cobra.Command{
	Use:   "respawn <agent-id>",
	Short: "Restart claude in a dead agent pane",
	Long: `Restart the claude process in an agent whose tmux pane is dead.

When you press Ctrl+C in an agent session, the claude process exits but
the tmux pane is preserved. Use this command to restart claude in that
agent and continue where you left off.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentRespawn,
}

func init() {
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentKillCmd)
	agentCmd.AddCommand(agentRespawnCmd)

	// agent create flags
	agentCreateCmd.Flags().IntP("count", "n", 1, "Number of agents to spawn")
	agentCreateCmd.Flags().String("branch", "", "Git branch for worktrees (default: current branch)")
	agentCreateCmd.Flags().Bool("worktree", true, "Use worktree isolation for each agent")
	agentCreateCmd.Flags().Bool("no-worktree", false, "Skip worktree isolation (all agents share cwd)")
	agentCreateCmd.Flags().String("name", "", "Agent name prefix (default: agent type)")
	agentCreateCmd.Flags().StringP("prompt", "p", "", "Initial prompt to send to the agent")
	agentCreateCmd.Flags().StringP("agent-type", "a", "claude", "Agent type: claude (default) or codex")
	agentCreateCmd.Flags().Bool("require-permissions", false, "Require permission prompts (default: permissions are skipped for autonomous operation)")

	// agent kill flags
	agentKillCmd.Flags().BoolP("force", "f", false, "Force kill (SIGKILL instead of SIGTERM)")
	agentKillCmd.Flags().BoolP("all", "a", false, "Kill all running agents")
}

func runAgentCreate(cmd *cobra.Command, args []string) error {
	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	count, _ := cmd.Flags().GetInt("count")
	branch, _ := cmd.Flags().GetString("branch")
	noWorktree, _ := cmd.Flags().GetBool("no-worktree")
	worktree, _ := cmd.Flags().GetBool("worktree")
	name, _ := cmd.Flags().GetString("name")
	prompt, _ := cmd.Flags().GetString("prompt")
	agentType, _ := cmd.Flags().GetString("agent-type")
	requirePermissions, _ := cmd.Flags().GetBool("require-permissions")

	// Validate agent type
	if agentType != "claude" && agentType != "codex" {
		return fmt.Errorf("invalid agent type %q: must be 'claude' or 'codex'", agentType)
	}

	// no-worktree overrides worktree
	useWorktree := worktree && !noWorktree

	// Skip permissions by default for autonomous operation (inverted from require-permissions flag)
	skipPermissions := !requirePermissions

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := &mapv1.SpawnAgentRequest{
		Count:           int32(count),
		Branch:          branch,
		UseWorktree:     useWorktree,
		NamePrefix:      name,
		Prompt:          prompt,
		AgentType:       agentType,
		SkipPermissions: skipPermissions,
	}

	resp, err := c.SpawnAgent(ctx, req)
	if err != nil {
		return fmt.Errorf("spawn agent: %w", err)
	}

	if len(resp.Agents) == 0 {
		fmt.Println("no agents spawned")
		return nil
	}

	fmt.Printf("spawned %d agent(s):\n\n", len(resp.Agents))
	fmt.Printf("%-25s %-8s %s\n", "AGENT ID", "TYPE", "WORKTREE")
	fmt.Println(strings.Repeat("-", 75))

	for _, agent := range resp.Agents {
		worktreePath := agent.WorktreePath
		if worktreePath == "" {
			worktreePath = "(none)"
		}
		agentTypeDisplay := agent.AgentType
		if agentTypeDisplay == "" {
			agentTypeDisplay = "claude"
		}
		fmt.Printf("%-25s %-8s %s\n",
			truncate(agent.AgentId, 25),
			agentTypeDisplay,
			worktreePath,
		)
	}

	return nil
}

func runAgentList(cmd *cobra.Command, args []string) error {
	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agents, err := c.ListSpawnedAgents(ctx)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	if len(agents) == 0 {
		fmt.Println("no agents spawned")
		return nil
	}

	fmt.Printf("%-25s %-8s %s\n", "AGENT ID", "TYPE", "WORKTREE")
	fmt.Println(strings.Repeat("-", 75))

	for _, agent := range agents {
		fmt.Printf("%-25s %-8s %s\n",
			truncate(agent.AgentId, 25),
			agent.AgentType,
			truncate(agent.WorktreePath, 40),
		)
	}

	return nil
}

func runAgentKill(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	killAll, _ := cmd.Flags().GetBool("all")

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Handle --all flag
	if killAll {
		agents, err := c.ListSpawnedAgents(ctx)
		if err != nil {
			return fmt.Errorf("list agents: %w", err)
		}

		if len(agents) == 0 {
			fmt.Println("no agents to kill")
			return nil
		}

		fmt.Printf("Killing %d agent(s)...\n", len(agents))
		var failed int
		for _, agent := range agents {
			resp, err := c.KillAgent(ctx, agent.GetAgentId(), force)
			if err != nil {
				fmt.Printf("  failed to kill %s: %v\n", agent.GetAgentId(), err)
				failed++
				continue
			}
			if resp.Success {
				fmt.Printf("  killed %s\n", agent.GetAgentId())
			} else {
				fmt.Printf("  failed to kill %s: %s\n", agent.GetAgentId(), resp.Message)
				failed++
			}
		}

		if failed > 0 {
			return fmt.Errorf("failed to kill %d agent(s)", failed)
		}
		fmt.Println("All agents killed")
		return nil
	}

	// Single agent kill requires an argument
	if len(args) == 0 {
		return fmt.Errorf("agent ID required (or use --all to kill all agents)")
	}

	agentID := args[0]

	// Resolve partial agent ID
	resolvedID, err := resolveAgentID(ctx, c, agentID)
	if err != nil {
		return err
	}

	resp, err := c.KillAgent(ctx, resolvedID, force)
	if err != nil {
		return fmt.Errorf("kill agent: %w", err)
	}

	if resp.Success {
		fmt.Printf("agent %s killed\n", resolvedID)
	} else {
		fmt.Printf("failed to kill agent: %s\n", resp.Message)
	}

	return nil
}

func runAgentRespawn(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Resolve partial agent ID
	resolvedID, err := resolveAgentID(ctx, c, agentID)
	if err != nil {
		return err
	}

	resp, err := c.RespawnAgent(ctx, resolvedID)
	if err != nil {
		return fmt.Errorf("respawn agent: %w", err)
	}

	if resp.Success {
		fmt.Printf("respawned claude in agent %s\n", resolvedID)
		fmt.Println("use 'map agent watch' to attach to the session")
	} else {
		fmt.Printf("failed to respawn agent: %s\n", resp.Message)
	}

	return nil
}

// resolveAgentID finds an agent by exact or partial ID match
func resolveAgentID(ctx context.Context, c *client.Client, agentID string) (string, error) {
	agents, err := c.ListSpawnedAgents(ctx)
	if err != nil {
		return "", fmt.Errorf("list agents: %w", err)
	}

	for _, a := range agents {
		if a.GetAgentId() == agentID || strings.HasPrefix(a.GetAgentId(), agentID) {
			return a.GetAgentId(), nil
		}
	}

	return "", fmt.Errorf("agent %s not found", agentID)
}
