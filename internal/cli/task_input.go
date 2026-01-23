package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

var taskInputNeededCmd = &cobra.Command{
	Use:   "input-needed <task-id> <question>",
	Short: "Request user input for a task via GitHub issue",
	Long: `Signal that an agent needs user input by posting a comment to the originating GitHub issue.

This command:
1. Posts a comment to the GitHub issue with the question
2. Sets the task status to WAITING_INPUT
3. The daemon will poll for responses and deliver them to the agent

The task must have originated from a GitHub issue (via 'map task sync gh-project').`,
	Args: cobra.MinimumNArgs(2),
	RunE: runTaskInputNeeded,
}

var taskMyTaskCmd = &cobra.Command{
	Use:   "my-task",
	Short: "Show the current task for this agent",
	Long: `Look up the current task by matching the working directory to an agent worktree.

This is useful for agents to introspect their assigned task.`,
	RunE: runTaskMyTask,
}

func init() {
	taskCmd.AddCommand(taskInputNeededCmd)
	taskCmd.AddCommand(taskMyTaskCmd)
}

func runTaskInputNeeded(cmd *cobra.Command, args []string) error {
	taskID := args[0]
	question := strings.Join(args[1:], " ")

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.RequestInput(ctx, taskID, question)
	if err != nil {
		return fmt.Errorf("request input: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("%s", resp.Message)
	}

	fmt.Println(resp.Message)
	return nil
}

func runTaskMyTask(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	task, err := c.GetCurrentTask(ctx, cwd)
	if err != nil {
		return fmt.Errorf("get current task: %w", err)
	}

	if task == nil {
		fmt.Println("no task found for this working directory")
		return nil
	}

	fmt.Printf("Task ID:     %s\n", task.TaskId)
	fmt.Printf("Status:      %s\n", taskStatusString(task.Status))
	fmt.Printf("Description: %s\n", task.Description)
	fmt.Printf("Assigned To: %s\n", valueOrDash(task.AssignedTo))

	if task.GithubSource != nil {
		fmt.Printf("GitHub:      %s/%s#%d\n",
			task.GithubSource.Owner,
			task.GithubSource.Repo,
			task.GithubSource.IssueNumber)
	}

	if task.WaitingInputQuestion != "" {
		fmt.Printf("\nWaiting for input:\n%s\n", task.WaitingInputQuestion)
	}

	return nil
}
