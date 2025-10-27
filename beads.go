// Package beads provides a minimal public API for extending bd with custom orchestration.
//
// Most extensions should use direct SQL queries against bd's database.
// This package exports only the essential types and functions needed for
// Go-based extensions that want to use bd's storage layer programmatically.
//
// For detailed guidance on extending bd, see EXTENDING.md.
package beads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// CanonicalDatabaseName is the required database filename for all beads repositories
const CanonicalDatabaseName = "beads.db"

// LegacyDatabaseNames are old names that should be migrated
var LegacyDatabaseNames = []string{"bd.db", "issues.db", "bugs.db"}

// Issue represents a tracked work item with metadata, dependencies, and status.
type (
	Issue = types.Issue
	// Status represents the current state of an issue (open, in progress, closed, blocked).
	Status = types.Status
	// IssueType represents the type of issue (bug, feature, task, epic, chore).
	IssueType = types.IssueType
	// Dependency represents a relationship between issues.
	Dependency = types.Dependency
	// DependencyType represents the type of dependency (blocks, related, parent-child, discovered-from).
	DependencyType = types.DependencyType
	// Comment represents a user comment on an issue.
	Comment = types.Comment
	// Event represents an audit log event.
	Event = types.Event
	// EventType represents the type of audit event.
	EventType = types.EventType
	// Label represents a tag attached to an issue.
	Label = types.Label
	// BlockedIssue represents an issue with blocking dependencies.
	BlockedIssue = types.BlockedIssue
	// TreeNode represents a node in a dependency tree.
	TreeNode = types.TreeNode
	// Statistics represents project-wide metrics.
	Statistics = types.Statistics
	// IssueFilter represents filtering criteria for issue queries.
	IssueFilter = types.IssueFilter
	// WorkFilter represents filtering criteria for work queries.
	WorkFilter = types.WorkFilter
	// SortPolicy determines how ready work is ordered.
	SortPolicy = types.SortPolicy
	// EpicStatus represents the status of an epic issue.
	EpicStatus = types.EpicStatus
)

// Status constants
const (
	StatusOpen       = types.StatusOpen
	StatusInProgress = types.StatusInProgress
	StatusClosed     = types.StatusClosed
	StatusBlocked    = types.StatusBlocked
)

// IssueType constants
const (
	TypeBug     = types.TypeBug
	TypeFeature = types.TypeFeature
	TypeTask    = types.TypeTask
	TypeEpic    = types.TypeEpic
	TypeChore   = types.TypeChore
)

// DependencyType constants
const (
	DepBlocks         = types.DepBlocks
	DepRelated        = types.DepRelated
	DepParentChild    = types.DepParentChild
	DepDiscoveredFrom = types.DepDiscoveredFrom
)

// SortPolicy constants
const (
	SortPolicyHybrid   = types.SortPolicyHybrid
	SortPolicyPriority = types.SortPolicyPriority
	SortPolicyOldest   = types.SortPolicyOldest
)

// EventType constants
const (
	EventCreated           = types.EventCreated
	EventUpdated           = types.EventUpdated
	EventStatusChanged     = types.EventStatusChanged
	EventCommented         = types.EventCommented
	EventClosed            = types.EventClosed
	EventReopened          = types.EventReopened
	EventDependencyAdded   = types.EventDependencyAdded
	EventDependencyRemoved = types.EventDependencyRemoved
	EventLabelAdded        = types.EventLabelAdded
	EventLabelRemoved      = types.EventLabelRemoved
	EventCompacted         = types.EventCompacted
)

// Storage provides the minimal interface for extension orchestration
type Storage = storage.Storage

// NewSQLiteStorage opens a bd SQLite database for programmatic access.
// Most extensions should use this to query ready work and update issue status.
func NewSQLiteStorage(dbPath string) (Storage, error) {
	return sqlite.New(dbPath)
}

// FindDatabasePath discovers the bd database path using bd's standard search order:
//  1. $BEADS_DB environment variable
//  2. .beads/*.db in current directory or ancestors
//
// Returns empty string if no database is found.
func FindDatabasePath() string {
	// 1. Check environment variable
	if envDB := os.Getenv("BEADS_DB"); envDB != "" {
		return envDB
	}

	// 2. Search for .beads/*.db in current directory and ancestors
	if foundDB := findDatabaseInTree(); foundDB != "" {
		return foundDB
	}

	// No fallback to ~/.beads - return empty string
	return ""
}

// FindJSONLPath returns the canonical JSONL file path for the given database path.
// This always returns "issues.jsonl" in the database directory as per bd's standard.
//
// This function does not create directories or files - it only constructs the path.
// Use this when you need to know where bd stores its JSONL export.
func FindJSONLPath(dbPath string) string {
	if dbPath == "" {
		return ""
	}

	// Get the directory containing the database
	dbDir := filepath.Dir(dbPath)

	// Always use issues.jsonl as the canonical filename
	return filepath.Join(dbDir, "issues.jsonl")
}

// DatabaseInfo contains information about a discovered beads database
type DatabaseInfo struct {
	Path      string // Full path to the .db file
	BeadsDir  string // Parent .beads directory
	IssueCount int   // Number of issues (-1 if unknown)
}

// findDatabaseInTree walks up the directory tree looking for .beads/*.db or .beads/markdown_db/
// Prefers config.json, then markdown_db/, then falls back to beads.db
func findDatabaseInTree() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Walk up directory tree
	for {
		beadsDir := filepath.Join(dir, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			// Check for config.json first (single source of truth)
			if cfg, err := configfile.Load(beadsDir); err == nil && cfg != nil {
				dbPath := cfg.DatabasePath(beadsDir)
				if _, err := os.Stat(dbPath); err == nil {
					return dbPath
				}
			}

			// Check for markdown_db directory
			markdownDB := filepath.Join(beadsDir, "markdown_db")
			if mdInfo, err := os.Stat(markdownDB); err == nil && mdInfo.IsDir() {
				return markdownDB
			}

			// Fall back to canonical beads.db for backward compatibility
			canonicalDB := filepath.Join(beadsDir, CanonicalDatabaseName)
			if _, err := os.Stat(canonicalDB); err == nil {
				return canonicalDB
			}

			// Found .beads/ directory, look for *.db files
			matches, err := filepath.Glob(filepath.Join(beadsDir, "*.db"))
			if err == nil && len(matches) > 0 {
			// Filter out backup files
			var validDBs []string
			for _, match := range matches {
			baseName := filepath.Base(match)
			// Skip backup files (e.g., beads.db.backup, bd.db.backup)
			if filepath.Ext(baseName) != ".backup" {
			validDBs = append(validDBs, match)
			}
			}

			if len(validDBs) > 1 {
			// Multiple databases found - this is ambiguous
			// Print error to stderr but return the first one for backward compatibility
			fmt.Fprintf(os.Stderr, "Warning: Multiple database files found in %s:\n", beadsDir)
			for _, db := range validDBs {
			fmt.Fprintf(os.Stderr, "  - %s\n", filepath.Base(db))
			}
			fmt.Fprintf(os.Stderr, "Run 'bd init' to migrate to %s or manually remove old databases.\n\n", CanonicalDatabaseName)
			}

			if len(validDBs) > 0 {
			// Check if using legacy name and warn
			 dbName := filepath.Base(validDBs[0])
			  if dbName != CanonicalDatabaseName {
					isLegacy := false
					for _, legacy := range LegacyDatabaseNames {
						if dbName == legacy {
							isLegacy = true
							break
						}
					}
					if isLegacy {
						fmt.Fprintf(os.Stderr, "WARNING: Using legacy database name: %s\n", dbName)
						fmt.Fprintf(os.Stderr, "Run 'bd migrate' to upgrade to canonical name: %s\n\n", CanonicalDatabaseName)
					}
				}
				return validDBs[0]
			}
		}
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return ""
}

// FindAllDatabases scans the directory hierarchy for all .beads directories
// Returns a slice of DatabaseInfo for each database found, starting from the
// closest to CWD (most relevant) to the furthest (least relevant).
func FindAllDatabases() []DatabaseInfo {
	var databases []DatabaseInfo
	
	dir, err := os.Getwd()
	if err != nil {
		return databases
	}

	// Walk up directory tree
	for {
		beadsDir := filepath.Join(dir, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			// Found .beads/ directory, look for *.db files
			matches, err := filepath.Glob(filepath.Join(beadsDir, "*.db"))
			if err == nil && len(matches) > 0 {
				// Count issues if we can open the database (best-effort)
				issueCount := -1
				dbPath := matches[0]
				// Don't fail if we can't open/query the database - it might be locked
				// or corrupted, but we still want to detect and warn about it
				store, err := sqlite.New(dbPath)
				if err == nil {
					ctx := context.Background()
					if issues, err := store.SearchIssues(ctx, "", types.IssueFilter{}); err == nil {
						issueCount = len(issues)
					}
					_ = store.Close()
				}
				
				databases = append(databases, DatabaseInfo{
					Path:       dbPath,
					BeadsDir:   beadsDir,
					IssueCount: issueCount,
				})
			}
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return databases
}
