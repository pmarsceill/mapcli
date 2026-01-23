package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

// GitHub Project data structures for JSON parsing
type ghProject struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type ghProjectList struct {
	Projects []ghProject `json:"projects"`
}

type ghField struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Options []ghFieldOption `json:"options"`
}

type ghFieldOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ghFieldList struct {
	Fields []ghField `json:"fields"`
}

type ghItemContent struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Type   string `json:"type"`
}

type ghItem struct {
	ID      string        `json:"id"`
	Content ghItemContent `json:"content"`
	Status  string        `json:"status"`
}

type ghItemList struct {
	Items []ghItem `json:"items"`
}

var taskSyncCmd = &cobra.Command{
	Use:   "sync <source> <name>",
	Short: "Sync tasks from external sources",
	Long:  `Sync tasks from external sources like GitHub Projects.`,
}

var taskSyncGHProjectCmd = &cobra.Command{
	Use:   "gh-project <project-name>",
	Short: "Sync tasks from a GitHub Project",
	Long: `Fetch issues from a GitHub Project's Todo column and create tasks for them.

This command:
1. Finds a GitHub Project by name
2. Fetches items from the source status column (default: "Todo")
3. Creates tasks for each item found
4. Moves items to the target status column (default: "In Progress")

Requires the 'gh' CLI to be installed and authenticated.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskSyncGHProject,
}

var (
	syncStatusColumn string
	syncTargetColumn string
	syncDryRun       bool
	syncOwner        string
	syncLimit        int
)

func init() {
	taskSyncGHProjectCmd.Flags().StringVar(&syncStatusColumn, "status-column", "Todo", "source status column to sync from")
	taskSyncGHProjectCmd.Flags().StringVar(&syncTargetColumn, "target-column", "In Progress", "target status column after task creation")
	taskSyncGHProjectCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "preview without creating tasks or updating GitHub")
	taskSyncGHProjectCmd.Flags().StringVar(&syncOwner, "owner", "@me", "GitHub project owner (user, org, or @me)")
	taskSyncGHProjectCmd.Flags().IntVar(&syncLimit, "limit", 10, "maximum number of items to sync")

	taskSyncCmd.AddCommand(taskSyncGHProjectCmd)
}

func runTaskSyncGHProject(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	// Check if gh CLI is available
	if err := checkGHCLI(); err != nil {
		return err
	}

	// Find project by name
	project, err := findProject(projectName, syncOwner)
	if err != nil {
		return err
	}

	fmt.Printf("Found project: %s (#%d)\n", project.Title, project.Number)

	// Get the Status field and its options
	statusField, err := getStatusField(project.Number, syncOwner)
	if err != nil {
		return err
	}

	// Find the source and target option IDs
	var sourceOptionID, targetOptionID string
	var availableOptions []string
	for _, opt := range statusField.Options {
		availableOptions = append(availableOptions, opt.Name)
		if opt.Name == syncStatusColumn {
			sourceOptionID = opt.ID
		}
		if opt.Name == syncTargetColumn {
			targetOptionID = opt.ID
		}
	}

	if sourceOptionID == "" {
		return fmt.Errorf("status column %q not found. Available options: %s", syncStatusColumn, strings.Join(availableOptions, ", "))
	}
	if targetOptionID == "" {
		return fmt.Errorf("target column %q not found. Available options: %s", syncTargetColumn, strings.Join(availableOptions, ", "))
	}

	// Fetch items from the project
	items, err := getProjectItems(project.Number, syncOwner)
	if err != nil {
		return err
	}

	// Filter items by status
	var todoItems []ghItem
	for _, item := range items {
		if item.Status == syncStatusColumn && item.Content.Type == "Issue" {
			todoItems = append(todoItems, item)
			if len(todoItems) >= syncLimit {
				break
			}
		}
	}

	if len(todoItems) == 0 {
		fmt.Printf("No items found in %q column\n", syncStatusColumn)
		return nil
	}

	fmt.Printf("Found %d item(s) in %q column\n", len(todoItems), syncStatusColumn)

	if syncDryRun {
		fmt.Println("\n[DRY RUN] Would create the following tasks:")
		for _, item := range todoItems {
			fmt.Printf("  - #%d: %s\n", item.Content.Number, item.Content.Title)
			fmt.Printf("    URL: %s\n", item.Content.URL)
		}
		return nil
	}

	// Connect to daemon
	c, err := client.New(getSocketPath())
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	// Process each item
	var succeeded, failed int
	for _, item := range todoItems {
		fmt.Printf("\nProcessing #%d: %s\n", item.Content.Number, item.Content.Title)

		// Build task description
		description := buildTaskDescription(item)

		// Submit task
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		task, err := c.SubmitTask(ctx, description, nil)
		cancel()

		if err != nil {
			fmt.Printf("  Error creating task: %v\n", err)
			failed++
			continue
		}

		fmt.Printf("  Created task: %s\n", task.TaskId)

		// Update item status on GitHub
		if err := updateItemStatus(project.ID, item.ID, statusField.ID, targetOptionID); err != nil {
			fmt.Printf("  Warning: failed to update GitHub status: %v\n", err)
		} else {
			fmt.Printf("  Moved to %q on GitHub\n", syncTargetColumn)
		}

		succeeded++
	}

	fmt.Printf("\nSync complete: %d succeeded, %d failed\n", succeeded, failed)
	return nil
}

func checkGHCLI() error {
	_, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not found. Install it from https://cli.github.com/")
	}
	return nil
}

func findProject(name, owner string) (*ghProject, error) {
	args := []string{"project", "list", "--owner", owner, "--format", "json"}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh project list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh project list failed: %w", err)
	}

	var list ghProjectList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse project list: %w", err)
	}

	var available []string
	for _, p := range list.Projects {
		available = append(available, p.Title)
		if strings.EqualFold(p.Title, name) {
			return &p, nil
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("project %q not found. No projects available for owner %q", name, owner)
	}
	return nil, fmt.Errorf("project %q not found. Available projects: %s", name, strings.Join(available, ", "))
}

func getStatusField(projectNumber int, owner string) (*ghField, error) {
	args := []string{"project", "field-list", fmt.Sprintf("%d", projectNumber), "--owner", owner, "--format", "json"}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh project field-list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh project field-list failed: %w", err)
	}

	var list ghFieldList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse field list: %w", err)
	}

	for _, f := range list.Fields {
		if f.Name == "Status" && f.Type == "ProjectV2SingleSelectField" {
			return &f, nil
		}
	}

	return nil, fmt.Errorf("status field not found in project; ensure the project has a Status column with single-select options")
}

func getProjectItems(projectNumber int, owner string) ([]ghItem, error) {
	args := []string{"project", "item-list", fmt.Sprintf("%d", projectNumber), "--owner", owner, "--format", "json", "--limit", "100"}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh project item-list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh project item-list failed: %w", err)
	}

	var list ghItemList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse item list: %w", err)
	}

	return list.Items, nil
}

func buildTaskDescription(item ghItem) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("GitHub Issue #%d: %s\n\n", item.Content.Number, item.Content.Title))

	if item.Content.Body != "" {
		sb.WriteString(item.Content.Body)
		sb.WriteString("\n\n")
	}

	sb.WriteString(fmt.Sprintf("Source: %s", item.Content.URL))

	return sb.String()
}

func updateItemStatus(projectID, itemID, fieldID, optionID string) error {
	args := []string{
		"project", "item-edit",
		"--project-id", projectID,
		"--id", itemID,
		"--field-id", fieldID,
		"--single-select-option-id", optionID,
	}

	out, err := exec.Command("gh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}

	return nil
}
