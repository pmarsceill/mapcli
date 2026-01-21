package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

var agentMergeCmd = &cobra.Command{
	Use:   "merge <agent-id>",
	Short: "Merge an agent's worktree changes into current branch",
	Long: `Merge changes from an agent's git worktree into your current branch.

This command will:
1. Commit any uncommitted changes in the agent's worktree
2. Merge those changes into your current branch
3. Optionally kill the agent after a successful merge (with -k flag)

Run this from your main repository directory.`,
	Args: cobra.ExactArgs(1),
	RunE: runAgentMerge,
}

var (
	mergeMessage  string
	mergeNoCommit bool
	mergeSquash   bool
	mergeKill     bool
)

func init() {
	agentMergeCmd.Flags().StringVarP(&mergeMessage, "message", "m", "", "commit message for uncommitted changes (default: auto-generated)")
	agentMergeCmd.Flags().BoolVar(&mergeNoCommit, "no-commit", false, "merge without committing (stage changes only)")
	agentMergeCmd.Flags().BoolVar(&mergeSquash, "squash", false, "squash all agent commits into one")
	agentMergeCmd.Flags().BoolVarP(&mergeKill, "kill", "k", false, "kill the agent after successful merge")
	agentCmd.AddCommand(agentMergeCmd)
}

func runAgentMerge(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	// Connect to daemon to get agent info
	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find the agent
	agents, err := c.ListSpawnedAgents(ctx)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	var worktreePath string
	var foundAgent string
	for _, a := range agents {
		if a.GetAgentId() == agentID || strings.HasPrefix(a.GetAgentId(), agentID) {
			worktreePath = a.GetWorktreePath()
			foundAgent = a.GetAgentId()
			break
		}
	}

	if worktreePath == "" {
		return fmt.Errorf("agent %s not found", agentID)
	}

	fmt.Printf("Merging changes from agent %s\n", foundAgent)
	fmt.Printf("Worktree: %s\n", worktreePath)

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree path does not exist: %s", worktreePath)
	}

	// Check if we're in a git repo
	if err := runGitCommand(".", "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("not in a git repository")
	}

	// Check for uncommitted changes in worktree
	hasChanges, err := worktreeHasChanges(worktreePath)
	if err != nil {
		return fmt.Errorf("check worktree status: %w", err)
	}

	if hasChanges {
		fmt.Println("Committing uncommitted changes in worktree...")

		// Stage all changes
		if err := runGitCommand(worktreePath, "add", "-A"); err != nil {
			return fmt.Errorf("stage changes: %w", err)
		}

		// Generate commit message
		commitMsg := mergeMessage
		if commitMsg == "" {
			commitMsg = fmt.Sprintf("Changes from agent %s", foundAgent)
		}

		// Commit
		if err := runGitCommand(worktreePath, "commit", "-m", commitMsg); err != nil {
			return fmt.Errorf("commit changes: %w", err)
		}
		fmt.Println("Changes committed.")
	}

	// Get the worktree's HEAD commit
	headRef, err := getGitOutput(worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("get worktree HEAD: %w", err)
	}
	headRef = strings.TrimSpace(headRef)

	fmt.Printf("Merging commit %s...\n", headRef[:8])

	// Build merge command
	mergeArgs := []string{"merge"}
	if mergeNoCommit {
		mergeArgs = append(mergeArgs, "--no-commit")
	}
	if mergeSquash {
		mergeArgs = append(mergeArgs, "--squash")
	}
	mergeArgs = append(mergeArgs, headRef, "-m", fmt.Sprintf("Merge changes from agent %s", foundAgent))

	// Perform merge
	if err := runGitCommandInteractive(".", mergeArgs...); err != nil {
		return fmt.Errorf("merge failed: %w\n\nYou may need to resolve conflicts manually", err)
	}

	fmt.Println("Merge successful!")

	// Kill the agent if requested
	if mergeKill {
		fmt.Printf("Killing agent %s...\n", foundAgent)
		killCtx, killCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer killCancel()

		resp, err := c.KillAgent(killCtx, foundAgent, false)
		if err != nil {
			return fmt.Errorf("kill agent: %w", err)
		}
		if resp.Success {
			fmt.Printf("Agent %s killed\n", foundAgent)
		} else {
			fmt.Printf("Failed to kill agent: %s\n", resp.Message)
		}
	}

	return nil
}

func worktreeHasChanges(dir string) (bool, error) {
	// Check for staged or unstaged changes
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

func runGitCommand(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runGitCommandInteractive(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
