package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	DefaultSocketPath = "/tmp/mapd.sock"
	DefaultDataDir    = "~/.mapd"
)

// Server is the main daemon server
type Server struct {
	mapv1.UnimplementedDaemonServiceServer

	store     *Store
	tasks     *TaskRouter
	worktrees *WorktreeManager
	processes *ProcessManager
	names     *NameGenerator
	eventCh   chan *mapv1.Event
	dataDir   string

	grpcServer *grpc.Server
	listener   net.Listener
	startedAt  time.Time

	mu         sync.RWMutex
	watchers   map[string]chan *mapv1.Event
	shutdown   chan struct{}
	socketPath string
}

// Config holds daemon configuration
type Config struct {
	SocketPath  string
	DataDir     string
	Multiplexer string // "tmux" (default) or "zellij"
}

// NewServer creates a new daemon server
func NewServer(cfg *Config) (*Server, error) {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.DataDir == "" {
		cfg.DataDir = expandPath(DefaultDataDir)
	}

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	eventCh := make(chan *mapv1.Event, 100)

	worktrees, err := NewWorktreeManager(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("init worktree manager: %w", err)
	}

	// Initialize multiplexer based on config or environment
	muxType := GetMultiplexerType()
	if cfg.Multiplexer != "" {
		muxType = MultiplexerType(cfg.Multiplexer)
	}
	mux, err := NewMultiplexer(muxType)
	if err != nil {
		return nil, fmt.Errorf("init multiplexer (%s): %w", muxType, err)
	}
	log.Printf("using %s as terminal multiplexer", mux.Name())

	processes := NewProcessManager(cfg.DataDir, eventCh, mux)
	tasks := NewTaskRouter(store, processes, eventCh)
	names := NewNameGenerator()

	// Wire up callback to process pending tasks when agents become available
	processes.SetOnAgentAvailable(tasks.ProcessPendingTasks)

	s := &Server{
		store:      store,
		tasks:      tasks,
		worktrees:  worktrees,
		processes:  processes,
		names:      names,
		eventCh:    eventCh,
		dataDir:    cfg.DataDir,
		watchers:   make(map[string]chan *mapv1.Event),
		shutdown:   make(chan struct{}),
		socketPath: cfg.SocketPath,
	}

	return s, nil
}

// Start begins listening for connections
func (s *Server) Start() error {
	// Remove existing socket
	_ = os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.listener = listener

	s.grpcServer = grpc.NewServer()
	mapv1.RegisterDaemonServiceServer(s.grpcServer, s)

	s.startedAt = time.Now()

	// Start event broadcaster
	go s.broadcastEvents()

	log.Printf("mapd listening on %s", s.socketPath)
	return s.grpcServer.Serve(listener)
}

// Stop gracefully shuts down the server
func (s *Server) Stop() {
	close(s.shutdown)

	// Kill all spawned processes
	if s.processes != nil {
		_ = s.processes.KillAll()
	}

	// Cleanup worktrees
	if s.worktrees != nil {
		_, _ = s.worktrees.Cleanup(nil)
	}

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	if s.store != nil {
		_ = s.store.Close()
	}
	_ = os.Remove(s.socketPath)
}

// broadcastEvents sends events to all watchers
func (s *Server) broadcastEvents() {
	for {
		select {
		case <-s.shutdown:
			return
		case event := <-s.eventCh:
			s.mu.RLock()
			for _, ch := range s.watchers {
				select {
				case ch <- event:
				default:
					// Drop if watcher is slow
				}
			}
			s.mu.RUnlock()
		}
	}
}

// --- DaemonService Implementation ---

func (s *Server) SubmitTask(ctx context.Context, req *mapv1.SubmitTaskRequest) (*mapv1.SubmitTaskResponse, error) {
	task, err := s.tasks.SubmitTask(ctx, req)
	if err != nil {
		return nil, err
	}
	return &mapv1.SubmitTaskResponse{Task: task}, nil
}

func (s *Server) ListTasks(ctx context.Context, req *mapv1.ListTasksRequest) (*mapv1.ListTasksResponse, error) {
	statusFilter := ""
	if req.StatusFilter != mapv1.TaskStatus_TASK_STATUS_UNSPECIFIED {
		statusFilter = taskStatusToString(req.StatusFilter)
	}

	tasks, err := s.tasks.ListTasks(statusFilter, req.AgentFilter, int(req.Limit))
	if err != nil {
		return nil, err
	}

	return &mapv1.ListTasksResponse{Tasks: tasks}, nil
}

func (s *Server) GetTask(ctx context.Context, req *mapv1.GetTaskRequest) (*mapv1.GetTaskResponse, error) {
	task, err := s.tasks.GetTask(req.TaskId)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", req.TaskId)
	}
	return &mapv1.GetTaskResponse{Task: task}, nil
}

func (s *Server) CancelTask(ctx context.Context, req *mapv1.CancelTaskRequest) (*mapv1.CancelTaskResponse, error) {
	task, err := s.tasks.CancelTask(req.TaskId)
	if err != nil {
		return nil, err
	}
	return &mapv1.CancelTaskResponse{Task: task}, nil
}

func (s *Server) Shutdown(ctx context.Context, req *mapv1.ShutdownRequest) (*mapv1.ShutdownResponse, error) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Stop()
	}()
	return &mapv1.ShutdownResponse{Message: "shutdown initiated"}, nil
}

func (s *Server) GetStatus(ctx context.Context, req *mapv1.GetStatusRequest) (*mapv1.GetStatusResponse, error) {
	pending, active, _ := s.store.GetStats()
	spawnedAgents := len(s.processes.List())

	muxName := ""
	if mux := s.processes.GetMultiplexer(); mux != nil {
		muxName = mux.Name()
	}

	return &mapv1.GetStatusResponse{
		Running:         true,
		StartedAt:       timestamppb.New(s.startedAt),
		ConnectedAgents: int32(spawnedAgents),
		PendingTasks:    int32(pending),
		ActiveTasks:     int32(active),
		Multiplexer:     muxName,
	}, nil
}

func (s *Server) WatchEvents(req *mapv1.WatchEventsRequest, stream mapv1.DaemonService_WatchEventsServer) error {
	watcherID := uuid.New().String()
	watchCh := make(chan *mapv1.Event, 50)

	s.mu.Lock()
	s.watchers[watcherID] = watchCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.watchers, watcherID)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-s.shutdown:
			return nil
		case event := <-watchCh:
			// Apply filters
			if len(req.TypeFilter) > 0 {
				found := false
				for _, t := range req.TypeFilter {
					if t == event.Type {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

// --- Spawned Agent Management ---

func (s *Server) SpawnAgent(ctx context.Context, req *mapv1.SpawnAgentRequest) (*mapv1.SpawnAgentResponse, error) {
	count := int(req.GetCount())
	if count < 1 {
		count = 1
	}

	// Get agent type, default to "claude"
	agentType := req.GetAgentType()
	if agentType == "" {
		agentType = AgentTypeClaude
	}

	namePrefix := req.GetNamePrefix()

	var agents []*mapv1.SpawnedAgentInfo

	for i := 0; i < count; i++ {
		var agentID string
		if namePrefix != "" {
			// Custom prefix provided: use prefix-uuid format
			agentID = fmt.Sprintf("%s-%s", namePrefix, uuid.New().String()[:8])
		} else {
			// No prefix: generate human-friendly name based on agent type
			agentID = s.names.GenerateName(agentType)
		}

		var workdir string
		var worktreePath string

		if req.GetUseWorktree() {
			// Create worktree for isolation
			wt, err := s.worktrees.Create(agentID, req.GetBranch())
			if err != nil {
				return nil, fmt.Errorf("create worktree for %s: %w", agentID, err)
			}
			workdir = wt.Path
			worktreePath = wt.Path
		} else {
			// Use the repo root or current directory
			workdir = s.worktrees.GetRepoRoot()
			if workdir == "" {
				var err error
				workdir, err = os.Getwd()
				if err != nil {
					return nil, fmt.Errorf("get working directory: %w", err)
				}
			}
		}

		// Create the agent slot
		// Skip permissions by default for autonomous agent operation
		// Permissions are skipped when: explicitly requested via skip_permissions OR using worktrees
		// Since proto3 bools default to false, we default to true for autonomous operation
		skipPermissions := req.GetSkipPermissions() || req.GetUseWorktree()
		if !req.GetSkipPermissions() && !req.GetUseWorktree() {
			// Neither flag set - default to skipping permissions for autonomous operation
			skipPermissions = true
		}
		slot, err := s.processes.Spawn(agentID, workdir, req.GetPrompt(), agentType, skipPermissions)
		if err != nil {
			// Cleanup worktree if we created one
			if worktreePath != "" {
				_ = s.worktrees.Remove(agentID)
			}
			return nil, fmt.Errorf("create agent %s: %w", agentID, err)
		}

		// Store in database
		now := time.Now()
		record := &SpawnedAgentRecord{
			AgentID:      agentID,
			WorktreePath: worktreePath,
			PID:          0, // No persistent process in new model
			Branch:       req.GetBranch(),
			Prompt:       req.GetPrompt(),
			Status:       AgentStatusIdle,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := s.store.CreateSpawnedAgent(record); err != nil {
			log.Printf("failed to store spawned agent %s: %v", agentID, err)
		}

		info := slot.ToProto()
		if mux := s.processes.GetMultiplexer(); mux != nil {
			info.Multiplexer = mux.Name()
		}
		agents = append(agents, info)

		log.Printf("created %s agent %s in %s", agentType, agentID, workdir)
	}

	return &mapv1.SpawnAgentResponse{Agents: agents}, nil
}

func (s *Server) KillAgent(ctx context.Context, req *mapv1.KillAgentRequest) (*mapv1.KillAgentResponse, error) {
	agentID := req.GetAgentId()
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	slot := s.processes.Get(agentID)
	if slot == nil {
		return &mapv1.KillAgentResponse{
			Success: false,
			Message: fmt.Sprintf("agent %s not found", agentID),
		}, nil
	}

	// Cleanup worktree if one was created
	if slot.WorktreePath != "" {
		if err := s.worktrees.Remove(agentID); err != nil {
			log.Printf("failed to remove worktree for %s: %v", agentID, err)
		}
	}

	// Update database
	_ = s.store.UpdateSpawnedAgentStatus(agentID, "removed")

	// Release the name for reuse
	s.names.ReleaseName(agentID)

	// Remove from process manager
	s.processes.Remove(agentID)

	return &mapv1.KillAgentResponse{
		Success: true,
		Message: fmt.Sprintf("agent %s removed", agentID),
	}, nil
}

func (s *Server) ListSpawnedAgents(ctx context.Context, req *mapv1.ListSpawnedAgentsRequest) (*mapv1.ListSpawnedAgentsResponse, error) {
	processes := s.processes.List()
	muxName := ""
	if mux := s.processes.GetMultiplexer(); mux != nil {
		muxName = mux.Name()
	}

	agents := make([]*mapv1.SpawnedAgentInfo, 0, len(processes))
	for _, sp := range processes {
		info := sp.ToProto()
		info.Multiplexer = muxName
		agents = append(agents, info)
	}

	return &mapv1.ListSpawnedAgentsResponse{Agents: agents}, nil
}

func (s *Server) RespawnAgent(ctx context.Context, req *mapv1.RespawnAgentRequest) (*mapv1.RespawnAgentResponse, error) {
	agentID := req.GetAgentId()
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	slot := s.processes.Get(agentID)
	if slot == nil {
		return &mapv1.RespawnAgentResponse{
			Success: false,
			Message: fmt.Sprintf("agent %s not found", agentID),
		}, nil
	}

	// Respawn with skip permissions if agent has a worktree (isolated environment)
	skipPermissions := slot.WorktreePath != ""
	if err := s.processes.RespawnInPane(agentID, skipPermissions); err != nil {
		return &mapv1.RespawnAgentResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &mapv1.RespawnAgentResponse{
		Success: true,
		Message: fmt.Sprintf("respawned claude in agent %s", agentID),
	}, nil
}

// --- Worktree Management ---

func (s *Server) ListWorktrees(ctx context.Context, req *mapv1.ListWorktreesRequest) (*mapv1.ListWorktreesResponse, error) {
	worktrees := s.worktrees.List()

	infos := make([]*mapv1.WorktreeInfo, 0, len(worktrees))
	for _, wt := range worktrees {
		infos = append(infos, &mapv1.WorktreeInfo{
			AgentId:   wt.AgentID,
			Path:      wt.Path,
			Branch:    wt.Branch,
			CreatedAt: timestamppb.New(wt.CreatedAt),
		})
	}

	return &mapv1.ListWorktreesResponse{Worktrees: infos}, nil
}

func (s *Server) CleanupWorktrees(ctx context.Context, req *mapv1.CleanupWorktreesRequest) (*mapv1.CleanupWorktreesResponse, error) {
	if req.GetAgentId() != "" {
		// Cleanup specific agent's worktree
		if err := s.worktrees.CleanupAgent(req.GetAgentId()); err != nil {
			return nil, fmt.Errorf("cleanup worktree: %w", err)
		}
		return &mapv1.CleanupWorktreesResponse{
			RemovedCount: 1,
			RemovedPaths: []string{},
		}, nil
	}

	// Cleanup orphaned worktrees
	runningAgents := s.processes.ListRunning()
	removed, err := s.worktrees.Cleanup(runningAgents)
	if err != nil {
		return nil, fmt.Errorf("cleanup worktrees: %w", err)
	}

	return &mapv1.CleanupWorktreesResponse{
		RemovedCount: int32(len(removed)),
		RemovedPaths: removed,
	}, nil
}

// Helper functions

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

func taskStatusToString(s mapv1.TaskStatus) string {
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
	default:
		return ""
	}
}
