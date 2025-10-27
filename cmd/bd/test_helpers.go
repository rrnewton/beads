package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"gopkg.in/yaml.v3"
)

// setupTestEnv creates a test environment with .beads/config.yaml and returns
// the temp directory path and a cleanup function.
// The cleanup function should be deferred.
func setupTestEnv(t *testing.T, prefix string) (tmpDir string, cleanup func()) {
	tmpDir = t.TempDir()

	// Create .beads directory and config.yaml
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads directory: %v", err)
	}

	// Write config with specified prefix
	configPath := filepath.Join(beadsDir, "config.yaml")
	configData := map[string]interface{}{
		"issue_prefix": prefix,
		"backend":      "sqlite",
	}
	configBytes, _ := yaml.Marshal(configData)
	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		t.Fatalf("Failed to write config.yaml: %v", err)
	}

	// Change to temp directory and initialize config
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}
	if err := config.Initialize(); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	cleanup = func() {
		os.Chdir(origWd)
	}

	return tmpDir, cleanup
}

// setupTestStore creates a test environment with database and returns
// the store, temp directory, and cleanup function.
func setupTestStore(t *testing.T, prefix string) (*sqlite.SQLiteStorage, string, func()) {
	tmpDir, envCleanup := setupTestEnv(t, prefix)

	beadsDir := filepath.Join(tmpDir, ".beads")
	dbPath := filepath.Join(beadsDir, prefix+".db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		envCleanup()
		t.Fatalf("Failed to create test database: %v", err)
	}

	cleanup := func() {
		store.Close()
		envCleanup()
	}

	return store, tmpDir, cleanup
}
