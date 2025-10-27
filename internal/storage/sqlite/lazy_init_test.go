package sqlite

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

const testIssueCustom1 = "custom-1"

// TestLazyCounterInitialization verifies that counters are initialized lazily
// on first use, not by scanning the entire database on every CreateIssue
func TestLazyCounterInitialization(t *testing.T) {
	store, cleanup := setupTestDBWithPrefix(t, "bd")
	defer cleanup()

	ctx := context.Background()

	// Create some issues with explicit IDs (simulating import)
	existingIssues := []string{"bd-5", "bd-10", "bd-15"}
	for _, id := range existingIssues {
		issue := &types.Issue{
			ID:        id,
			Title:     "Existing issue",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test-user")
		if err != nil {
			t.Fatalf("CreateIssue with explicit ID failed: %v", err)
		}
	}

	// Verify no counter exists yet (lazy init hasn't happened)
	var count int
	err := store.db.QueryRow(`SELECT COUNT(*) FROM issue_counters WHERE prefix = 'bd'`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query counters: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected no counter yet, but found %d", count)
	}

	// Now create an issue with auto-generated ID
	// This should trigger lazy initialization
	autoIssue := &types.Issue{
		Title:     "Auto-generated ID",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err = store.CreateIssue(ctx, autoIssue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue with auto ID failed: %v", err)
	}

	// Verify the ID is correct (should be bd-16, after bd-15)
	if autoIssue.ID != "bd-16" {
		t.Errorf("Expected bd-16, got %s", autoIssue.ID)
	}

	// Verify counter was initialized
	var lastID int
	err = store.db.QueryRow(`SELECT last_id FROM issue_counters WHERE prefix = 'bd'`).Scan(&lastID)
	if err != nil {
		t.Fatalf("Failed to query counter: %v", err)
	}

	if lastID != 16 {
		t.Errorf("Expected counter at 16, got %d", lastID)
	}

	// Create another issue - should NOT re-scan, just increment
	anotherIssue := &types.Issue{
		Title:     "Another auto-generated ID",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err = store.CreateIssue(ctx, anotherIssue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if anotherIssue.ID != "bd-17" {
		t.Errorf("Expected bd-17, got %s", anotherIssue.ID)
	}
}

// TestLazyCounterInitializationMultiplePrefix tests lazy init with multiple prefixes
// Note: With global config, prefixes are set at project level, but the counter system
// still needs to handle multiple prefixes (e.g., from imports or explicit IDs)
func TestLazyCounterInitializationMultiplePrefix(t *testing.T) {
	store, cleanup := setupTestDBWithPrefix(t, "test")
	defer cleanup()

	ctx := context.Background()

	// Create issues with explicit IDs using different prefixes
	// This simulates having issues imported from different sources
	bdIssue := &types.Issue{
		ID:        "bd-1",
		Title:     "BD issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err := store.CreateIssue(ctx, bdIssue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue with explicit ID failed: %v", err)
	}

	customIssue := &types.Issue{
		ID:        testIssueCustom1,
		Title:     "Custom issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err = store.CreateIssue(ctx, customIssue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue with explicit ID failed: %v", err)
	}

	// Now create an auto-generated issue with the configured prefix
	testIssue := &types.Issue{
		Title:     "Test issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err = store.CreateIssue(ctx, testIssue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Should get test-1 (using configured prefix)
	if testIssue.ID != "test-1" {
		t.Errorf("Expected test-1, got %s", testIssue.ID)
	}

	// Verify counter was created for the configured prefix
	var count int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM issue_counters WHERE prefix = 'test'`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query counters: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 counter for 'test' prefix, got %d", count)
	}
}

// TestCounterInitializationFromExisting tests that the counter
// correctly initializes from the max ID of existing issues
func TestCounterInitializationFromExisting(t *testing.T) {
	store, cleanup := setupTestDBWithPrefix(t, "bd")
	defer cleanup()

	ctx := context.Background()

	// Set the issue prefix to "bd" for this test

	// Create issues with explicit IDs, out of order
	explicitIDs := []string{"bd-5", "bd-100", "bd-42", "bd-7"}
	for _, id := range explicitIDs {
		issue := &types.Issue{
			ID:        id,
			Title:     "Explicit ID",
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		err := store.CreateIssue(ctx, issue, "test-user")
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	// Now auto-generate - should start at 101 (max is bd-100)
	autoIssue := &types.Issue{
		Title:     "Auto ID",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}

	err := store.CreateIssue(ctx, autoIssue, "test-user")
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if autoIssue.ID != "bd-101" {
		t.Errorf("Expected bd-101 (max was bd-100), got %s", autoIssue.ID)
	}
}
