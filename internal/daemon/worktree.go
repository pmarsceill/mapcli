package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WorktreeManager manages git worktrees for spawned agents
type WorktreeManager struct {
	repoRoot    string
	worktreeDir string
	mu          sync.RWMutex
	worktrees   map[string]*Worktree
}

// Worktree represents a git worktree for an agent
type Worktree struct {
	AgentID   string
	Path      string
	Branch    string
	CreatedAt time.Time
	RepoRoot  string // source repository root the worktree was created from
}

// NewWorktreeManager creates a new worktree manager
func NewWorktreeManager(dataDir string) (*WorktreeManager, error) {
	// Find git repo root from current directory
	repoRoot, err := getGitRepoRoot()
	if err != nil {
		// Not in a git repo - that's fine, we'll handle this when operations are attempted
		repoRoot = ""
	}

	worktreeDir := filepath.Join(dataDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		return nil, fmt.Errorf("create worktree dir: %w", err)
	}

	return &WorktreeManager{
		repoRoot:    repoRoot,
		worktreeDir: worktreeDir,
		worktrees:   make(map[string]*Worktree),
	}, nil
}

// Create creates a new worktree for an agent using the manager's default repo root
func (m *WorktreeManager) Create(agentID, branch string) (*Worktree, error) {
	return m.CreateFromRepo(agentID, branch, m.repoRoot)
}

// CreateFromRepo creates a new worktree for an agent from a specific repository
func (m *WorktreeManager) CreateFromRepo(agentID, branch, repoRoot string) (*Worktree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if repoRoot == "" {
		return nil, fmt.Errorf("not in a git repository")
	}

	// Use current branch if none specified
	if branch == "" {
		var err error
		branch, err = getCurrentBranch(repoRoot)
		if err != nil {
			return nil, fmt.Errorf("get current branch: %w", err)
		}
	}

	worktreePath := filepath.Join(m.worktreeDir, agentID)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, fmt.Errorf("worktree already exists for agent %s", agentID)
	}

	// Create the worktree using detached HEAD to avoid branch conflicts
	// First, get the commit SHA for the branch
	commitSHA, err := getCommitSHA(repoRoot, branch)
	if err != nil {
		return nil, fmt.Errorf("get commit SHA for branch %s: %w", branch, err)
	}

	// Create worktree at the commit (detached HEAD)
	cmd := exec.Command("git", "worktree", "add", "--detach", worktreePath, commitSHA)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("create worktree: %s: %w", stderr.String(), err)
	}

	wt := &Worktree{
		AgentID:   agentID,
		Path:      worktreePath,
		Branch:    branch,
		CreatedAt: time.Now(),
		RepoRoot:  repoRoot,
	}

	m.worktrees[agentID] = wt
	return wt, nil
}

// Remove removes a worktree for an agent
func (m *WorktreeManager) Remove(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, exists := m.worktrees[agentID]
	if !exists {
		// Try to find by path pattern
		worktreePath := filepath.Join(m.worktreeDir, agentID)
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			return nil // Already removed
		}
		wt = &Worktree{Path: worktreePath}
	}

	// Remove the worktree using git
	if m.repoRoot != "" {
		cmd := exec.Command("git", "worktree", "remove", "--force", wt.Path)
		cmd.Dir = m.repoRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// If git worktree remove fails, try manual removal
			if removeErr := os.RemoveAll(wt.Path); removeErr != nil {
				return fmt.Errorf("remove worktree: %s: %w", stderr.String(), err)
			}
		}
	} else {
		// No git repo, just remove the directory
		if err := os.RemoveAll(wt.Path); err != nil {
			return fmt.Errorf("remove worktree directory: %w", err)
		}
	}

	delete(m.worktrees, agentID)
	return nil
}

// Get retrieves a worktree by agent ID
func (m *WorktreeManager) Get(agentID string) *Worktree {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.worktrees[agentID]
}

// List returns all tracked worktrees
func (m *WorktreeManager) List() []*Worktree {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Worktree, 0, len(m.worktrees))
	for _, wt := range m.worktrees {
		result = append(result, wt)
	}
	return result
}

// Cleanup removes orphaned worktrees (those without running agents)
func (m *WorktreeManager) Cleanup(runningAgentIDs map[string]bool) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var removed []string

	// Get list of worktree directories
	entries, err := os.ReadDir(m.worktreeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read worktree dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		agentID := entry.Name()

		// Skip if agent is still running
		if runningAgentIDs[agentID] {
			continue
		}

		worktreePath := filepath.Join(m.worktreeDir, agentID)

		// Remove using git if possible
		if m.repoRoot != "" {
			cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
			cmd.Dir = m.repoRoot
			_ = cmd.Run() // Ignore errors, we'll try manual removal
		}

		// Manual removal as fallback
		if err := os.RemoveAll(worktreePath); err != nil {
			continue // Skip this one
		}

		delete(m.worktrees, agentID)
		removed = append(removed, worktreePath)
	}

	return removed, nil
}

// CleanupAgent removes the worktree for a specific agent
func (m *WorktreeManager) CleanupAgent(agentID string) error {
	return m.Remove(agentID)
}

// GetRepoRoot returns the git repository root path
func (m *WorktreeManager) GetRepoRoot() string {
	return m.repoRoot
}

// Helper functions

func getGitRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("not a git repository: %s", stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func getCurrentBranch(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("get current branch: %s", stderr.String())
	}
	branch := strings.TrimSpace(stdout.String())
	if branch == "HEAD" {
		// Detached HEAD state, get the commit SHA instead
		return getHeadCommit(repoRoot)
	}
	return branch, nil
}

func getCommitSHA(repoRoot, ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("resolve ref %s: %s", ref, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func getHeadCommit(repoRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("get HEAD commit: %s", stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}
