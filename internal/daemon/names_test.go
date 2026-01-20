package daemon

import (
	"slices"
	"strings"
	"testing"
)

func TestNameGenerator_GenerateName_Claude(t *testing.T) {
	ng := NewNameGenerator()

	name := ng.GenerateName(AgentTypeClaude)

	// Should be in format firstname-lastname
	parts := strings.Split(name, "-")
	if len(parts) != 2 {
		t.Errorf("expected name in format firstname-lastname, got %s", name)
	}

	// Should be using French names
	if !slices.Contains(frenchFirstNames, parts[0]) {
		t.Errorf("first name %s not in French first names list", parts[0])
	}

	if !slices.Contains(frenchLastNames, parts[1]) {
		t.Errorf("last name %s not in French last names list", parts[1])
	}
}

func TestNameGenerator_GenerateName_Codex(t *testing.T) {
	ng := NewNameGenerator()

	name := ng.GenerateName(AgentTypeCodex)

	// Should be in format firstname-lastname
	parts := strings.Split(name, "-")
	if len(parts) != 2 {
		t.Errorf("expected name in format firstname-lastname, got %s", name)
	}

	// Should be using California names
	if !slices.Contains(californiaFirstNames, parts[0]) {
		t.Errorf("first name %s not in California first names list", parts[0])
	}

	if !slices.Contains(californiaLastNames, parts[1]) {
		t.Errorf("last name %s not in California last names list", parts[1])
	}
}

func TestNameGenerator_UniqueNames(t *testing.T) {
	ng := NewNameGenerator()

	names := make(map[string]bool)
	// Generate many names and ensure uniqueness
	for range 100 {
		name := ng.GenerateName(AgentTypeClaude)
		if names[name] {
			t.Errorf("duplicate name generated: %s", name)
		}
		names[name] = true
	}
}

func TestNameGenerator_ReleaseName(t *testing.T) {
	ng := NewNameGenerator()

	name := ng.GenerateName(AgentTypeClaude)
	ng.ReleaseName(name)

	// After releasing, the name should be available again
	// We can't guarantee it will be regenerated immediately,
	// but we can verify the internal state
	ng.mu.Lock()
	if ng.usedNames[name] {
		t.Errorf("name %s should have been released", name)
	}
	ng.mu.Unlock()
}

func TestNameGenerator_MarkUsed(t *testing.T) {
	ng := NewNameGenerator()

	ng.MarkUsed("test-name")

	ng.mu.Lock()
	if !ng.usedNames["test-name"] {
		t.Errorf("name 'test-name' should be marked as used")
	}
	ng.mu.Unlock()
}
