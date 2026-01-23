package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:     "agents",
	Aliases: []string{"ag"},
	Short:   "List spawned agents",
	Long:    `List all agents spawned by the daemon (claude and codex).`,
	RunE:    runAgents,
}

func init() {
	rootCmd.AddCommand(agentsCmd)
}

func runAgents(cmd *cobra.Command, args []string) error {
	c, err := client.New(getSocketPath())
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
