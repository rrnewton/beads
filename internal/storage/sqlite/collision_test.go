package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

const testIssueBD1 = "bd-1"

func TestDetectCollisions(t *testing.T) {
	// Create temporary database
	tmpDir, err := os.MkdirTemp("", "collision-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Set issue prefix to prevent "database not initialized" errors
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Setup: Create some existing issues in the database
	existingIssue1 := &types.Issue{
		ID:          testIssueBD1,
		Title:       "Existing issue 1",
		Description: "This is an existing issue",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	existingIssue2 := &types.Issue{
		ID:          "bd-2",
		Title:       "Existing issue 2",
		Description: "Another existing issue",
		Status:      types.StatusInProgress,
		Priority:    2,
		IssueType:   types.TypeBug,
	}

	if err := store.CreateIssue(ctx, existingIssue1, "test"); err != nil {
		t.Fatalf("failed to create existing issue 1: %v", err)
	}
	if err := store.CreateIssue(ctx, existingIssue2, "test"); err != nil {
		t.Fatalf("failed to create existing issue 2: %v", err)
	}

	// Test cases
	tests := []struct {
		name              string
		incomingIssues    []*types.Issue
		expectedExact     int
		expectedCollision int
		expectedNew       int
		checkCollisions   func(t *testing.T, collisions []*CollisionDetail)
	}{
		{
			name: "exact match - idempotent import",
			incomingIssues: []*types.Issue{
				{
					ID:          testIssueBD1,
					Title:       "Existing issue 1",
					Description: "This is an existing issue",
					Status:      types.StatusOpen,
					Priority:    1,
					IssueType:   types.TypeTask,
				},
			},
			expectedExact:     1,
			expectedCollision: 0,
			expectedNew:       0,
		},
		{
			name: "new issue - doesn't exist in DB",
			incomingIssues: []*types.Issue{
				{
					ID:          "bd-100",
					Title:       "Brand new issue",
					Description: "This doesn't exist yet",
					Status:      types.StatusOpen,
					Priority:    1,
					IssueType:   types.TypeFeature,
				},
			},
			expectedExact:     0,
			expectedCollision: 0,
			expectedNew:       1,
		},
		{
			name: "collision - same ID, different title",
			incomingIssues: []*types.Issue{
				{
					ID:          testIssueBD1,
					Title:       "Modified title",
					Description: "This is an existing issue",
					Status:      types.StatusOpen,
					Priority:    1,
					IssueType:   types.TypeTask,
				},
			},
			expectedExact:     0,
			expectedCollision: 1,
			expectedNew:       0,
			checkCollisions: func(t *testing.T, collisions []*CollisionDetail) {
				if len(collisions) != 1 {
					t.Fatalf("expected 1 collision, got %d", len(collisions))
				}
				if collisions[0].ID != testIssueBD1 {
					t.Errorf("expected collision ID bd-1, got %s", collisions[0].ID)
				}
				if len(collisions[0].ConflictingFields) != 1 {
					t.Errorf("expected 1 conflicting field, got %d", len(collisions[0].ConflictingFields))
				}
				if collisions[0].ConflictingFields[0] != "title" {
					t.Errorf("expected conflicting field 'title', got %s", collisions[0].ConflictingFields[0])
				}
			},
		},
		{
			name: "collision - multiple fields differ",
			incomingIssues: []*types.Issue{
				{
					ID:          "bd-2",
					Title:       "Changed title",
					Description: "Changed description",
					Status:      types.StatusClosed,
					Priority:    3,
					IssueType:   types.TypeFeature,
				},
			},
			expectedExact:     0,
			expectedCollision: 1,
			expectedNew:       0,
			checkCollisions: func(t *testing.T, collisions []*CollisionDetail) {
				if len(collisions) != 1 {
					t.Fatalf("expected 1 collision, got %d", len(collisions))
				}
				// Should have multiple conflicting fields
				expectedFields := map[string]bool{
					"title":       true,
					"description": true,
					"status":      true,
					"priority":    true,
					"issue_type":  true,
				}
				for _, field := range collisions[0].ConflictingFields {
					if !expectedFields[field] {
						t.Errorf("unexpected conflicting field: %s", field)
					}
					delete(expectedFields, field)
				}
				if len(expectedFields) > 0 {
					t.Errorf("missing expected conflicting fields: %v", expectedFields)
				}
			},
		},
		{
			name: "mixed - exact, collision, and new",
			incomingIssues: []*types.Issue{
				{
					// Exact match
					ID:          testIssueBD1,
					Title:       "Existing issue 1",
					Description: "This is an existing issue",
					Status:      types.StatusOpen,
					Priority:    1,
					IssueType:   types.TypeTask,
				},
				{
					// Collision
					ID:          "bd-2",
					Title:       "Modified issue 2",
					Description: "Another existing issue",
					Status:      types.StatusInProgress,
					Priority:    2,
					IssueType:   types.TypeBug,
				},
				{
					// New issue
					ID:          "bd-200",
					Title:       "New issue",
					Description: "This is new",
					Status:      types.StatusOpen,
					Priority:    1,
					IssueType:   types.TypeTask,
				},
			},
			expectedExact:     1,
			expectedCollision: 1,
			expectedNew:       1,
		},
		{
			name: "collision - estimated_minutes differs",
			incomingIssues: []*types.Issue{
				{
					ID:               testIssueBD1,
					Title:            "Existing issue 1",
					Description:      "This is an existing issue",
					Status:           types.StatusOpen,
					Priority:         1,
					IssueType:        types.TypeTask,
					EstimatedMinutes: intPtr(60),
				},
			},
			expectedExact:     0,
			expectedCollision: 1,
			expectedNew:       0,
			checkCollisions: func(t *testing.T, collisions []*CollisionDetail) {
				if len(collisions[0].ConflictingFields) != 1 {
					t.Errorf("expected 1 conflicting field, got %d", len(collisions[0].ConflictingFields))
				}
				if collisions[0].ConflictingFields[0] != "estimated_minutes" {
					t.Errorf("expected conflicting field 'estimated_minutes', got %s", collisions[0].ConflictingFields[0])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DetectCollisions(ctx, store, tt.incomingIssues)
			if err != nil {
				t.Fatalf("DetectCollisions failed: %v", err)
			}

			if len(result.ExactMatches) != tt.expectedExact {
				t.Errorf("expected %d exact matches, got %d", tt.expectedExact, len(result.ExactMatches))
			}
			if len(result.Collisions) != tt.expectedCollision {
				t.Errorf("expected %d collisions, got %d", tt.expectedCollision, len(result.Collisions))
			}
			if len(result.NewIssues) != tt.expectedNew {
				t.Errorf("expected %d new issues, got %d", tt.expectedNew, len(result.NewIssues))
			}

			if tt.checkCollisions != nil {
				tt.checkCollisions(t, result.Collisions)
			}
		})
	}
}

func TestCompareIssues(t *testing.T) {
	tests := []struct {
		name     string
		existing *types.Issue
		incoming *types.Issue
		expected []string
	}{
		{
			name: "identical issues",
			existing: &types.Issue{
				ID:          testIssueBD1,
				Title:       "Test",
				Description: "Test desc",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
			},
			incoming: &types.Issue{
				ID:          testIssueBD1,
				Title:       "Test",
				Description: "Test desc",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
			},
			expected: []string{},
		},
		{
			name: "different title",
			existing: &types.Issue{
				ID:          testIssueBD1,
				Title:       "Original",
				Description: "Test",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
			},
			incoming: &types.Issue{
				ID:          testIssueBD1,
				Title:       "Modified",
				Description: "Test",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
			},
			expected: []string{"title"},
		},
		{
			name: "different status and priority",
			existing: &types.Issue{
				ID:          testIssueBD1,
				Title:       "Test",
				Description: "Test",
				Status:      types.StatusOpen,
				Priority:    1,
				IssueType:   types.TypeTask,
			},
			incoming: &types.Issue{
				ID:          testIssueBD1,
				Title:       "Test",
				Description: "Test",
				Status:      types.StatusClosed,
				Priority:    3,
				IssueType:   types.TypeTask,
			},
			expected: []string{"status", "priority"},
		},
		{
			name: "estimated_minutes - both nil",
			existing: &types.Issue{
				ID:               testIssueBD1,
				Title:            "Test",
				Description:      "Test",
				Status:           types.StatusOpen,
				Priority:         1,
				IssueType:        types.TypeTask,
				EstimatedMinutes: nil,
			},
			incoming: &types.Issue{
				ID:               testIssueBD1,
				Title:            "Test",
				Description:      "Test",
				Status:           types.StatusOpen,
				Priority:         1,
				IssueType:        types.TypeTask,
				EstimatedMinutes: nil,
			},
			expected: []string{},
		},
		{
			name: "estimated_minutes - existing nil, incoming set",
			existing: &types.Issue{
				ID:               testIssueBD1,
				Title:            "Test",
				Description:      "Test",
				Status:           types.StatusOpen,
				Priority:         1,
				IssueType:        types.TypeTask,
				EstimatedMinutes: nil,
			},
			incoming: &types.Issue{
				ID:               testIssueBD1,
				Title:            "Test",
				Description:      "Test",
				Status:           types.StatusOpen,
				Priority:         1,
				IssueType:        types.TypeTask,
				EstimatedMinutes: intPtr(30),
			},
			expected: []string{"estimated_minutes"},
		},
		{
			name: "estimated_minutes - same values",
			existing: &types.Issue{
				ID:               testIssueBD1,
				Title:            "Test",
				Description:      "Test",
				Status:           types.StatusOpen,
				Priority:         1,
				IssueType:        types.TypeTask,
				EstimatedMinutes: intPtr(60),
			},
			incoming: &types.Issue{
				ID:               testIssueBD1,
				Title:            "Test",
				Description:      "Test",
				Status:           types.StatusOpen,
				Priority:         1,
				IssueType:        types.TypeTask,
				EstimatedMinutes: intPtr(60),
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conflicts := compareIssues(tt.existing, tt.incoming)
			if len(conflicts) != len(tt.expected) {
				t.Errorf("expected %d conflicts, got %d: %v", len(tt.expected), len(conflicts), conflicts)
				return
			}
			for i, expected := range tt.expected {
				if conflicts[i] != expected {
					t.Errorf("conflict[%d]: expected %s, got %s", i, expected, conflicts[i])
				}
			}
		})
	}
}

func TestEqualIntPtr(t *testing.T) {
	tests := []struct {
		name     string
		a        *int
		b        *int
		expected bool
	}{
		{"both nil", nil, nil, true},
		{"a nil, b set", nil, intPtr(5), false},
		{"a set, b nil", intPtr(5), nil, false},
		{"same values", intPtr(10), intPtr(10), true},
		{"different values", intPtr(10), intPtr(20), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := equalIntPtr(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper function to create *int from int value
func intPtr(i int) *int {
	return &i
}

func TestScoreCollisions(t *testing.T) {
	// Create temporary database
	tmpDir, err := os.MkdirTemp("", "score-collision-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Set issue prefix to prevent "database not initialized" errors
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Setup: Create issues with various reference patterns
	issue1 := &types.Issue{
		ID:          "bd-1",
		Title:       "Issue 1",
		Description: "Depends on bd-2",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	issue2 := &types.Issue{
		ID:          "bd-2",
		Title:       "Issue 2",
		Description: "Referenced by bd-1 and bd-3",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	issue3 := &types.Issue{
		ID:          "bd-3",
		Title:       "Issue 3",
		Description: "Mentions bd-2 multiple times: bd-2 and bd-2",
		Notes:       "Also mentions bd-2 here",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	issue4 := &types.Issue{
		ID:          "bd-4",
		Title:       "Issue 4",
		Description: "Lonely issue with no references",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	// Create issues in DB
	if err := store.CreateIssue(ctx, issue1, "test"); err != nil {
		t.Fatalf("failed to create issue1: %v", err)
	}
	if err := store.CreateIssue(ctx, issue2, "test"); err != nil {
		t.Fatalf("failed to create issue2: %v", err)
	}
	if err := store.CreateIssue(ctx, issue3, "test"); err != nil {
		t.Fatalf("failed to create issue3: %v", err)
	}
	if err := store.CreateIssue(ctx, issue4, "test"); err != nil {
		t.Fatalf("failed to create issue4: %v", err)
	}

	// Add dependencies
	dep1 := &types.Dependency{IssueID: "bd-1", DependsOnID: "bd-2", Type: types.DepBlocks}
	dep2 := &types.Dependency{IssueID: "bd-3", DependsOnID: "bd-2", Type: types.DepBlocks}

	if err := store.AddDependency(ctx, dep1, "test"); err != nil {
		t.Fatalf("failed to add dependency1: %v", err)
	}
	if err := store.AddDependency(ctx, dep2, "test"); err != nil {
		t.Fatalf("failed to add dependency2: %v", err)
	}

	// Create collision details (simulated)
	collisions := []*CollisionDetail{
		{
			ID:            "bd-1",
			IncomingIssue: issue1,
			ExistingIssue: issue1,
		},
		{
			ID:            "bd-2",
			IncomingIssue: issue2,
			ExistingIssue: issue2,
		},
		{
			ID:            "bd-3",
			IncomingIssue: issue3,
			ExistingIssue: issue3,
		},
		{
			ID:            "bd-4",
			IncomingIssue: issue4,
			ExistingIssue: issue4,
		},
	}

	allIssues := []*types.Issue{issue1, issue2, issue3, issue4}

	// Score the collisions
	err = ScoreCollisions(ctx, store, collisions, allIssues)
	if err != nil {
		t.Fatalf("ScoreCollisions failed: %v", err)
	}

	// Verify RemapIncoming was set based on content hashes (bd-95)
	// ScoreCollisions now uses content-based hashing instead of reference counting
	// Each collision should have RemapIncoming set based on hash comparison
	for _, collision := range collisions {
		existingHash := hashIssueContent(collision.ExistingIssue)
		incomingHash := hashIssueContent(collision.IncomingIssue)
		expectedRemapIncoming := existingHash < incomingHash
		
		if collision.RemapIncoming != expectedRemapIncoming {
			t.Errorf("collision %s: RemapIncoming=%v but expected %v (existingHash=%s, incomingHash=%s)",
				collision.ID, collision.RemapIncoming, expectedRemapIncoming,
				existingHash[:8], incomingHash[:8])
		}
	}
}

func TestReplaceIDReferences(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		idMapping map[string]string
		expected  string
	}{
		{
			name: "single replacement",
			text: "This references bd-1 in the description",
			idMapping: map[string]string{
				"bd-1": "bd-100",
			},
			expected: "This references bd-100 in the description",
		},
		{
			name: "multiple replacements",
			text: "bd-1 depends on bd-2 and bd-3",
			idMapping: map[string]string{
				"bd-1": "bd-100",
				"bd-2": "bd-101",
				"bd-3": "bd-102",
			},
			expected: "bd-100 depends on bd-101 and bd-102",
		},
		{
			name: "word boundary - don't replace partial matches",
			text: "bd-10 and bd-100 and bd-1",
			idMapping: map[string]string{
				"bd-1": "bd-200",
			},
			expected: "bd-10 and bd-100 and bd-200",
		},
		{
			name: "no replacements needed",
			text: "This has no matching IDs",
			idMapping: map[string]string{
				"bd-1": "bd-100",
			},
			expected: "This has no matching IDs",
		},
		{
			name: "replace same ID multiple times",
			text: "bd-1 is mentioned twice: bd-1",
			idMapping: map[string]string{
				"bd-1": "bd-100",
			},
			expected: "bd-100 is mentioned twice: bd-100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceIDReferences(tt.text, tt.idMapping)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRemapCollisions(t *testing.T) {
	// Create temporary database
	tmpDir, err := os.MkdirTemp("", "remap-collision-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Set issue prefix to prevent "database not initialized" errors
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Setup: Create an existing issue in the database with a high ID number
	// This ensures that when we remap bd-2 and bd-3, they get new IDs that don't conflict
	existingIssue := &types.Issue{
		ID:          "bd-10",
		Title:       "Existing issue",
		Description: "This mentions bd-2 and bd-3",
		Notes:       "Also bd-2 here",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, existingIssue, "test"); err != nil {
		t.Fatalf("failed to create existing issue: %v", err)
	}

	// Create existing issues in DB that will collide with incoming issues
	dbIssue2 := &types.Issue{
		ID:          "bd-2",
		Title:       "Existing issue bd-2",
		Description: "Original content for bd-2",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, dbIssue2, "test"); err != nil {
		t.Fatalf("failed to create dbIssue2: %v", err)
	}

	dbIssue3 := &types.Issue{
		ID:          "bd-3",
		Title:       "Existing issue bd-3",
		Description: "Original content for bd-3",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, dbIssue3, "test"); err != nil {
		t.Fatalf("failed to create dbIssue3: %v", err)
	}

	// Create collisions (incoming issues with same IDs as DB but different content)
	collision1 := &CollisionDetail{
		ID:            "bd-2",
		ExistingIssue: dbIssue2,
		IncomingIssue: &types.Issue{
			ID:          "bd-2",
			Title:       "Collision 2",
			Description: "This is different content",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		},
		RemapIncoming: true, // Incoming will be remapped
	}

	collision2 := &CollisionDetail{
		ID:            "bd-3",
		ExistingIssue: dbIssue3,
		IncomingIssue: &types.Issue{
			ID:          "bd-3",
			Title:       "Collision 3",
			Description: "Different content for bd-3",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		},
		RemapIncoming: true, // Incoming will be remapped
	}

	collisions := []*CollisionDetail{collision1, collision2}
	allIssues := []*types.Issue{existingIssue, dbIssue2, dbIssue3, collision1.IncomingIssue, collision2.IncomingIssue}

	// Remap collisions
	idMapping, err := RemapCollisions(ctx, store, collisions, allIssues)
	if err != nil {
		t.Fatalf("RemapCollisions failed: %v", err)
	}

	// Verify ID mapping was created
	if len(idMapping) != 2 {
		t.Errorf("expected 2 ID mappings, got %d", len(idMapping))
	}

	newID2, ok := idMapping["bd-2"]
	if !ok {
		t.Fatal("bd-2 was not remapped")
	}
	newID3, ok := idMapping["bd-3"]
	if !ok {
		t.Fatal("bd-3 was not remapped")
	}

	// Verify new issues were created with new IDs
	remappedIssue2, err := store.GetIssue(ctx, newID2)
	if err != nil {
		t.Fatalf("failed to get remapped issue %s: %v", newID2, err)
	}
	if remappedIssue2 == nil {
		t.Fatalf("remapped issue %s not found", newID2)
	}
	if remappedIssue2.Title != "Collision 2" {
		t.Errorf("unexpected title for remapped issue: %s", remappedIssue2.Title)
	}

	// Verify references in existing issue were updated
	updatedExisting, err := store.GetIssue(ctx, "bd-10")
	if err != nil {
		t.Fatalf("failed to get updated existing issue: %v", err)
	}

	// Check that description was updated
	if updatedExisting.Description != fmt.Sprintf("This mentions %s and %s", newID2, newID3) {
		t.Errorf("description was not updated correctly. Got: %q", updatedExisting.Description)
	}

	// Check that notes were updated
	if updatedExisting.Notes != fmt.Sprintf("Also %s here", newID2) {
		t.Errorf("notes were not updated correctly. Got: %q", updatedExisting.Notes)
	}
}

// BenchmarkReplaceIDReferences benchmarks the old approach (compiling regex every time)
func BenchmarkReplaceIDReferences(b *testing.B) {
	// Simulate a realistic scenario: 10 ID mappings
	idMapping := make(map[string]string)
	for i := 1; i <= 10; i++ {
		idMapping[fmt.Sprintf("bd-%d", i)] = fmt.Sprintf("bd-%d", i+100)
	}

	text := "This mentions bd-1, bd-2, bd-3, bd-4, and bd-5 multiple times. " +
		"Also bd-6, bd-7, bd-8, bd-9, and bd-10 are referenced here."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = replaceIDReferences(text, idMapping)
	}
}

// BenchmarkReplaceIDReferencesWithCache benchmarks the new cached approach
func BenchmarkReplaceIDReferencesWithCache(b *testing.B) {
	// Simulate a realistic scenario: 10 ID mappings
	idMapping := make(map[string]string)
	for i := 1; i <= 10; i++ {
		idMapping[fmt.Sprintf("bd-%d", i)] = fmt.Sprintf("bd-%d", i+100)
	}

	text := "This mentions bd-1, bd-2, bd-3, bd-4, and bd-5 multiple times. " +
		"Also bd-6, bd-7, bd-8, bd-9, and bd-10 are referenced here."

	// Pre-compile the cache (this is done once in real usage)
	cache, err := BuildReplacementCache(idMapping)
	if err != nil {
		b.Fatalf("failed to build cache: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ReplaceIDReferencesWithCache(text, cache)
	}
}

// BenchmarkReplaceIDReferencesMultipleTexts simulates the real-world scenario:
// processing multiple text fields (4 per issue) across 100 issues
func BenchmarkReplaceIDReferencesMultipleTexts(b *testing.B) {
	// 10 ID mappings (typical collision scenario)
	idMapping := make(map[string]string)
	for i := 1; i <= 10; i++ {
		idMapping[fmt.Sprintf("bd-%d", i)] = fmt.Sprintf("bd-%d", i+100)
	}

	// Simulate 100 issues with 4 text fields each
	texts := make([]string, 400)
	for i := 0; i < 400; i++ {
		texts[i] = fmt.Sprintf("Issue %d mentions bd-1, bd-2, and bd-5", i)
	}

	b.Run("without cache", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, text := range texts {
				_ = replaceIDReferences(text, idMapping)
			}
		}
	})

	b.Run("with cache", func(b *testing.B) {
		cache, _ := BuildReplacementCache(idMapping)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, text := range texts {
				_ = ReplaceIDReferencesWithCache(text, cache)
			}
		}
	})
}

// TestDetectCollisionsReadOnly verifies that DetectCollisions does not modify the database
func TestDetectCollisionsReadOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "collision-readonly-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Create an issue in the database
	dbIssue := &types.Issue{
		ID:          "bd-1",
		Title:       "Original issue",
		Description: "Original content",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, dbIssue, "test"); err != nil {
		t.Fatalf("failed to create DB issue: %v", err)
	}

	// Create incoming issue with SAME CONTENT but DIFFERENT ID (rename scenario)
	incomingIssue := &types.Issue{
		ID:          "bd-100",
		Title:       "Original issue",
		Description: "Original content",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	// Call DetectCollisions
	result, err := DetectCollisions(ctx, store, []*types.Issue{incomingIssue})
	if err != nil {
		t.Fatalf("DetectCollisions failed: %v", err)
	}

	// Verify rename was detected
	if len(result.Renames) != 1 {
		t.Fatalf("expected 1 rename, got %d", len(result.Renames))
	}
	if result.Renames[0].OldID != "bd-1" {
		t.Errorf("expected OldID bd-1, got %s", result.Renames[0].OldID)
	}
	if result.Renames[0].NewID != "bd-100" {
		t.Errorf("expected NewID bd-100, got %s", result.Renames[0].NewID)
	}

	// CRITICAL: Verify the old issue still exists in the database (not deleted)
	oldIssue, err := store.GetIssue(ctx, "bd-1")
	if err != nil {
		t.Fatalf("failed to get old issue: %v", err)
	}
	if oldIssue == nil {
		t.Fatal("old issue bd-1 was deleted - DetectCollisions is not read-only!")
	}
	if oldIssue.Title != "Original issue" {
		t.Errorf("old issue was modified - expected title 'Original issue', got '%s'", oldIssue.Title)
	}
}

// TestApplyCollisionResolution verifies that ApplyCollisionResolution correctly applies renames
func TestApplyCollisionResolution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apply-resolution-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Create an issue to be renamed
	oldIssue := &types.Issue{
		ID:          "bd-1",
		Title:       "Issue to rename",
		Description: "Content",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	if err := store.CreateIssue(ctx, oldIssue, "test"); err != nil {
		t.Fatalf("failed to create old issue: %v", err)
	}

	// Create a collision result with a rename
	newIssue := &types.Issue{
		ID:          "bd-100",
		Title:       "Issue to rename",
		Description: "Content",
		Status:      types.StatusOpen,
		Priority:    1,
		IssueType:   types.TypeTask,
	}
	result := &CollisionResult{
		Renames: []*RenameDetail{
			{
				OldID: "bd-1",
				NewID: "bd-100",
				Issue: newIssue,
			},
		},
	}

	// Apply the resolution
	emptyMapping := make(map[string]string)
	if err := ApplyCollisionResolution(ctx, store, result, emptyMapping); err != nil {
		t.Fatalf("ApplyCollisionResolution failed: %v", err)
	}

	// Verify old issue was deleted
	oldDeleted, err := store.GetIssue(ctx, "bd-1")
	if err != nil {
		t.Fatalf("failed to check old issue: %v", err)
	}
	if oldDeleted != nil {
		t.Error("old issue bd-1 was not deleted")
	}
}
