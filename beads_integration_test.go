package beads_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads"
)

// integrationTestHelper provides common test setup and assertion methods
type integrationTestHelper struct {
	t     *testing.T
	ctx   context.Context
	store beads.Storage
}

func newIntegrationHelper(t *testing.T, store beads.Storage) *integrationTestHelper {
	return &integrationTestHelper{t: t, ctx: context.Background(), store: store}
}

func (h *integrationTestHelper) createIssue(title string, issueType beads.IssueType, priority int) *beads.Issue {
	issue := &beads.Issue{
		Title:     title,
		Status:    beads.StatusOpen,
		Priority:  priority,
		IssueType: issueType,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.store.CreateIssue(h.ctx, issue, "test-actor"); err != nil {
		h.t.Fatalf("CreateIssue failed: %v", err)
	}
	return issue
}

func (h *integrationTestHelper) createFullIssue(desc, design, acceptance, notes, assignee string) *beads.Issue {
	issue := &beads.Issue{
		Title:              "Complete issue",
		Description:        desc,
		Design:             design,
		AcceptanceCriteria: acceptance,
		Notes:              notes,
		Status:             beads.StatusOpen,
		Priority:           1,
		IssueType:          beads.TypeFeature,
		Assignee:           assignee,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := h.store.CreateIssue(h.ctx, issue, "test-actor"); err != nil {
		h.t.Fatalf("CreateIssue failed: %v", err)
	}
	return issue
}

func (h *integrationTestHelper) updateIssue(id string, updates map[string]interface{}) {
	if err := h.store.UpdateIssue(h.ctx, id, updates, "test-actor"); err != nil {
		h.t.Fatalf("UpdateIssue failed: %v", err)
	}
}

func (h *integrationTestHelper) closeIssue(id string, reason string) {
	if err := h.store.CloseIssue(h.ctx, id, reason, "test-actor"); err != nil {
		h.t.Fatalf("CloseIssue failed: %v", err)
	}
}

func (h *integrationTestHelper) addDependency(issue1ID, issue2ID string) {
	dep := &beads.Dependency{
		IssueID:     issue1ID,
		DependsOnID: issue2ID,
		Type:        beads.DepBlocks,
		CreatedAt:   time.Now(),
		CreatedBy:   "test-actor",
	}
	if err := h.store.AddDependency(h.ctx, dep, "test-actor"); err != nil {
		h.t.Fatalf("AddDependency failed: %v", err)
	}
}

func (h *integrationTestHelper) addLabel(id, label string) {
	if err := h.store.AddLabel(h.ctx, id, label, "test-actor"); err != nil {
		h.t.Fatalf("AddLabel failed: %v", err)
	}
}

func (h *integrationTestHelper) addComment(id, user, text string) *beads.Comment {
	comment, err := h.store.AddIssueComment(h.ctx, id, user, text)
	if err != nil {
		h.t.Fatalf("AddIssueComment failed: %v", err)
	}
	return comment
}

func (h *integrationTestHelper) getIssue(id string) *beads.Issue {
	issue, err := h.store.GetIssue(h.ctx, id)
	if err != nil {
		h.t.Fatalf("GetIssue failed: %v", err)
	}
	return issue
}

func (h *integrationTestHelper) getDependencies(id string) []*beads.Issue {
	deps, err := h.store.GetDependencies(h.ctx, id)
	if err != nil {
		h.t.Fatalf("GetDependencies failed: %v", err)
	}
	return deps
}

func (h *integrationTestHelper) getLabels(id string) []string {
	labels, err := h.store.GetLabels(h.ctx, id)
	if err != nil {
		h.t.Fatalf("GetLabels failed: %v", err)
	}
	return labels
}

func (h *integrationTestHelper) getComments(id string) []*beads.Comment {
	comments, err := h.store.GetIssueComments(h.ctx, id)
	if err != nil {
		h.t.Fatalf("GetIssueComments failed: %v", err)
	}
	return comments
}

func (h *integrationTestHelper) assertID(id string) {
	if id == "" {
		h.t.Error("Issue ID should be auto-generated")
	}
}

func (h *integrationTestHelper) assertEqual(expected, actual interface{}, field string) {
	if expected != actual {
		h.t.Errorf("Expected %s %v, got %v", field, expected, actual)
	}
}

func (h *integrationTestHelper) assertNotNil(value interface{}, field string) {
	if value == nil {
		h.t.Errorf("Expected %s to be set", field)
	}
}

func (h *integrationTestHelper) assertCount(count, expected int, item string) {
	if count != expected {
		h.t.Fatalf("Expected %d %s, got %d", expected, item, count)
	}
}

// TestLibraryIntegration tests the full public API that external users will use
func TestLibraryIntegration(t *testing.T) {
	// Setup: Create a temporary database
	tmpDir, err := os.MkdirTemp("", "beads-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := beads.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}
	defer store.Close()

	h := newIntegrationHelper(t, store)

	// Test 1: Create issue
	t.Run("CreateIssue", func(t *testing.T) {
		issue := h.createIssue("Test task", beads.TypeTask, 2)
		h.assertID(issue.ID)
		t.Logf("Created issue: %s", issue.ID)
	})

	// Test 2: Get issue
	t.Run("GetIssue", func(_ *testing.T) {
		issue := h.createIssue("Get test", beads.TypeBug, 1)
		retrieved := h.getIssue(issue.ID)
		h.assertEqual(issue.Title, retrieved.Title, "title")
		h.assertEqual(beads.TypeBug, retrieved.IssueType, "type")
	})

	// Test 3: Update issue
	t.Run("UpdateIssue", func(_ *testing.T) {
		issue := h.createIssue("Update test", beads.TypeTask, 2)
		updates := map[string]interface{}{"status": beads.StatusInProgress, "assignee": "test-user"}
		h.updateIssue(issue.ID, updates)
		updated := h.getIssue(issue.ID)
		h.assertEqual(beads.StatusInProgress, updated.Status, "status")
		h.assertEqual("test-user", updated.Assignee, "assignee")
	})

	// Test 4: Add dependency
	t.Run("AddDependency", func(_ *testing.T) {
		issue1 := h.createIssue("Parent task", beads.TypeTask, 1)
		issue2 := h.createIssue("Child task", beads.TypeTask, 1)
		h.addDependency(issue1.ID, issue2.ID)
		deps := h.getDependencies(issue1.ID)
		h.assertCount(len(deps), 1, "dependencies")
		h.assertEqual(issue2.ID, deps[0].ID, "dependency ID")
	})

	// Test 5: Add label
	t.Run("AddLabel", func(t *testing.T) {
		issue := h.createIssue("Label test", beads.TypeFeature, 2)
		h.addLabel(issue.ID, "urgent")
		labels := h.getLabels(issue.ID)
		h.assertCount(len(labels), 1, "labels")
		h.assertEqual("urgent", labels[0], "label")
	})

	// Test 6: Add comment
	t.Run("AddComment", func(t *testing.T) {
		issue := h.createIssue("Comment test", beads.TypeTask, 2)
		comment := h.addComment(issue.ID, "test-user", "Test comment")
		h.assertEqual("Test comment", comment.Text, "comment text")
		comments := h.getComments(issue.ID)
		h.assertCount(len(comments), 1, "comments")
	})

	// Test 7: Get ready work
	t.Run("GetReadyWork", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			h.createIssue("Ready work test", beads.TypeTask, i)
		}
		ready, err := store.GetReadyWork(h.ctx, beads.WorkFilter{Status: beads.StatusOpen, Limit: 5})
		if err != nil {
			t.Fatalf("GetReadyWork failed: %v", err)
		}
		if len(ready) == 0 {
			t.Error("Expected some ready work, got none")
		}
		t.Logf("Found %d ready issues", len(ready))
	})

	// Test 8: Get statistics
	t.Run("GetStatistics", func(t *testing.T) {
		stats, err := store.GetStatistics(h.ctx)
		if err != nil {
			t.Fatalf("GetStatistics failed: %v", err)
		}
		if stats.TotalIssues == 0 {
			t.Error("Expected some total issues, got 0")
		}
		t.Logf("Stats: Total=%d, Open=%d, InProgress=%d, Closed=%d",
			stats.TotalIssues, stats.OpenIssues, stats.InProgressIssues, stats.ClosedIssues)
	})

	// Test 9: Close issue
	t.Run("CloseIssue", func(t *testing.T) {
		issue := h.createIssue("Close test", beads.TypeTask, 2)
		h.closeIssue(issue.ID, "Completed")
		closed := h.getIssue(issue.ID)
		h.assertEqual(beads.StatusClosed, closed.Status, "status")
		h.assertNotNil(closed.ClosedAt, "ClosedAt")
	})
}

// TestDependencyTypes ensures all dependency type constants are exported
func TestDependencyTypes(t *testing.T) {
	types := []beads.DependencyType{
		beads.DepBlocks,
		beads.DepRelated,
		beads.DepParentChild,
		beads.DepDiscoveredFrom,
	}

	for _, dt := range types {
		if dt == "" {
			t.Errorf("Dependency type should not be empty")
		}
	}
}

// TestStatusConstants ensures all status constants are exported
func TestStatusConstants(t *testing.T) {
	statuses := []beads.Status{
		beads.StatusOpen,
		beads.StatusInProgress,
		beads.StatusClosed,
		beads.StatusBlocked,
	}

	for _, s := range statuses {
		if s == "" {
			t.Errorf("Status should not be empty")
		}
	}
}

// TestIssueTypeConstants ensures all issue type constants are exported
func TestIssueTypeConstants(t *testing.T) {
	types := []beads.IssueType{
		beads.TypeBug,
		beads.TypeFeature,
		beads.TypeTask,
		beads.TypeEpic,
		beads.TypeChore,
	}

	for _, it := range types {
		if it == "" {
			t.Errorf("IssueType should not be empty")
		}
	}
}

// TestBatchCreateIssues tests creating multiple issues at once
func TestBatchCreateIssues(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-batch-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := beads.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create multiple issues
	issues := make([]*beads.Issue, 5)
	for i := 0; i < 5; i++ {
		issues[i] = &beads.Issue{
			Title:     "Batch test",
			Status:    beads.StatusOpen,
			Priority:  2,
			IssueType: beads.TypeTask,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	err = store.CreateIssues(ctx, issues, "test-actor")
	if err != nil {
		t.Fatalf("CreateIssues failed: %v", err)
	}

	// Verify all got IDs
	for i, issue := range issues {
		if issue.ID == "" {
			t.Errorf("Issue %d should have ID set", i)
		}
	}
}

// TestFindDatabasePathIntegration tests the database discovery
func TestFindDatabasePathIntegration(t *testing.T) {
	// Save original working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create temporary directory with .beads
	tmpDir, err := os.MkdirTemp("", "beads-find-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	beadsDir := filepath.Join(tmpDir, ".beads")
	os.MkdirAll(beadsDir, 0o755)

	dbPath := filepath.Join(beadsDir, "test.db")
	f, _ := os.Create(dbPath)
	f.Close()

	// Change to temp directory
	os.Chdir(tmpDir)

	// Should find the database
	found := beads.FindDatabasePath()
	if found == "" {
		t.Error("Expected to find database, got empty string")
	}

	t.Logf("Found database at: %s", found)
}

// TestRoundTripIssue tests creating, updating, and retrieving an issue
func TestRoundTripIssue(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "beads-roundtrip-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := beads.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStorage failed: %v", err)
	}
	defer store.Close()

	h := newIntegrationHelper(t, store)
	original := h.createFullIssue("Full description", "Design notes", "Acceptance criteria", "Implementation notes", "developer")

	// Retrieve and verify all fields
	retrieved := h.getIssue(original.ID)
	h.assertEqual(original.Title, retrieved.Title, "Title")
	h.assertEqual(original.Description, retrieved.Description, "Description")
	h.assertEqual(original.Design, retrieved.Design, "Design")
	h.assertEqual(original.AcceptanceCriteria, retrieved.AcceptanceCriteria, "AcceptanceCriteria")
	h.assertEqual(original.Notes, retrieved.Notes, "Notes")
	h.assertEqual(original.Status, retrieved.Status, "Status")
	h.assertEqual(original.Priority, retrieved.Priority, "Priority")
	h.assertEqual(original.IssueType, retrieved.IssueType, "IssueType")
	h.assertEqual(original.Assignee, retrieved.Assignee, "Assignee")
}
