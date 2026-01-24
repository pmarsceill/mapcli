package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"
)

func setupTestTaskRouter(t *testing.T) (*TaskRouter, *Store, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "mapd-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	store, err := NewStore(tempDir)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("create store: %v", err)
	}

	eventCh := make(chan *mapv1.Event, 100)
	router := NewTaskRouter(store, nil, eventCh)

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tempDir)
	}

	return router, store, cleanup
}

func TestNewTaskRouter(t *testing.T) {
	router, _, cleanup := setupTestTaskRouter(t)
	defer cleanup()

	if router == nil {
		t.Fatal("NewTaskRouter returned nil")
	}
}

func TestTaskRouter_SubmitTask(t *testing.T) {
	router, store, cleanup := setupTestTaskRouter(t)
	defer cleanup()

	req := &mapv1.SubmitTaskRequest{
		Description: "Test task description",
		ScopePaths:  []string{"/path/one", "/path/two"},
	}

	task, err := router.SubmitTask(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	if task == nil {
		t.Fatal("SubmitTask returned nil task")
	}

	if task.TaskId == "" {
		t.Error("TaskId should not be empty")
	}
	if task.Description != "Test task description" {
		t.Errorf("Description = %q, want %q", task.Description, "Test task description")
	}
	if len(task.ScopePaths) != 2 {
		t.Errorf("ScopePaths len = %d, want 2", len(task.ScopePaths))
	}
	if task.Status != mapv1.TaskStatus_TASK_STATUS_PENDING {
		t.Errorf("Status = %v, want PENDING", task.Status)
	}

	// Verify task is persisted in store
	storedTask, err := store.GetTask(task.TaskId)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if storedTask == nil {
		t.Fatal("Task not found in store")
	}
	if storedTask.Status != "pending" {
		t.Errorf("Stored status = %q, want %q", storedTask.Status, "pending")
	}
}

func TestTaskRouter_GetTask(t *testing.T) {
	router, store, cleanup := setupTestTaskRouter(t)
	defer cleanup()

	now := time.Now()
	record := &TaskRecord{
		TaskID:      "task-123",
		Description: "Test task",
		Status:      "pending",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.CreateTask(record); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	task, err := router.GetTask("task-123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task == nil {
		t.Fatal("GetTask returned nil")
	}
	if task.TaskId != "task-123" {
		t.Errorf("TaskId = %q, want %q", task.TaskId, "task-123")
	}

	// Non-existent task
	nonExistent, err := router.GetTask("nonexistent")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if nonExistent != nil {
		t.Error("GetTask should return nil for nonexistent task")
	}
}

func TestTaskRouter_ListTasks(t *testing.T) {
	router, store, cleanup := setupTestTaskRouter(t)
	defer cleanup()

	now := time.Now()
	tasks := []*TaskRecord{
		{TaskID: "task-1", Description: "Task 1", Status: "pending", CreatedAt: now, UpdatedAt: now},
		{TaskID: "task-2", Description: "Task 2", Status: "in_progress", AssignedTo: "agent-1", CreatedAt: now.Add(time.Second), UpdatedAt: now},
		{TaskID: "task-3", Description: "Task 3", Status: "completed", CreatedAt: now.Add(2 * time.Second), UpdatedAt: now},
	}
	for _, task := range tasks {
		if err := store.CreateTask(task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	// List all
	all, err := router.ListTasks("", "", "", 0)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListTasks returned %d tasks, want 3", len(all))
	}

	// Filter by status
	pending, err := router.ListTasks("pending", "", "", 0)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("ListTasks(pending) returned %d tasks, want 1", len(pending))
	}

	// Filter by agent
	agentTasks, err := router.ListTasks("", "agent-1", "", 0)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(agentTasks) != 1 {
		t.Errorf("ListTasks(agent-1) returned %d tasks, want 1", len(agentTasks))
	}

	// With limit
	limited, err := router.ListTasks("", "", "", 2)
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("ListTasks(limit=2) returned %d tasks, want 2", len(limited))
	}
}

func TestTaskRouter_CancelTask(t *testing.T) {
	router, store, cleanup := setupTestTaskRouter(t)
	defer cleanup()

	now := time.Now()

	testCases := []struct {
		name           string
		initialStatus  string
		expectError    bool
		expectedStatus string
	}{
		{"pending task", "pending", false, "cancelled"},
		{"in_progress task", "in_progress", false, "cancelled"},
		{"completed task", "completed", true, "completed"},
		{"failed task", "failed", true, "failed"},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			taskID := string(rune('A' + i))
			record := &TaskRecord{
				TaskID:    taskID,
				Status:    tc.initialStatus,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := store.CreateTask(record); err != nil {
				t.Fatalf("CreateTask failed: %v", err)
			}

			task, err := router.CancelTask(taskID)
			if tc.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("CancelTask failed: %v", err)
				}
				if task.Status != mapv1.TaskStatus_TASK_STATUS_CANCELLED {
					t.Errorf("Status = %v, want CANCELLED", task.Status)
				}
			}
		})
	}
}

func TestTaskRouter_CancelTask_NotFound(t *testing.T) {
	router, _, cleanup := setupTestTaskRouter(t)
	defer cleanup()

	_, err := router.CancelTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func Test_taskStatusFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected mapv1.TaskStatus
	}{
		{"pending", mapv1.TaskStatus_TASK_STATUS_PENDING},
		{"offered", mapv1.TaskStatus_TASK_STATUS_OFFERED},
		{"accepted", mapv1.TaskStatus_TASK_STATUS_ACCEPTED},
		{"in_progress", mapv1.TaskStatus_TASK_STATUS_IN_PROGRESS},
		{"completed", mapv1.TaskStatus_TASK_STATUS_COMPLETED},
		{"failed", mapv1.TaskStatus_TASK_STATUS_FAILED},
		{"cancelled", mapv1.TaskStatus_TASK_STATUS_CANCELLED},
		{"unknown", mapv1.TaskStatus_TASK_STATUS_UNSPECIFIED},
		{"", mapv1.TaskStatus_TASK_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := taskStatusFromString(tt.input)
			if result != tt.expected {
				t.Errorf("taskStatusFromString(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func Test_taskRecordToProto(t *testing.T) {
	now := time.Now()
	record := &TaskRecord{
		TaskID:      "task-123",
		Description: "Test task",
		ScopePaths:  []string{"/path/one"},
		Status:      "in_progress",
		AssignedTo:  "agent-1",
		Result:      "some result",
		Error:       "some error",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	proto := taskRecordToProto(record)

	if proto.TaskId != "task-123" {
		t.Errorf("TaskId = %q, want %q", proto.TaskId, "task-123")
	}
	if proto.Description != "Test task" {
		t.Errorf("Description = %q, want %q", proto.Description, "Test task")
	}
	if len(proto.ScopePaths) != 1 {
		t.Errorf("ScopePaths len = %d, want 1", len(proto.ScopePaths))
	}
	if proto.Status != mapv1.TaskStatus_TASK_STATUS_IN_PROGRESS {
		t.Errorf("Status = %v, want IN_PROGRESS", proto.Status)
	}
	if proto.AssignedTo != "agent-1" {
		t.Errorf("AssignedTo = %q, want %q", proto.AssignedTo, "agent-1")
	}
	if proto.Result != "some result" {
		t.Errorf("Result = %q, want %q", proto.Result, "some result")
	}
	if proto.Error != "some error" {
		t.Errorf("Error = %q, want %q", proto.Error, "some error")
	}
}
