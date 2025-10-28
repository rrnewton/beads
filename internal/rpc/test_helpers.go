package rpc

import (
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

// newTestStore creates a SQLite store with issue-prefix configured (bd-166)
// This prevents "database not initialized" errors in tests
func newTestStore(t *testing.T, dbPath string) *sqlite.SQLiteStorage {
	t.Helper()

	// Initialize config package for tests
	if err := config.Initialize(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// CRITICAL (bd-166): Set issue-prefix to prevent "database not initialized" errors
	if err := config.SetIssuePrefix("bd"); err != nil {
		store.Close()
		t.Fatalf("Failed to set issue-prefix: %v", err)
	}

	return store
}
