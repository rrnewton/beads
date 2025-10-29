package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"gopkg.in/yaml.v3"
)

// newTestStore creates a SQLite store with issue_prefix configured (bd-166)
// This prevents "database not initialized" errors in tests
func newTestStore(t *testing.T, dbPath string) *sqlite.SQLiteStorage {
	t.Helper()

	// Initialize config package (needed for config.SetIssuePrefix)
	if err := config.Initialize(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	// Set issue prefix in config (source of truth)
	if err := config.SetIssuePrefix("test"); err != nil {
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("Failed to create database directory: %v", err)
	}

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	t.Cleanup(func() { store.Close() })
	return store
}

// newTestStoreWithPrefix creates a SQLite store with custom issue_prefix configured
func newTestStoreWithPrefix(t *testing.T, dbPath string, prefix string) *sqlite.SQLiteStorage {
	t.Helper()

	// Initialize config package (needed for config.SetIssuePrefix)
	if err := config.Initialize(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	// Set issue prefix in config (source of truth)
	if err := config.SetIssuePrefix(prefix); err != nil {
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("Failed to create database directory: %v", err)
	}

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	t.Cleanup(func() { store.Close() })
	return store
}

// openExistingTestDB opens an existing database without modifying it.
// Used in tests where the database was already created by the code under test.
func openExistingTestDB(t *testing.T, dbPath string) (*sqlite.SQLiteStorage, error) {
	t.Helper()
	return sqlite.New(dbPath)
}

// readIssuePrefixFromConfig reads the issue-prefix from config.yaml in the .beads directory
// relative to the given database path. Used in tests to verify that init command
// correctly wrote the prefix to config.yaml.
func readIssuePrefixFromConfig(t *testing.T, dbPath string) (string, error) {
	t.Helper()

	// Get the directory containing the database
	dbDir := filepath.Dir(dbPath)

	// Try multiple locations for config.yaml:
	// 1. In the same directory as the database (for .beads/config.yaml structure)
	// 2. In a .beads subdirectory next to the database (for custom DB paths)
	configPaths := []string{
		filepath.Join(dbDir, "config.yaml"),
		filepath.Join(dbDir, ".beads", "config.yaml"),
	}

	var data []byte
	var err error
	for _, configPath := range configPaths {
		data, err = os.ReadFile(configPath)
		if err == nil {
			break
		}
	}

	if err != nil {
		return "", fmt.Errorf("config.yaml not found in any expected location: %w", err)
	}

	// Parse YAML to extract issue-prefix
	var configData map[string]interface{}
	if err := yaml.Unmarshal(data, &configData); err != nil {
		// Fallback: try simple string parsing
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "issue-prefix:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1]), nil
				}
			}
		}
		return "", err
	}

	// Extract issue-prefix from parsed config
	if prefix, ok := configData["issue-prefix"].(string); ok {
		return prefix, nil
	}

	return "", nil
}
