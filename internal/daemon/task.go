package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TaskRouter manages task distribution to agents
type TaskRouter struct {
	mu      sync.RWMutex
	store   *Store
	spawned *ProcessManager // Spawned agents (Claude/Codex)
	eventCh chan *mapv1.Event
}

// NewTaskRouter creates a new task router
func NewTaskRouter(store *Store, spawned *ProcessManager, eventCh chan *mapv1.Event) *TaskRouter {
	return &TaskRouter{
		store:   store,
		spawned: spawned,
		eventCh: eventCh,
	}
}

// SubmitTask creates a new task and routes it to an available agent
func (r *TaskRouter) SubmitTask(ctx context.Context, req *mapv1.SubmitTaskRequest) (*mapv1.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	taskID := uuid.New().String()
	now := time.Now()

	// Create task record with optional GitHub source
	record := &TaskRecord{
		TaskID:            taskID,
		Description:       req.Description,
		ScopePaths:        req.ScopePaths,
		Status:            "pending",
		CreatedAt:         now,
		UpdatedAt:         now,
		GitHubOwner:       req.GetGithubOwner(),
		GitHubRepo:        req.GetGithubRepo(),
		GitHubIssueNumber: int(req.GetGithubIssueNumber()),
	}

	if err := r.store.CreateTask(record); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	task := &mapv1.Task{
		TaskId:      taskID,
		Description: req.Description,
		ScopePaths:  req.ScopePaths,
		Status:      mapv1.TaskStatus_TASK_STATUS_PENDING,
		CreatedAt:   timestamppb.New(now),
		UpdatedAt:   timestamppb.New(now),
	}

	// Add GitHub source if provided
	if record.GitHubOwner != "" && record.GitHubRepo != "" && record.GitHubIssueNumber > 0 {
		task.GithubSource = &mapv1.GitHubSource{
			Owner:       record.GitHubOwner,
			Repo:        record.GitHubRepo,
			IssueNumber: int32(record.GitHubIssueNumber),
		}
	}

	// Emit task created event
	r.emitTaskEvent(mapv1.EventType_EVENT_TYPE_TASK_CREATED, task, "")

	// Try to route immediately (non-blocking)
	go r.routeTask(task)

	return task, nil
}

// routeTask attempts to assign a task to an available agent
func (r *TaskRouter) routeTask(task *mapv1.Task) {
	// Try to route to a spawned agent
	if r.spawned != nil {
		if slot := r.spawned.FindAvailableAgent(); slot != nil {
			r.executeOnSpawnedAgent(task, slot)
			return
		}
	}
	// No agents available, task remains pending
}

// ProcessPendingTasks assigns pending tasks to available agents.
// Called when an agent becomes available (spawned or finished a task).
func (r *TaskRouter) ProcessPendingTasks() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get pending tasks ordered by creation time (oldest first)
	pendingTasks, err := r.store.ListTasks("pending", "", 0)
	if err != nil {
		return
	}

	// Reverse to process oldest first (ListTasks returns DESC order)
	for i := len(pendingTasks) - 1; i >= 0; i-- {
		task := pendingTasks[i]

		// Find an available agent
		if r.spawned == nil {
			return
		}
		slot := r.spawned.FindAvailableAgent()
		if slot == nil {
			// No more available agents
			return
		}

		// Convert to proto and assign
		protoTask := taskRecordToProto(task)
		r.executeOnSpawnedAgent(protoTask, slot)
	}
}

// executeOnSpawnedAgent runs a task on a spawned Claude agent slot
func (r *TaskRouter) executeOnSpawnedAgent(task *mapv1.Task, slot *AgentSlot) {
	// Update task status to in_progress
	_ = r.store.AssignTask(task.TaskId, slot.AgentID)
	_ = r.store.UpdateTaskStatus(task.TaskId, "in_progress")
	task.Status = mapv1.TaskStatus_TASK_STATUS_IN_PROGRESS
	task.AssignedTo = slot.AgentID
	r.emitTaskEvent(mapv1.EventType_EVENT_TYPE_TASK_STARTED, task, slot.AgentID)

	// Execute asynchronously - send prompt to tmux session
	// Task remains in_progress since we can't know when the agent finishes
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		_, err := r.spawned.ExecuteTask(ctx, slot.AgentID, task.TaskId, task.Description, task.ScopePaths)

		// Only update task if sending to tmux failed
		if err != nil {
			record, _ := r.store.GetTask(task.TaskId)
			if record == nil {
				return
			}

			record.UpdatedAt = time.Now()
			record.Status = "failed"
			record.Error = err.Error()
			_ = r.store.UpdateTask(record)

			protoTask := taskRecordToProto(record)
			r.emitTaskEvent(mapv1.EventType_EVENT_TYPE_TASK_FAILED, protoTask, slot.AgentID)
		}
		// Task stays in_progress - user can manually complete/cancel via CLI
	}()
}

// GetTask retrieves a task by ID
func (r *TaskRouter) GetTask(taskID string) (*mapv1.Task, error) {
	record, err := r.store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	return taskRecordToProto(record), nil
}

// ListTasks retrieves tasks with optional filters
func (r *TaskRouter) ListTasks(statusFilter, agentFilter string, limit int) ([]*mapv1.Task, error) {
	records, err := r.store.ListTasks(statusFilter, agentFilter, limit)
	if err != nil {
		return nil, err
	}

	tasks := make([]*mapv1.Task, len(records))
	for i, rec := range records {
		tasks[i] = taskRecordToProto(rec)
	}
	return tasks, nil
}

// CancelTask cancels a task
func (r *TaskRouter) CancelTask(taskID string) (*mapv1.Task, error) {
	task, err := r.store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	// Can only cancel pending or in_progress tasks
	switch task.Status {
	case "pending", "in_progress":
		// OK to cancel
	default:
		return nil, fmt.Errorf("cannot cancel task in status: %s", task.Status)
	}

	task.Status = "cancelled"
	task.UpdatedAt = time.Now()
	if err := r.store.UpdateTask(task); err != nil {
		return nil, err
	}

	protoTask := taskRecordToProto(task)
	r.emitTaskEvent(mapv1.EventType_EVENT_TYPE_TASK_CANCELLED, protoTask, task.AssignedTo)

	return protoTask, nil
}

func (r *TaskRouter) emitTaskEvent(eventType mapv1.EventType, task *mapv1.Task, agentID string) {
	event := &mapv1.Event{
		EventId:   uuid.New().String(),
		Type:      eventType,
		Timestamp: timestamppb.Now(),
		Payload: &mapv1.Event_Task{
			Task: &mapv1.TaskEvent{
				TaskId:    task.TaskId,
				NewStatus: task.Status,
				AgentId:   agentID,
			},
		},
	}

	// Non-blocking send
	select {
	case r.eventCh <- event:
	default:
	}
}

func taskRecordToProto(rec *TaskRecord) *mapv1.Task {
	return &mapv1.Task{
		TaskId:      rec.TaskID,
		Description: rec.Description,
		ScopePaths:  rec.ScopePaths,
		Status:      taskStatusFromString(rec.Status),
		AssignedTo:  rec.AssignedTo,
		Result:      rec.Result,
		Error:       rec.Error,
		CreatedAt:   timestamppb.New(rec.CreatedAt),
		UpdatedAt:   timestamppb.New(rec.UpdatedAt),
	}
}

// taskRecordToProtoWithGitHub converts TaskRecord to proto including GitHub fields
func (r *TaskRouter) taskRecordToProtoWithGitHub(rec *TaskRecord) *mapv1.Task {
	task := &mapv1.Task{
		TaskId:                rec.TaskID,
		Description:           rec.Description,
		ScopePaths:            rec.ScopePaths,
		Status:                taskStatusFromString(rec.Status),
		AssignedTo:            rec.AssignedTo,
		Result:                rec.Result,
		Error:                 rec.Error,
		CreatedAt:             timestamppb.New(rec.CreatedAt),
		UpdatedAt:             timestamppb.New(rec.UpdatedAt),
		WaitingInputQuestion:  rec.WaitingInputQuestion,
	}

	if rec.GitHubOwner != "" && rec.GitHubRepo != "" && rec.GitHubIssueNumber > 0 {
		task.GithubSource = &mapv1.GitHubSource{
			Owner:       rec.GitHubOwner,
			Repo:        rec.GitHubRepo,
			IssueNumber: int32(rec.GitHubIssueNumber),
		}
	}

	return task
}

func taskStatusFromString(s string) mapv1.TaskStatus {
	switch s {
	case "pending":
		return mapv1.TaskStatus_TASK_STATUS_PENDING
	case "offered":
		return mapv1.TaskStatus_TASK_STATUS_OFFERED
	case "accepted":
		return mapv1.TaskStatus_TASK_STATUS_ACCEPTED
	case "in_progress":
		return mapv1.TaskStatus_TASK_STATUS_IN_PROGRESS
	case "completed":
		return mapv1.TaskStatus_TASK_STATUS_COMPLETED
	case "failed":
		return mapv1.TaskStatus_TASK_STATUS_FAILED
	case "cancelled":
		return mapv1.TaskStatus_TASK_STATUS_CANCELLED
	case "waiting_input":
		return mapv1.TaskStatus_TASK_STATUS_WAITING_INPUT
	default:
		return mapv1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}
