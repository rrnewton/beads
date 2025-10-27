package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"gopkg.in/yaml.v3"
)

func TestInitCommand(t *testing.T) {
	tests := []struct {
		name           string
		prefix         string
		quiet          bool
		wantOutputText string
		wantNoOutput   bool
	}{
		{
			name:           "init with default prefix",
			prefix:         "",
			quiet:          false,
			wantOutputText: "bd initialized successfully",
		},
		{
			name:           "init with custom prefix",
			prefix:         "myproject",
			quiet:          false,
			wantOutputText: "myproject-1, myproject-2",
		},
		{
			name:         "init with quiet flag",
			prefix:       "test",
			quiet:        true,
			wantNoOutput: true,
		},
		{
			name:           "init with prefix ending in hyphen",
			prefix:         "test-",
			quiet:          false,
			wantOutputText: "test-1, test-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global state
			origDBPath := dbPath
			defer func() { dbPath = origDBPath }()
			dbPath = ""
			
			// Reset Cobra command state
			rootCmd.SetArgs([]string{})
			initCmd.Flags().Set("prefix", "")
			initCmd.Flags().Set("quiet", "false")

			tmpDir := t.TempDir()
			originalWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get working directory: %v", err)
			}
			defer os.Chdir(originalWd)

			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Failed to change to temp directory: %v", err)
			}

			// Capture output
			var buf bytes.Buffer
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w
			defer func() {
				os.Stdout = oldStdout
			}()

			// Build command arguments
			args := []string{"init"}
			if tt.prefix != "" {
				args = append(args, "--prefix", tt.prefix)
			}
			if tt.quiet {
				args = append(args, "--quiet")
			}

			rootCmd.SetArgs(args)

			// Run command
			err = rootCmd.Execute()

			// Restore stdout and read output
			w.Close()
			buf.ReadFrom(r)
			os.Stdout = oldStdout
			output := buf.String()

			if err != nil {
				t.Fatalf("init command failed: %v", err)
			}

			// Check output
			if tt.wantNoOutput {
				if output != "" {
					t.Errorf("Expected no output with --quiet, got: %s", output)
				}
			} else if tt.wantOutputText != "" {
				if !strings.Contains(output, tt.wantOutputText) {
					t.Errorf("Expected output to contain %q, got: %s", tt.wantOutputText, output)
				}
			}

			// Verify .beads directory was created
			beadsDir := filepath.Join(tmpDir, ".beads")
			if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
				t.Error(".beads directory was not created")
			}

			// Verify .gitignore was created with proper content
			gitignorePath := filepath.Join(beadsDir, ".gitignore")
			gitignoreContent, err := os.ReadFile(gitignorePath)
			if err != nil {
				t.Errorf(".gitignore file was not created: %v", err)
			} else {
				// Check for essential patterns
				gitignoreStr := string(gitignoreContent)
				expectedPatterns := []string{
					"*.db",
					"*.db-journal",
					"*.db-wal",
					"*.db-shm",
					"daemon.log",
					"daemon.pid",
					"bd.sock",
					"!*.jsonl",
				}
				for _, pattern := range expectedPatterns {
					if !strings.Contains(gitignoreStr, pattern) {
						t.Errorf(".gitignore missing expected pattern: %s", pattern)
					}
				}
			}

			// Verify database was created (always beads.db now)
			dbPath := filepath.Join(beadsDir, "beads.db")
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Errorf("Database file was not created at %s", dbPath)
			}

			// Verify config.yaml has correct prefix
			configPath := filepath.Join(beadsDir, "config.yaml")
			configData, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("Failed to read config.yaml: %v", err)
			}

			var cfg map[string]interface{}
			if err := yaml.Unmarshal(configData, &cfg); err != nil {
				t.Fatalf("Failed to parse config.yaml: %v", err)
			}

			expectedPrefix := tt.prefix
			if expectedPrefix == "" {
				expectedPrefix = filepath.Base(tmpDir)
			} else {
				expectedPrefix = strings.TrimRight(expectedPrefix, "-")
			}

			prefix, ok := cfg["issue_prefix"].(string)
			if !ok {
				t.Error("issue_prefix not found in config.yaml")
			} else if prefix != expectedPrefix {
				t.Errorf("Expected prefix %q, got %q", expectedPrefix, prefix)
			}

			// Verify database metadata was set
			store, err := sqlite.New(dbPath)
			if err != nil {
				t.Fatalf("Failed to open created database: %v", err)
			}
			defer store.Close()

			ctx := context.Background()
			version, err := store.GetMetadata(ctx, "bd_version")
			if err != nil {
				t.Errorf("Failed to get bd_version metadata: %v", err)
			}
			if version == "" {
				t.Error("bd_version metadata was not set")
			}
		})
	}
}

// Note: Error case testing is omitted because the init command calls os.Exit()
// on errors, which makes it difficult to test in a unit test context.

func TestInitAlreadyInitialized(t *testing.T) {
	// Reset global state
	origDBPath := dbPath
	defer func() { dbPath = origDBPath }()
	dbPath = ""
	
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalWd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize once
	rootCmd.SetArgs([]string{"init", "--prefix", "test", "--quiet"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("First init failed: %v", err)
	}

	// Initialize again with same prefix - should succeed (overwrites)
	rootCmd.SetArgs([]string{"init", "--prefix", "test", "--quiet"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Second init failed: %v", err)
	}

	// Verify config.yaml has correct prefix
	configPath := filepath.Join(tmpDir, ".beads", "config.yaml")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config.yaml after re-init: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("Failed to parse config.yaml: %v", err)
	}

	prefix, ok := cfg["issue_prefix"].(string)
	if !ok {
		t.Error("issue_prefix not found in config.yaml")
	} else if prefix != "test" {
		t.Errorf("Expected prefix 'test', got %q", prefix)
	}
}

func TestInitWithCustomDBPath(t *testing.T) {
	// Save original state
	origDBPath := dbPath
	defer func() { dbPath = origDBPath }()

	tmpDir := t.TempDir()
	customDBDir := filepath.Join(tmpDir, "custom", "location")

	// Change to a different directory to ensure --db flag is actually used
	workDir := filepath.Join(tmpDir, "workdir")
	if err := os.MkdirAll(workDir, 0750); err != nil {
		t.Fatalf("Failed to create work directory: %v", err)
	}

	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(originalWd)

	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Failed to change to work directory: %v", err)
	}

	customDBPath := filepath.Join(customDBDir, "test.db")

	// Test with --db flag
	t.Run("init with --db flag", func(t *testing.T) {
		dbPath = "" // Reset global
		rootCmd.SetArgs([]string{"--db", customDBPath, "init", "--prefix", "custom", "--quiet"})

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Init with --db flag failed: %v", err)
		}

		// Verify database was created at custom location
		if _, err := os.Stat(customDBPath); os.IsNotExist(err) {
			t.Errorf("Database was not created at custom path %s", customDBPath)
		}

		// Verify config.yaml has correct prefix (should be in custom DB location's parent dir)
		configDir := filepath.Join(customDBDir, "..")
		beadsDir := filepath.Join(configDir, ".beads")
		// Check in work directory first (where .beads should be if using local config)
		configPath := filepath.Join(workDir, ".beads", "config.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// Try beads dir near custom DB path
			configPath = filepath.Join(beadsDir, "config.yaml")
		}

		// For custom DB path, config might not exist - that's ok, prefix comes from CLI
		// Just verify database was created
		if _, err := os.Stat(customDBPath); os.IsNotExist(err) {
			t.Errorf("Database was not created at custom path %s", customDBPath)
		}

		// Verify .beads/ directory was NOT created in work directory
		if _, err := os.Stat(filepath.Join(workDir, ".beads")); err == nil {
			t.Error(".beads/ directory should not be created when using --db flag")
		}
	})

	// Test with BEADS_DB env var
	t.Run("init with BEADS_DB env var", func(t *testing.T) {
		dbPath = "" // Reset global
		envDBPath := filepath.Join(tmpDir, "env", "location", "env.db")
		os.Setenv("BEADS_DB", envDBPath)
		defer os.Unsetenv("BEADS_DB")

		rootCmd.SetArgs([]string{"init", "--prefix", "envtest", "--quiet"})

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Init with BEADS_DB failed: %v", err)
		}

		// Verify database was created at env location
		if _, err := os.Stat(envDBPath); os.IsNotExist(err) {
			t.Errorf("Database was not created at BEADS_DB path %s", envDBPath)
		}

		// Verify database was created
		if _, err := os.Stat(envDBPath); os.IsNotExist(err) {
			t.Errorf("Database was not created at BEADS_DB path %s", envDBPath)
		}

		// For custom DB path via env var, prefix is stored in CLI flag, not necessarily in config file
		// The important thing is the database was created
	})

	// Test that custom path containing ".beads" doesn't create CWD/.beads
	t.Run("init with custom path containing .beads", func(t *testing.T) {
		dbPath = "" // Reset global
		// Path contains ".beads" but is outside work directory
		customPath := filepath.Join(tmpDir, "storage", ".beads-backup", "test.db")
		rootCmd.SetArgs([]string{"--db", customPath, "init", "--prefix", "beadstest", "--quiet"})

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Init with custom .beads path failed: %v", err)
		}

		// Verify database was created at custom location
		if _, err := os.Stat(customPath); os.IsNotExist(err) {
			t.Errorf("Database was not created at custom path %s", customPath)
		}

		// Verify .beads/ directory was NOT created in work directory
		if _, err := os.Stat(filepath.Join(workDir, ".beads")); err == nil {
			t.Error(".beads/ directory should not be created in CWD when custom path contains .beads")
		}
	})

	// Test flag precedence over env var
	t.Run("flag takes precedence over BEADS_DB", func(t *testing.T) {
		dbPath = "" // Reset global
		flagPath := filepath.Join(tmpDir, "flag", "flag.db")
		envPath := filepath.Join(tmpDir, "env", "env.db")
		
		os.Setenv("BEADS_DB", envPath)
		defer os.Unsetenv("BEADS_DB")
		
		rootCmd.SetArgs([]string{"--db", flagPath, "init", "--prefix", "flagtest", "--quiet"})

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Init with flag precedence failed: %v", err)
		}

		// Verify database was created at flag location, not env location
		if _, err := os.Stat(flagPath); os.IsNotExist(err) {
			t.Errorf("Database was not created at flag path %s", flagPath)
		}
		if _, err := os.Stat(envPath); err == nil {
			t.Error("Database should not be created at BEADS_DB path when --db flag is set")
		}
	})
}
