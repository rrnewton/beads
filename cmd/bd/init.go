package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/markdown"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize bd in the current directory",
	Long: `Initialize bd in the current directory by creating a .beads/ directory
and database file. Optionally specify a custom issue prefix.

Backend options (--backend):
  sqlite:   SQLite database (default) - .beads/<prefix>.db
  markdown: Human-readable markdown files - .beads/markdown.db/issues/*.md

With --no-db: creates .beads/ directory and nodb_prefix.txt file instead of SQLite database.`,
	Run: func(cmd *cobra.Command, _ []string) {
		prefix, _ := cmd.Flags().GetString("prefix")
		quiet, _ := cmd.Flags().GetBool("quiet")
		backend, _ := cmd.Flags().GetString("backend")

		// Validate backend
		if backend != "sqlite" && backend != "markdown" {
			fmt.Fprintf(os.Stderr, "Error: invalid backend '%s'. Must be 'sqlite' or 'markdown'\n", backend)
			os.Exit(1)
		}

		// Check BEADS_DB environment variable if --db flag not set
		// (PersistentPreRun doesn't run for init command)
		if dbPath == "" {
			if envDB := os.Getenv("BEADS_DB"); envDB != "" {
				dbPath = envDB
			}
		}

		if prefix == "" {
			// Auto-detect from directory name
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
				os.Exit(1)
			}
			prefix = filepath.Base(cwd)
		}

		// Normalize prefix: strip trailing hyphens
		// The hyphen is added automatically during ID generation
		prefix = strings.TrimRight(prefix, "-")

		// Determine storage path based on backend
		var storePath string
		if dbPath != "" {
			storePath = dbPath
		} else {
			if backend == "markdown" {
				storePath = filepath.Join(".beads", "markdown.db")
			} else {
				storePath = filepath.Join(".beads", prefix+".db")
			}
		}
		
		// Determine if we should create .beads/ directory in CWD
		// Only create it if the database will be stored there
	cwd, err := os.Getwd()
		if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to get current directory: %v\n", err)
		os.Exit(1)
	}
	
	localBeadsDir := filepath.Join(cwd, ".beads")
	storeDir := filepath.Dir(storePath)

	// Convert both to absolute paths for comparison
	localBeadsDirAbs, err := filepath.Abs(localBeadsDir)
	if err != nil {
		localBeadsDirAbs = filepath.Clean(localBeadsDir)
	}
	storeDirAbs, err := filepath.Abs(storeDir)
	if err != nil {
		storeDirAbs = filepath.Clean(storeDir)
	}

	useLocalBeads := filepath.Clean(storeDirAbs) == filepath.Clean(localBeadsDirAbs)
	
	if useLocalBeads {
		// Create .beads directory
		if err := os.MkdirAll(localBeadsDir, 0750); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create .beads directory: %v\n", err)
			os.Exit(1)
		}

		// Handle --no-db mode: create nodb_prefix.txt instead of database
		if noDb {
			prefixFile := filepath.Join(localBeadsDir, "nodb_prefix.txt")
			if err := os.WriteFile(prefixFile, []byte(prefix+"\n"), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to write prefix file: %v\n", err)
				os.Exit(1)
			}

			// Create empty issues.jsonl file
			jsonlPath := filepath.Join(localBeadsDir, "issues.jsonl")
			if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
				if err := os.WriteFile(jsonlPath, []byte{}, 0644); err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to create issues.jsonl: %v\n", err)
					os.Exit(1)
				}
			}

			if !quiet {
				green := color.New(color.FgGreen).SprintFunc()
				cyan := color.New(color.FgCyan).SprintFunc()

				fmt.Printf("\n%s bd initialized successfully in --no-db mode!\n\n", green("✓"))
				fmt.Printf("  Mode: %s\n", cyan("no-db (JSONL-only)"))
				fmt.Printf("  Issues file: %s\n", cyan(jsonlPath))
				fmt.Printf("  Issue prefix: %s\n", cyan(prefix))
				fmt.Printf("  Issues will be named: %s\n\n", cyan(prefix+"-1, "+prefix+"-2, ..."))
				fmt.Printf("Run %s to get started.\n\n", cyan("bd --no-db quickstart"))
			}
			return
		}

		// Create or update config.yaml in .beads directory
		configPath := filepath.Join(localBeadsDir, "config.yaml")
		configData := make(map[string]interface{})

		// Check if config already exists (from version control)
		if existingData, err := os.ReadFile(configPath); err == nil {
			// Parse existing config
			if err := yaml.Unmarshal(existingData, &configData); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse existing config.yaml: %v\n", err)
				configData = make(map[string]interface{})
			}
		}

		// Only set values if not already present (respect existing config)
		if _, exists := configData["backend"]; !exists {
			configData["backend"] = backend
		}
		if _, exists := configData["issue_prefix"]; !exists {
			configData["issue_prefix"] = prefix
		}
		if backend == "markdown" {
			if _, exists := configData["no-db"]; !exists {
				configData["no-db"] = false
			}
		}

		configBytes, err := yaml.Marshal(configData)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to marshal config: %v\n", err)
		} else {
			if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to create config.yaml: %v\n", err)
			}
		}

		// Create .gitignore in .beads directory
		gitignorePath := filepath.Join(localBeadsDir, ".gitignore")
		var gitignoreContent string
		if backend == "markdown" {
			gitignoreContent = `# Markdown backend temporary files
*.tmp.*
*.lock.*
*.trash.*

# Daemon runtime files
daemon.log
daemon.pid
bd.sock

# Keep markdown issues (source of truth for git)
!markdown.db/issues/*.md
`
		} else {
			gitignoreContent = `# SQLite databases
*.db
*.db-journal
*.db-wal
*.db-shm

# Daemon runtime files
daemon.log
daemon.pid
bd.sock

# Legacy database files
db.sqlite
bd.db

# Keep JSONL exports (source of truth for git)
!*.jsonl
`
		}
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0600); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create .gitignore: %v\n", err)
			// Non-fatal - continue anyway
		}
		}
	
		// Ensure parent directory exists for the storage
		if err := os.MkdirAll(storeDir, 0750); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create storage directory %s: %v\n", storeDir, err)
			os.Exit(1)
		}

		// Create storage backend
		var store storage.Storage
		if backend == "markdown" {
			store, err = markdown.New(storePath)
		} else {
			store, err = sqlite.New(storePath)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create %s backend: %v\n", backend, err)
			os.Exit(1)
		}

		// Prefix is now stored only in .beads/config.yaml (single source of truth)
		ctx := context.Background()

		// Store the bd version in metadata (for version mismatch detection)
		if err := store.SetMetadata(ctx, "bd_version", Version); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to store version metadata: %v\n", err)
		// Non-fatal - continue anyway
		}

		// Check if git has existing issues to import (fresh clone scenario)
		issueCount, jsonlPath := checkGitForIssues()
		if issueCount > 0 {
		if !quiet {
		fmt.Fprintf(os.Stderr, "\n✓ Database initialized. Found %d issues in git, importing...\n", issueCount)
		}
		
		if err := importFromGit(ctx, storePath, store, jsonlPath); err != nil {
		if !quiet {
		fmt.Fprintf(os.Stderr, "Warning: auto-import failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Try manually: git show HEAD:%s | bd import -i /dev/stdin\n", jsonlPath)
		}
		// Non-fatal - continue with empty database
		} else if !quiet {
		fmt.Fprintf(os.Stderr, "✓ Successfully imported %d issues from git.\n\n", issueCount)
		}
}

if err := store.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to close database: %v\n", err)
}

// Skip output if quiet mode
if quiet {
		return
}

		green := color.New(color.FgGreen).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()

		fmt.Printf("\n%s bd initialized successfully!\n\n", green("✓"))
		fmt.Printf("  Backend: %s\n", cyan(backend))
		if backend == "markdown" {
			fmt.Printf("  Storage: %s\n", cyan(storePath))
			fmt.Printf("  Issues directory: %s\n", cyan(filepath.Join(storePath, "issues")))
		} else {
			fmt.Printf("  Database: %s\n", cyan(storePath))
		}
		fmt.Printf("  Issue prefix: %s\n", cyan(prefix))
		fmt.Printf("  Issues will be named: %s\n\n", cyan(prefix+"-1, "+prefix+"-2, ..."))
	
	// Check if we're in a git repo and hooks aren't installed
	if isGitRepo() && !hooksInstalled() {
		fmt.Printf("%s Git hooks not installed\n", yellow("⚠"))
		fmt.Printf("  Install git hooks to prevent race conditions between commits and auto-flush.\n")
		fmt.Printf("  Run: %s\n\n", cyan("./examples/git-hooks/install.sh"))
		
		// Prompt to install
		fmt.Printf("Install git hooks now? [Y/n] ")
		var response string
		_, _ = fmt.Scanln(&response) // ignore EOF on empty input
		response = strings.ToLower(strings.TrimSpace(response))
		
		if response == "" || response == "y" || response == "yes" {
			if err := installGitHooks(); err != nil {
				fmt.Fprintf(os.Stderr, "Error installing hooks: %v\n", err)
				fmt.Printf("You can install manually with: %s\n\n", cyan("./examples/git-hooks/install.sh"))
			} else {
				fmt.Printf("%s Git hooks installed successfully!\n\n", green("✓"))
			}
		}
	}
	
	fmt.Printf("Run %s to get started.\n\n", cyan("bd quickstart"))
	},
}

func init() {
	initCmd.Flags().StringP("prefix", "p", "", "Issue prefix (default: current directory name)")
	initCmd.Flags().BoolP("quiet", "q", false, "Suppress output (quiet mode)")
	initCmd.Flags().String("backend", "sqlite", "Storage backend: sqlite or markdown")
	rootCmd.AddCommand(initCmd)
}

// hooksInstalled checks if bd git hooks are installed
func hooksInstalled() bool {
	preCommit := filepath.Join(".git", "hooks", "pre-commit")
	postMerge := filepath.Join(".git", "hooks", "post-merge")
	
	// Check if both hooks exist
	_, err1 := os.Stat(preCommit)
	_, err2 := os.Stat(postMerge)
	
	if err1 != nil || err2 != nil {
		return false
	}
	
	// Verify they're bd hooks by checking for signature comment
	preCommitContent, err := os.ReadFile(preCommit)
	if err != nil || !strings.Contains(string(preCommitContent), "bd (beads) pre-commit hook") {
		return false
	}
	
	postMergeContent, err := os.ReadFile(postMerge)
	if err != nil || !strings.Contains(string(postMergeContent), "bd (beads) post-merge hook") {
		return false
	}
	
	return true
}

// installGitHooks runs the install script
func installGitHooks() error {
	// Find the install script
	installScript := filepath.Join("examples", "git-hooks", "install.sh")
	
	// Check if script exists
	if _, err := os.Stat(installScript); err != nil {
		return fmt.Errorf("install script not found at %s", installScript)
	}
	
	// Run the install script
	cmd := exec.Command("/bin/bash", installScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}
