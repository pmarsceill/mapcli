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

var taskCmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"tasks", "t"},
	Short:   "Task management commands",
	Long:    `Commands for creating and managing tasks.`,
}

var taskSubmitCmd = &cobra.Command{
	Use:   "submit <description>",
	Short: "Submit a new task",
	Long:  `Create and submit a new task for agent processing.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTaskSubmit,
}

var taskListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List tasks",
	Long:    `List all tasks with their current status.`,
	RunE:    runTaskList,
}

var taskShowCmd = &cobra.Command{
	Use:   "show <task-id>",
	Short: "Show task details",
	Long:  `Display detailed information about a specific task.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskShow,
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel <task-id>",
	Short: "Cancel a task",
	Long:  `Cancel a pending or in-progress task.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskCancel,
}

var (
	taskLimit int32
	taskPaths []string
)

func init() {
	taskSubmitCmd.Flags().StringSliceVarP(&taskPaths, "path", "p", nil, "scope paths for the task")
	taskListCmd.Flags().Int32VarP(&taskLimit, "limit", "n", 20, "maximum number of tasks to show")

	taskCmd.AddCommand(taskSubmitCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskShowCmd)
	taskCmd.AddCommand(taskCancelCmd)
	taskCmd.AddCommand(taskSyncCmd)
	rootCmd.AddCommand(taskCmd)
}

func runTaskSubmit(cmd *cobra.Command, args []string) error {
	description := strings.Join(args, " ")

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	task, err := c.SubmitTask(ctx, description, taskPaths)
	if err != nil {
		return fmt.Errorf("submit task: %w", err)
	}

	fmt.Printf("task created: %s\n", task.TaskId)
	return nil
}

func runTaskList(cmd *cobra.Command, args []string) error {
	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tasks, err := c.ListTasks(ctx, taskLimit)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return nil
	}

	fmt.Printf("%-36s %-15s %-20s %s\n", "TASK ID", "STATUS", "ASSIGNED TO", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 100))

	for _, task := range tasks {
		assignedTo := task.AssignedTo
		if assignedTo == "" {
			assignedTo = "-"
		}
		fmt.Printf("%-36s %-15s %-20s %s\n",
			task.TaskId,
			taskStatusString(task.Status),
			truncate(assignedTo, 20),
			truncate(task.Description, 40),
		)
	}

	return nil
}

func runTaskShow(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	task, err := c.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}

	fmt.Printf("Task ID:     %s\n", task.TaskId)
	fmt.Printf("Status:      %s\n", taskStatusString(task.Status))
	fmt.Printf("Description: %s\n", task.Description)
	fmt.Printf("Assigned To: %s\n", valueOrDash(task.AssignedTo))
	fmt.Printf("Created:     %s\n", task.CreatedAt.AsTime().Local().Format(time.RFC3339))
	fmt.Printf("Updated:     %s\n", task.UpdatedAt.AsTime().Local().Format(time.RFC3339))

	if len(task.ScopePaths) > 0 {
		fmt.Printf("Scope Paths: %s\n", strings.Join(task.ScopePaths, ", "))
	}
	if task.Error != "" {
		fmt.Printf("\n--- Error ---\n%s\n", task.Error)
	}
	if task.Result != "" {
		fmt.Printf("\n--- Output ---\n%s\n", task.Result)
	}

	return nil
}

func runTaskCancel(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	task, err := c.CancelTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}

	fmt.Printf("task cancelled: %s (status: %s)\n", task.TaskId, taskStatusString(task.Status))
	return nil
}

func taskStatusString(s mapv1.TaskStatus) string {
	switch s {
	case mapv1.TaskStatus_TASK_STATUS_PENDING:
		return "pending"
	case mapv1.TaskStatus_TASK_STATUS_OFFERED:
		return "offered"
	case mapv1.TaskStatus_TASK_STATUS_ACCEPTED:
		return "accepted"
	case mapv1.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return "in_progress"
	case mapv1.TaskStatus_TASK_STATUS_COMPLETED:
		return "completed"
	case mapv1.TaskStatus_TASK_STATUS_FAILED:
		return "failed"
	case mapv1.TaskStatus_TASK_STATUS_CANCELLED:
		return "cancelled"
	case mapv1.TaskStatus_TASK_STATUS_WAITING_INPUT:
		return "waiting_input"
	default:
		return "unknown"
	}
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
