package cli

import (
	"encoding/json"
	"testing"
)

func TestGHProjectListParsing(t *testing.T) {
	// Test parsing the actual format returned by gh project list --format json
	jsonData := `{
		"projects": [
			{"number": 1, "title": "Project Alpha", "owner": {"login": "user1", "type": "User"}},
			{"number": 2, "title": "Project Beta", "owner": {"login": "org1", "type": "Organization"}}
		]
	}`

	var list ghProjectListRaw
	if err := json.Unmarshal([]byte(jsonData), &list); err != nil {
		t.Fatalf("failed to parse project list: %v", err)
	}

	if len(list.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(list.Projects))
	}
	if list.Projects[0].Number != 1 {
		t.Errorf("expected project number 1, got %d", list.Projects[0].Number)
	}
	if list.Projects[0].Title != "Project Alpha" {
		t.Errorf("expected title 'Project Alpha', got %q", list.Projects[0].Title)
	}
	if list.Projects[0].Owner.Login != "user1" {
		t.Errorf("expected owner 'user1', got %q", list.Projects[0].Owner.Login)
	}
	if list.Projects[1].Owner.Login != "org1" {
		t.Errorf("expected owner 'org1', got %q", list.Projects[1].Owner.Login)
	}
}

func TestGHFieldListParsing(t *testing.T) {
	jsonData := `{
		"fields": [
			{
				"id": "PVTSSF_123",
				"name": "Status",
				"type": "ProjectV2SingleSelectField",
				"options": [
					{"id": "opt_1", "name": "Todo"},
					{"id": "opt_2", "name": "In Progress"},
					{"id": "opt_3", "name": "Done"}
				]
			},
			{
				"id": "PVTF_456",
				"name": "Title",
				"type": "ProjectV2Field",
				"options": null
			}
		]
	}`

	var list ghFieldList
	if err := json.Unmarshal([]byte(jsonData), &list); err != nil {
		t.Fatalf("failed to parse field list: %v", err)
	}

	if len(list.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(list.Fields))
	}

	statusField := list.Fields[0]
	if statusField.Name != "Status" {
		t.Errorf("expected field name 'Status', got %q", statusField.Name)
	}
	if statusField.Type != "ProjectV2SingleSelectField" {
		t.Errorf("expected type 'ProjectV2SingleSelectField', got %q", statusField.Type)
	}
	if len(statusField.Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(statusField.Options))
	}
	if statusField.Options[0].Name != "Todo" {
		t.Errorf("expected first option 'Todo', got %q", statusField.Options[0].Name)
	}
}

func TestGHItemListParsing(t *testing.T) {
	jsonData := `{
		"items": [
			{
				"id": "PVTI_123",
				"content": {
					"number": 42,
					"title": "Fix the bug",
					"body": "This is a bug that needs fixing.\n\nSteps to reproduce:\n1. Do this\n2. See error",
					"url": "https://github.com/owner/repo/issues/42",
					"type": "Issue"
				},
				"status": "Todo"
			},
			{
				"id": "PVTI_456",
				"content": {
					"number": 43,
					"title": "Add feature",
					"body": "",
					"url": "https://github.com/owner/repo/issues/43",
					"type": "Issue"
				},
				"status": "In Progress"
			}
		]
	}`

	var list ghItemList
	if err := json.Unmarshal([]byte(jsonData), &list); err != nil {
		t.Fatalf("failed to parse item list: %v", err)
	}

	if len(list.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(list.Items))
	}

	item := list.Items[0]
	if item.ID != "PVTI_123" {
		t.Errorf("expected ID 'PVTI_123', got %q", item.ID)
	}
	if item.Status != "Todo" {
		t.Errorf("expected status 'Todo', got %q", item.Status)
	}
	if item.Content.Number != 42 {
		t.Errorf("expected issue number 42, got %d", item.Content.Number)
	}
	if item.Content.Title != "Fix the bug" {
		t.Errorf("expected title 'Fix the bug', got %q", item.Content.Title)
	}
	if item.Content.Type != "Issue" {
		t.Errorf("expected type 'Issue', got %q", item.Content.Type)
	}
}

func TestBuildTaskDescription(t *testing.T) {
	tests := []struct {
		name     string
		item     ghItem
		contains []string
	}{
		{
			name: "issue with body",
			item: ghItem{
				ID: "PVTI_123",
				Content: ghItemContent{
					Number: 42,
					Title:  "Fix authentication bug",
					Body:   "Users cannot log in after password reset.",
					URL:    "https://github.com/owner/repo/issues/42",
					Type:   "Issue",
				},
				Status: "Todo",
			},
			contains: []string{
				"GitHub Issue #42: Fix authentication bug",
				"Users cannot log in after password reset.",
				"Source: https://github.com/owner/repo/issues/42",
			},
		},
		{
			name: "issue without body",
			item: ghItem{
				ID: "PVTI_456",
				Content: ghItemContent{
					Number: 100,
					Title:  "Add dark mode",
					Body:   "",
					URL:    "https://github.com/owner/repo/issues/100",
					Type:   "Issue",
				},
				Status: "Todo",
			},
			contains: []string{
				"GitHub Issue #100: Add dark mode",
				"Source: https://github.com/owner/repo/issues/100",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := buildTaskDescription(tc.item)

			for _, expected := range tc.contains {
				if !containsString(result, expected) {
					t.Errorf("expected description to contain %q, got:\n%s", expected, result)
				}
			}
		})
	}
}

func TestBuildTaskDescription_NoDoubleNewlines(t *testing.T) {
	item := ghItem{
		Content: ghItemContent{
			Number: 1,
			Title:  "Test",
			Body:   "",
			URL:    "https://example.com",
		},
	}

	result := buildTaskDescription(item)

	// When body is empty, there should not be excessive newlines
	if containsString(result, "\n\n\n") {
		t.Errorf("description should not contain triple newlines:\n%q", result)
	}
}

func TestGHItemFilterByStatus(t *testing.T) {
	items := []ghItem{
		{ID: "1", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
		{ID: "2", Status: "In Progress", Content: ghItemContent{Type: "Issue"}},
		{ID: "3", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
		{ID: "4", Status: "Done", Content: ghItemContent{Type: "Issue"}},
		{ID: "5", Status: "Todo", Content: ghItemContent{Type: "PullRequest"}},
	}

	// Filter for Todo issues only (mimics the logic in runTaskSyncGHProject)
	var todoItems []ghItem
	for _, item := range items {
		if item.Status == "Todo" && item.Content.Type == "Issue" {
			todoItems = append(todoItems, item)
		}
	}

	if len(todoItems) != 2 {
		t.Errorf("expected 2 Todo issues, got %d", len(todoItems))
	}
	if todoItems[0].ID != "1" {
		t.Errorf("expected first item ID '1', got %q", todoItems[0].ID)
	}
	if todoItems[1].ID != "3" {
		t.Errorf("expected second item ID '3', got %q", todoItems[1].ID)
	}
}

func TestGHItemFilterWithLimit(t *testing.T) {
	items := []ghItem{
		{ID: "1", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
		{ID: "2", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
		{ID: "3", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
		{ID: "4", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
		{ID: "5", Status: "Todo", Content: ghItemContent{Type: "Issue"}},
	}

	limit := 3
	var todoItems []ghItem
	for _, item := range items {
		if item.Status == "Todo" && item.Content.Type == "Issue" {
			todoItems = append(todoItems, item)
			if len(todoItems) >= limit {
				break
			}
		}
	}

	if len(todoItems) != 3 {
		t.Errorf("expected 3 items (limited), got %d", len(todoItems))
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
