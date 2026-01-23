package client

import (
	"context"
	"fmt"
	"net"
	"time"

	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const DefaultSocketPath = "/tmp/mapd.sock"

// Client is a gRPC client for the daemon
type Client struct {
	conn   *grpc.ClientConn
	daemon mapv1.DaemonServiceClient
}

// New creates a new client connected to the daemon
func New(socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	conn, err := grpc.NewClient(
		"unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}

	// Trigger connection attempt (NewClient doesn't connect immediately)
	conn.Connect()

	return &Client{
		conn:   conn,
		daemon: mapv1.NewDaemonServiceClient(conn),
	}, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// SubmitTask creates a new task
func (c *Client) SubmitTask(ctx context.Context, description string, scopePaths []string) (*mapv1.Task, error) {
	resp, err := c.daemon.SubmitTask(ctx, &mapv1.SubmitTaskRequest{
		Description: description,
		ScopePaths:  scopePaths,
	})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

// SubmitTaskWithGitHub creates a new task with GitHub issue source tracking
func (c *Client) SubmitTaskWithGitHub(ctx context.Context, description string, scopePaths []string, owner, repo string, issueNumber int32) (*mapv1.Task, error) {
	resp, err := c.daemon.SubmitTask(ctx, &mapv1.SubmitTaskRequest{
		Description:       description,
		ScopePaths:        scopePaths,
		GithubOwner:       owner,
		GithubRepo:        repo,
		GithubIssueNumber: issueNumber,
	})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

// ListTasks returns tasks with optional filters
func (c *Client) ListTasks(ctx context.Context, limit int32) ([]*mapv1.Task, error) {
	resp, err := c.daemon.ListTasks(ctx, &mapv1.ListTasksRequest{
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// GetTask retrieves a specific task
func (c *Client) GetTask(ctx context.Context, taskID string) (*mapv1.Task, error) {
	resp, err := c.daemon.GetTask(ctx, &mapv1.GetTaskRequest{
		TaskId: taskID,
	})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

// CancelTask cancels a task
func (c *Client) CancelTask(ctx context.Context, taskID string) (*mapv1.Task, error) {
	resp, err := c.daemon.CancelTask(ctx, &mapv1.CancelTaskRequest{
		TaskId: taskID,
	})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

// RequestInput signals that an agent needs user input
func (c *Client) RequestInput(ctx context.Context, taskID, question string) (*mapv1.RequestInputResponse, error) {
	return c.daemon.RequestInput(ctx, &mapv1.RequestInputRequest{
		TaskId:   taskID,
		Question: question,
	})
}

// GetCurrentTask finds the task for a working directory
func (c *Client) GetCurrentTask(ctx context.Context, workingDir string) (*mapv1.Task, error) {
	resp, err := c.daemon.GetCurrentTask(ctx, &mapv1.GetCurrentTaskRequest{
		WorkingDirectory: workingDir,
	})
	if err != nil {
		return nil, err
	}
	return resp.Task, nil
}

// GetStatus returns daemon status
func (c *Client) GetStatus(ctx context.Context) (*mapv1.GetStatusResponse, error) {
	return c.daemon.GetStatus(ctx, &mapv1.GetStatusRequest{})
}

// Shutdown requests daemon shutdown
func (c *Client) Shutdown(ctx context.Context, force bool) error {
	_, err := c.daemon.Shutdown(ctx, &mapv1.ShutdownRequest{Force: force})
	return err
}

// WatchEvents streams events from the daemon
func (c *Client) WatchEvents(ctx context.Context) (mapv1.DaemonService_WatchEventsClient, error) {
	return c.daemon.WatchEvents(ctx, &mapv1.WatchEventsRequest{})
}

// --- Spawned Agent Methods ---

// SpawnAgent spawns Claude Code agents
func (c *Client) SpawnAgent(ctx context.Context, req *mapv1.SpawnAgentRequest) (*mapv1.SpawnAgentResponse, error) {
	return c.daemon.SpawnAgent(ctx, req)
}

// KillAgent terminates a spawned agent
func (c *Client) KillAgent(ctx context.Context, agentID string, force bool) (*mapv1.KillAgentResponse, error) {
	return c.daemon.KillAgent(ctx, &mapv1.KillAgentRequest{
		AgentId: agentID,
		Force:   force,
	})
}

// ListSpawnedAgents returns all spawned agents
func (c *Client) ListSpawnedAgents(ctx context.Context) ([]*mapv1.SpawnedAgentInfo, error) {
	resp, err := c.daemon.ListSpawnedAgents(ctx, &mapv1.ListSpawnedAgentsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

// RespawnAgent restarts claude in an agent with a dead pane
func (c *Client) RespawnAgent(ctx context.Context, agentID string) (*mapv1.RespawnAgentResponse, error) {
	return c.daemon.RespawnAgent(ctx, &mapv1.RespawnAgentRequest{
		AgentId: agentID,
	})
}

// --- Worktree Methods ---

// ListWorktrees returns all worktrees
func (c *Client) ListWorktrees(ctx context.Context) ([]*mapv1.WorktreeInfo, error) {
	resp, err := c.daemon.ListWorktrees(ctx, &mapv1.ListWorktreesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Worktrees, nil
}

// CleanupWorktrees removes orphaned worktrees
func (c *Client) CleanupWorktrees(ctx context.Context, agentID string, all bool) (*mapv1.CleanupWorktreesResponse, error) {
	return c.daemon.CleanupWorktrees(ctx, &mapv1.CleanupWorktreesRequest{
		AgentId: agentID,
		All:     all,
	})
}

// IsDaemonRunning checks if the daemon is running
func IsDaemonRunning(socketPath string) bool {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}

	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
