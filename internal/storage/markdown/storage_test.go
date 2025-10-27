package markdown

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestMarkdownStorage_CreateAndGetIssue(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "beads-markdown-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage
	store, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a test issue
	now := time.Now()
	issue := &types.Issue{
		ID:          "test-1",
		Title:       "Test Issue",
		Description: "This is a test issue",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
		Labels:      []string{"test", "markdown"},
	}

	// Create the issue
	err = store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Verify file exists
	issuePath := filepath.Join(tmpDir, "issues", "test-1.md")
	if _, err := os.Stat(issuePath); os.IsNotExist(err) {
		t.Fatalf("Issue file was not created: %s", issuePath)
	}

	// Get the issue back
	retrieved, err := store.GetIssue(ctx, "test-1")
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	// Verify fields
	if retrieved.ID != issue.ID {
		t.Errorf("Expected ID %s, got %s", issue.ID, retrieved.ID)
	}
	if retrieved.Title != issue.Title {
		t.Errorf("Expected title %s, got %s", issue.Title, retrieved.Title)
	}
	if retrieved.Description != issue.Description {
		t.Errorf("Expected description %s, got %s", issue.Description, retrieved.Description)
	}
	if retrieved.Status != issue.Status {
		t.Errorf("Expected status %s, got %s", issue.Status, retrieved.Status)
	}
	if retrieved.Priority != issue.Priority {
		t.Errorf("Expected priority %d, got %d", issue.Priority, retrieved.Priority)
	}
	if len(retrieved.Labels) != len(issue.Labels) {
		t.Errorf("Expected %d labels, got %d", len(issue.Labels), len(retrieved.Labels))
	}
}

func TestMarkdownStorage_UpdateIssue(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "beads-markdown-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage
	store, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a test issue
	now := time.Now()
	issue := &types.Issue{
		ID:          "test-2",
		Title:       "Test Issue 2",
		Description: "Original description",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Create the issue
	err = store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Update the issue
	updates := map[string]interface{}{
		"title":       "Updated Title",
		"description": "Updated description",
		"priority":    3,
		"status":      string(types.StatusInProgress),
	}

	err = store.UpdateIssue(ctx, "test-2", updates, "test-user")
	if err != nil {
		t.Fatalf("Failed to update issue: %v", err)
	}

	// Get the updated issue
	retrieved, err := store.GetIssue(ctx, "test-2")
	if err != nil {
		t.Fatalf("Failed to get issue: %v", err)
	}

	// Verify updates
	if retrieved.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got %s", retrieved.Title)
	}
	if retrieved.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got %s", retrieved.Description)
	}
	if retrieved.Priority != 3 {
		t.Errorf("Expected priority 3, got %d", retrieved.Priority)
	}
	if retrieved.Status != types.StatusInProgress {
		t.Errorf("Expected status %s, got %s", types.StatusInProgress, retrieved.Status)
	}
}

func TestMarkdownStorage_DeleteIssue(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "beads-markdown-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage
	store, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a test issue
	now := time.Now()
	issue := &types.Issue{
		ID:          "test-3",
		Title:       "Test Issue 3",
		Description: "To be deleted",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Create the issue
	err = store.CreateIssue(ctx, issue, "test-user")
	if err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Verify it exists
	issuePath := filepath.Join(tmpDir, "issues", "test-3.md")
	if _, err := os.Stat(issuePath); os.IsNotExist(err) {
		t.Fatalf("Issue file was not created")
	}

	// Delete the issue
	err = store.DeleteIssue(ctx, "test-3", "test-user")
	if err != nil {
		t.Fatalf("Failed to delete issue: %v", err)
	}

	// Verify it no longer exists
	if _, err := os.Stat(issuePath); !os.IsNotExist(err) {
		t.Errorf("Issue file still exists after deletion")
	}

	// Try to get the deleted issue - should return (nil, nil) like SQLite
	deletedIssue, err := store.GetIssue(ctx, "test-3")
	if err != nil {
		t.Errorf("Expected no error when getting deleted issue, got: %v", err)
	}
	if deletedIssue != nil {
		t.Error("Expected nil issue when getting deleted issue, got non-nil")
	}
}

func TestMarkdownStorage_ListIssues(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "beads-markdown-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage
	store, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()

	// Create multiple issues
	issues := []*types.Issue{
		{
			ID:          "test-4",
			Title:       "Issue 4",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "test-5",
			Title:       "Issue 5",
			Status:      types.StatusInProgress,
			Priority:    2,
			IssueType:   types.TypeBug,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          "test-6",
			Title:       "Issue 6",
			Status:      types.StatusOpen,
			Priority:    3,
			IssueType:   types.TypeTask,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	for _, issue := range issues {
		err = store.CreateIssue(ctx, issue, "test-user")
		if err != nil {
			t.Fatalf("Failed to create issue %s: %v", issue.ID, err)
		}
	}

	// List all issues
	allIssues, err := store.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to list issues: %v", err)
	}

	if len(allIssues) != 3 {
		t.Errorf("Expected 3 issues, got %d", len(allIssues))
	}

	// List issues with filter (status = open)
	openStatus := types.StatusOpen
	openIssues, err := store.ListIssues(ctx, types.IssueFilter{
		Status: &openStatus,
	})
	if err != nil {
		t.Fatalf("Failed to list open issues: %v", err)
	}

	if len(openIssues) != 2 {
		t.Errorf("Expected 2 open issues, got %d", len(openIssues))
	}

	// List issues with filter (issue_type = bug)
	bugType := types.TypeBug
	bugIssues, err := store.ListIssues(ctx, types.IssueFilter{
		IssueType: &bugType,
	})
	if err != nil {
		t.Fatalf("Failed to list bug issues: %v", err)
	}

	if len(bugIssues) != 1 {
		t.Errorf("Expected 1 bug issue, got %d", len(bugIssues))
	}
}
