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
	Owner  string `json:"-"` // Owner login (user or org), populated separately
}

// ghProjectRaw is used for parsing gh project list JSON output
type ghProjectRaw struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Owner  struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type ghProjectListRaw struct {
	Projects []ghProjectRaw `json:"projects"`
}

// Types for GraphQL response when querying linked projects
type ghLinkedProjectsResponse struct {
	Data struct {
		Repository struct {
			ProjectsV2 struct {
				Nodes []struct {
					ID     string `json:"id"`
					Number int    `json:"number"`
					Title  string `json:"title"`
					Owner  struct {
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"nodes"`
			} `json:"projectsV2"`
		} `json:"repository"`
	} `json:"data"`
}

type ghRepoInfo struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
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
1. Finds a GitHub Project by name (searches projects linked to current repo first)
2. Fetches items from the source status column (default: "Todo")
3. Creates tasks for each item found
4. Moves items to the target status column (default: "In Progress")

By default, searches for projects linked to the current repository, which includes
projects owned by organizations. Use --owner to search a specific user/org instead.

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
	taskSyncGHProjectCmd.Flags().StringVar(&syncOwner, "owner", "", "GitHub project owner (user or org); if empty, searches projects linked to current repo")
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

	fmt.Printf("Found project: %s (#%d) owned by %s\n", project.Title, project.Number, project.Owner)

	// Get the Status field and its options (use project's owner)
	statusField, err := getStatusField(project.Number, project.Owner)
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

	// Fetch items from the project (use project's owner)
	items, err := getProjectItems(project.Number, project.Owner)
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

		// Extract GitHub metadata from issue URL
		owner, repo := parseGitHubURL(item.Content.URL)

		// Submit task with GitHub source tracking
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		repoRoot := getRepoRoot()
		task, err := c.SubmitTaskWithGitHub(ctx, description, nil, owner, repo, int32(item.Content.Number), repoRoot)
		cancel()

		if err != nil {
			fmt.Printf("  Error creating task: %v\n", err)
			failed++
			continue
		}

		fmt.Printf("  Created task: %s\n", task.TaskId)
		if owner != "" && repo != "" {
			fmt.Printf("  GitHub source: %s/%s#%d\n", owner, repo, item.Content.Number)
		}

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
	// If no owner specified, try to find projects linked to the current repo first
	if owner == "" {
		project, err := findLinkedProject(name)
		if err == nil {
			return project, nil
		}
		// Fall back to @me if no linked projects found
		owner = "@me"
	}

	return findProjectByOwner(name, owner)
}

// findLinkedProject searches for a project by name among projects linked to the current repository
func findLinkedProject(name string) (*ghProject, error) {
	// Get current repo info
	repoOut, err := exec.Command("gh", "repo", "view", "--json", "owner,name").Output()
	if err != nil {
		return nil, fmt.Errorf("not in a git repository or gh not authenticated")
	}

	var repo ghRepoInfo
	if err := json.Unmarshal(repoOut, &repo); err != nil {
		return nil, fmt.Errorf("parse repo info: %w", err)
	}

	// Query projects linked to this repository via GraphQL
	query := fmt.Sprintf(`query {
		repository(owner: %q, name: %q) {
			projectsV2(first: 20) {
				nodes {
					id
					number
					title
					owner {
						... on Organization { login }
						... on User { login }
					}
				}
			}
		}
	}`, repo.Owner.Login, repo.Name)

	out, err := exec.Command("gh", "api", "graphql", "-f", "query="+query).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh api graphql failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh api graphql failed: %w", err)
	}

	var resp ghLinkedProjectsResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse linked projects: %w", err)
	}

	var available []string
	for _, p := range resp.Data.Repository.ProjectsV2.Nodes {
		available = append(available, fmt.Sprintf("%s (owner: %s)", p.Title, p.Owner.Login))
		if strings.EqualFold(p.Title, name) {
			return &ghProject{
				ID:     p.ID,
				Number: p.Number,
				Title:  p.Title,
				Owner:  p.Owner.Login,
			}, nil
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no projects linked to repository %s/%s", repo.Owner.Login, repo.Name)
	}
	return nil, fmt.Errorf("project %q not found. Projects linked to this repo: %s", name, strings.Join(available, ", "))
}

// findProjectByOwner searches for a project by name using the gh project list command
func findProjectByOwner(name, owner string) (*ghProject, error) {
	args := []string{"project", "list", "--owner", owner, "--format", "json"}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh project list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh project list failed: %w", err)
	}

	var list ghProjectListRaw
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse project list: %w", err)
	}

	var available []string
	for _, p := range list.Projects {
		available = append(available, p.Title)
		if strings.EqualFold(p.Title, name) {
			return &ghProject{
				ID:     p.ID,
				Number: p.Number,
				Title:  p.Title,
				Owner:  p.Owner.Login,
			}, nil
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

	sb.WriteString(fmt.Sprintf("Source: %s\n\n", item.Content.URL))
	sb.WriteString("When you're done with your work and you're confident in your solution, open a PR with the GH CLI.")

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

// parseGitHubURL extracts owner and repo from a GitHub issue URL
// Example: https://github.com/pmarsceill/mapcli/issues/42 -> "pmarsceill", "mapcli"
func parseGitHubURL(url string) (owner, repo string) {
	// Expected format: https://github.com/OWNER/REPO/issues/NUMBER
	parts := strings.Split(url, "/")
	if len(parts) >= 5 && parts[2] == "github.com" {
		return parts[3], parts[4]
	}
	return "", ""
}
