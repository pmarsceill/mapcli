package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage agent worktrees",
	Long:  `Commands for listing and cleaning up git worktrees created for agents.`,
}

var worktreeLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List worktrees",
	Long:    `List all git worktrees created for spawned agents.`,
	RunE:    runWorktreeLs,
}

var worktreeCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove orphaned worktrees",
	Long: `Remove git worktrees that are no longer associated with running agents.

By default, only removes orphaned worktrees. Use --all to remove all agent worktrees.`,
	RunE: runWorktreeCleanup,
}

func init() {
	rootCmd.AddCommand(worktreeCmd)
	worktreeCmd.AddCommand(worktreeLsCmd)
	worktreeCmd.AddCommand(worktreeCleanupCmd)

	// cleanup flags
	worktreeCleanupCmd.Flags().String("agent", "", "Remove worktree for a specific agent ID")
	worktreeCleanupCmd.Flags().Bool("all", false, "Remove all agent worktrees (including those with running agents)")
}

func runWorktreeLs(cmd *cobra.Command, args []string) error {
	c, err := client.New(getSocketPath())
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Filter by current repo
	repoRoot := getRepoRoot()
	worktrees, err := c.ListWorktrees(ctx, repoRoot)
	if err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		fmt.Println("no worktrees")
		return nil
	}

	fmt.Printf("%-20s %-15s %s\n", "AGENT ID", "BRANCH", "PATH")
	fmt.Println(strings.Repeat("-", 80))

	for _, wt := range worktrees {
		branch := wt.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Printf("%-20s %-15s %s\n",
			truncate(wt.AgentId, 20),
			truncate(branch, 15),
			wt.Path,
		)
	}

	return nil
}

func runWorktreeCleanup(cmd *cobra.Command, args []string) error {
	agentID, _ := cmd.Flags().GetString("agent")
	all, _ := cmd.Flags().GetBool("all")

	c, err := client.New(getSocketPath())
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := c.CleanupWorktrees(ctx, agentID, all)
	if err != nil {
		return fmt.Errorf("cleanup worktrees: %w", err)
	}

	if resp.RemovedCount == 0 {
		fmt.Println("no worktrees to cleanup")
		return nil
	}

	fmt.Printf("removed %d worktree(s)\n", resp.RemovedCount)
	for _, path := range resp.RemovedPaths {
		fmt.Printf("  - %s\n", path)
	}

	return nil
}
