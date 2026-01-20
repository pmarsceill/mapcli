package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestWorktreeManager(t *testing.T) (*WorktreeManager, string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "mapd-worktree-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	mgr, err := NewWorktreeManager(tempDir)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("create worktree manager: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	return mgr, tempDir, cleanup
}

// initTestGitRepo initializes a git repository for testing
func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Skipf("git init failed (git may not be available): %v", err)
	}

	// Configure git user (required for commits)
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	_ = cmd.Run()

	// Create initial commit
	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func TestNewWorktreeManager(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mapd-worktree-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	mgr, err := NewWorktreeManager(tempDir)
	if err != nil {
		t.Fatalf("NewWorktreeManager failed: %v", err)
	}

	if mgr == nil {
		t.Fatal("NewWorktreeManager returned nil")
	}

	// Verify worktrees directory was created
	worktreesDir := filepath.Join(tempDir, "worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		t.Error("worktrees directory was not created")
	}
}

func TestNewWorktreeManager_CreatesDataDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mapd-worktree-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Use a nested path that doesn't exist
	dataDir := filepath.Join(tempDir, "nested", "data", "dir")

	mgr, err := NewWorktreeManager(dataDir)
	if err != nil {
		t.Fatalf("NewWorktreeManager failed: %v", err)
	}

	if mgr == nil {
		t.Fatal("NewWorktreeManager returned nil")
	}

	worktreesDir := filepath.Join(dataDir, "worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		t.Error("nested worktrees directory was not created")
	}
}

func TestWorktreeManager_Get_NotTracked(t *testing.T) {
	mgr, _, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	wt := mgr.Get("nonexistent")
	if wt != nil {
		t.Error("Get should return nil for untracked worktree")
	}
}

func TestWorktreeManager_List_Empty(t *testing.T) {
	mgr, _, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	worktrees := mgr.List()
	if len(worktrees) != 0 {
		t.Errorf("List returned %d worktrees, want 0", len(worktrees))
	}
}

func TestWorktreeManager_GetRepoRoot(t *testing.T) {
	mgr, _, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	// The repo root might be empty if not in a git repo, or set if we are
	// Either way, the method should not panic
	_ = mgr.GetRepoRoot()
}

func TestWorktreeManager_Create_NotInGitRepo(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mapd-worktree-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Change to temp dir (not a git repo)
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(tempDir)

	mgr, err := NewWorktreeManager(tempDir)
	if err != nil {
		t.Fatalf("NewWorktreeManager failed: %v", err)
	}

	// Creating a worktree should fail when not in a git repo
	_, err = mgr.Create("agent-1", "")
	if err == nil {
		t.Error("Create should fail when not in a git repo")
	}
}

func TestWorktreeManager_Remove_NonExistent(t *testing.T) {
	mgr, _, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	// Should not error for non-existent worktree
	err := mgr.Remove("nonexistent")
	if err != nil {
		t.Errorf("Remove should not error for non-existent worktree: %v", err)
	}
}

func TestWorktreeManager_CleanupAgent(t *testing.T) {
	mgr, _, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	// Should not error for non-existent agent
	err := mgr.CleanupAgent("nonexistent")
	if err != nil {
		t.Errorf("CleanupAgent should not error for non-existent agent: %v", err)
	}
}

func TestWorktreeManager_Cleanup_EmptyDir(t *testing.T) {
	mgr, _, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	runningAgents := map[string]bool{}
	removed, err := mgr.Cleanup(runningAgents)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("Cleanup removed %d paths, want 0", len(removed))
	}
}

func TestWorktreeManager_Cleanup_SkipsRunningAgents(t *testing.T) {
	mgr, tempDir, cleanup := setupTestWorktreeManager(t)
	defer cleanup()

	// Create some fake worktree directories
	worktreesDir := filepath.Join(tempDir, "worktrees")
	agentDirs := []string{"agent-1", "agent-2", "agent-3"}
	for _, dir := range agentDirs {
		if err := os.MkdirAll(filepath.Join(worktreesDir, dir), 0755); err != nil {
			t.Fatalf("create agent dir: %v", err)
		}
	}

	// Mark agent-1 and agent-2 as running
	runningAgents := map[string]bool{
		"agent-1": true,
		"agent-2": true,
	}

	removed, err := mgr.Cleanup(runningAgents)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Should only remove agent-3
	if len(removed) != 1 {
		t.Errorf("Cleanup removed %d paths, want 1", len(removed))
	}

	// Verify agent-1 and agent-2 still exist
	for _, agent := range []string{"agent-1", "agent-2"} {
		path := filepath.Join(worktreesDir, agent)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Running agent directory %s should not be removed", agent)
		}
	}

	// Verify agent-3 is removed
	agent3Path := filepath.Join(worktreesDir, "agent-3")
	if _, err := os.Stat(agent3Path); !os.IsNotExist(err) {
		t.Error("Non-running agent directory should be removed")
	}
}

// Integration tests that require a git repository
// These tests create a temporary git repo for testing

func TestWorktreeManager_CreateAndRemove_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a temporary directory for the git repo
	repoDir, err := os.MkdirTemp("", "mapd-git-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(repoDir) }()

	// Initialize git repo
	initTestGitRepo(t, repoDir)

	// Create data directory for worktrees
	dataDir, err := os.MkdirTemp("", "mapd-data-test-*")
	if err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	// Change to repo directory so NewWorktreeManager finds it
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(repoDir)

	mgr, err := NewWorktreeManager(dataDir)
	if err != nil {
		t.Fatalf("NewWorktreeManager failed: %v", err)
	}

	if mgr.GetRepoRoot() == "" {
		t.Skip("could not detect git repo root")
	}

	// Create a worktree
	wt, err := mgr.Create("test-agent", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if wt == nil {
		t.Fatal("Create returned nil worktree")
	}

	if wt.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", wt.AgentID, "test-agent")
	}

	// Verify worktree path exists
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		t.Error("worktree path does not exist")
	}

	// Get should return the worktree
	got := mgr.Get("test-agent")
	if got == nil {
		t.Error("Get returned nil for created worktree")
	}

	// List should include the worktree
	list := mgr.List()
	if len(list) != 1 {
		t.Errorf("List returned %d worktrees, want 1", len(list))
	}

	// Remove the worktree
	err = mgr.Remove("test-agent")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify worktree is removed
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Error("worktree path should be removed")
	}

	// Get should return nil
	got = mgr.Get("test-agent")
	if got != nil {
		t.Error("Get should return nil after removal")
	}
}

func TestWorktreeManager_Create_AlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a temporary directory for the git repo
	repoDir, err := os.MkdirTemp("", "mapd-git-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(repoDir) }()

	// Initialize git repo
	initTestGitRepo(t, repoDir)

	// Create data directory for worktrees
	dataDir, err := os.MkdirTemp("", "mapd-data-test-*")
	if err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	// Change to repo directory
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()
	_ = os.Chdir(repoDir)

	mgr, err := NewWorktreeManager(dataDir)
	if err != nil {
		t.Fatalf("NewWorktreeManager failed: %v", err)
	}

	if mgr.GetRepoRoot() == "" {
		t.Skip("could not detect git repo root")
	}

	// Create a worktree
	_, err = mgr.Create("test-agent", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Try to create again - should fail
	_, err = mgr.Create("test-agent", "")
	if err == nil {
		t.Error("Create should fail when worktree already exists")
	}
}
