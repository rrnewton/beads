package sqlite

import (
	"testing"

	"github.com/steveyegge/beads/internal/config"
)

// newTestStore creates a SQLiteStorage with issue-prefix configured (bd-166)
// This prevents "database not initialized" errors in tests
func newTestStore(t *testing.T, dbPath string) *SQLiteStorage {
	t.Helper()

	// Initialize config package (needed for config.SetIssuePrefix)
	if err := config.Initialize(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	// Set issue prefix in config (source of truth)
	if err := config.SetIssuePrefix("bd"); err != nil {
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	return store
}
