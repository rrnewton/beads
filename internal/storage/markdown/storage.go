package markdown

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// MarkdownStorage implements storage.Storage using markdown files
type MarkdownStorage struct {
	rootDir   string           // .beads/markdown.db
	issuesDir string           // .beads/markdown.db/issues
	pid       int              // current process ID
	locks     map[string]*lock // active locks held by this process
	locksMu   sync.Mutex       // protects locks map
}

// lock represents an acquired file lock
type lock struct {
	issueID  string
	lockPath string // path to .lock.<pid> file
}

// New creates a new markdown storage backend
func New(rootDir string) (*MarkdownStorage, error) {
	issuesDir := filepath.Join(rootDir, "issues")

	// Create directories if they don't exist
	if err := os.MkdirAll(issuesDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create issues directory: %w", err)
	}

	// Create other directories
	for _, dir := range []string{"comments", "events"} {
		if err := os.MkdirAll(filepath.Join(rootDir, dir), 0750); err != nil {
			return nil, fmt.Errorf("failed to create %s directory: %w", dir, err)
		}
	}

	m := &MarkdownStorage{
		rootDir:   rootDir,
		issuesDir: issuesDir,
		pid:       os.Getpid(),
		locks:     make(map[string]*lock),
	}

	// Clean up stale locks from dead processes
	if err := m.cleanupStaleLocks(); err != nil {
		return nil, fmt.Errorf("failed to cleanup stale locks: %w", err)
	}

	return m, nil
}

// Close cleans up resources and releases all locks
func (m *MarkdownStorage) Close() error {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()

	for _, lock := range m.locks {
		_ = m.unlockFile(lock)
	}
	m.locks = make(map[string]*lock)

	return nil
}

// Path returns the root directory path for the markdown storage
func (m *MarkdownStorage) Path() string {
	return m.rootDir
}

// UnderlyingDB returns nil for markdown storage (no SQL database)
func (m *MarkdownStorage) UnderlyingDB() *sql.DB {
	return nil
}

// Verify MarkdownStorage implements storage.Storage interface
var _ storage.Storage = (*MarkdownStorage)(nil)

// CreateIssue creates a new issue
func (m *MarkdownStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	issuePath := m.getIssuePath(issue.ID)

	// Check if issue already exists
	if _, err := os.Stat(issuePath); err == nil {
		return fmt.Errorf("issue already exists: %s", issue.ID)
	}

	// Convert issue to markdown
	data, err := issueToMarkdown(issue)
	if err != nil {
		return fmt.Errorf("failed to convert issue to markdown: %w", err)
	}

	// Write to temp file first
	tempPath := m.getTempPath(issue.ID)
	if err := os.WriteFile(tempPath, data, 0640); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomically rename to actual file
	if err := os.Rename(tempPath, issuePath); err != nil {
		_ = os.Remove(tempPath) // Cleanup temp file
		return fmt.Errorf("failed to create issue file: %w", err)
	}

	return nil
}

// CreateIssues creates multiple issues atomically
func (m *MarkdownStorage) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// GetIssue retrieves an issue by ID
func (m *MarkdownStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	issuePath := m.getIssuePath(id)

	// Check if file exists (handle locked files too)
	var data []byte
	var err error

	// Try to read the normal file first
	data, err = os.ReadFile(issuePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Check if it's locked by another process
			lockFiles, _ := filepath.Glob(issuePath + ".lock.*")
			if len(lockFiles) > 0 {
				// Read from lock file
				data, err = os.ReadFile(lockFiles[0])
				if err != nil {
					return nil, fmt.Errorf("issue not found: %s", id)
				}
			} else {
				return nil, fmt.Errorf("issue not found: %s", id)
			}
		} else {
			return nil, fmt.Errorf("failed to read issue: %w", err)
		}
	}

	// Parse markdown into Issue
	issue, err := markdownToIssue(id, data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse issue: %w", err)
	}

	return issue, nil
}

// UpdateIssue updates an existing issue
func (m *MarkdownStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Lock the issue file
	lock, err := m.lockFile(id)
	if err != nil {
		return fmt.Errorf("failed to lock issue: %w", err)
	}
	defer func() {
		if lock != nil {
			_ = m.unlockFile(lock)
		}
	}()

	// Read current issue from lock file
	data, err := os.ReadFile(lock.lockPath)
	if err != nil {
		return fmt.Errorf("failed to read issue: %w", err)
	}

	issue, err := markdownToIssue(id, data)
	if err != nil {
		return fmt.Errorf("failed to parse issue: %w", err)
	}

	// Apply updates
	if err := applyUpdates(issue, updates); err != nil {
		return fmt.Errorf("failed to apply updates: %w", err)
	}

	// Update timestamp
	issue.UpdatedAt = time.Now()

	// Convert to markdown
	updatedData, err := issueToMarkdown(issue)
	if err != nil {
		return fmt.Errorf("failed to convert to markdown: %w", err)
	}

	// Write to temp file
	tempPath := m.getTempPath(id)
	if err := os.WriteFile(tempPath, updatedData, 0640); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Commit changes (temp -> actual, lock -> trash)
	if err := m.commitFile(lock, tempPath); err != nil {
		_ = os.Remove(tempPath) // Cleanup temp file
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	lock = nil // Prevent defer from trying to unlock
	return nil
}

// UpdateIssueID renames an issue's ID and updates all references
func (m *MarkdownStorage) UpdateIssueID(ctx context.Context, oldID, newID string, issue *types.Issue, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// DeleteIssue deletes an issue
func (m *MarkdownStorage) DeleteIssue(ctx context.Context, id string, actor string) error {
	// Lock the issue file
	lock, err := m.lockFile(id)
	if err != nil {
		return fmt.Errorf("failed to lock issue: %w", err)
	}

	// Remove the lock file (which is the actual issue file now)
	if err := os.Remove(lock.lockPath); err != nil {
		_ = m.unlockFile(lock) // Try to restore file
		return fmt.Errorf("failed to delete issue: %w", err)
	}

	// Remove from locks map
	m.locksMu.Lock()
	delete(m.locks, id)
	m.locksMu.Unlock()

	return nil
}

// DeleteIssues deletes multiple issues
func (m *MarkdownStorage) DeleteIssues(ctx context.Context, ids []string, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// ListIssues lists all issues matching the filter
func (m *MarkdownStorage) ListIssues(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error) {
	// Read all markdown files in the issues directory
	entries, err := os.ReadDir(m.issuesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read issues directory: %w", err)
	}

	var issues []*types.Issue
	for _, entry := range entries {
		// Skip non-files and non-markdown files
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}

		name := entry.Name()

		// Skip lock, temp, and trash files
		if contains(name, ".lock.") || contains(name, ".tmp.") || contains(name, ".trash.") {
			continue
		}

		// Only process .md files
		if !hasSuffix(name, ".md") {
			continue
		}

		// Extract issue ID from filename
		issueID := name[:len(name)-3] // Remove .md extension

		// Read and parse the issue
		issue, err := m.GetIssue(ctx, issueID)
		if err != nil {
			// Skip issues that can't be read
			continue
		}

		// Apply filter
		if matchesFilter(issue, filter) {
			issues = append(issues, issue)
		}
	}

	return issues, nil
}

// SearchIssues searches issues by query string
func (m *MarkdownStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// CreateDependency creates a dependency between two issues
func (m *MarkdownStorage) CreateDependency(ctx context.Context, from, to, depType string) error {
	// Lock the "from" issue
	lock, err := m.lockFile(from)
	if err != nil {
		return fmt.Errorf("failed to lock issue: %w", err)
	}
	defer func() {
		if lock != nil {
			_ = m.unlockFile(lock)
		}
	}()

	// Read current issue
	data, err := os.ReadFile(lock.lockPath)
	if err != nil {
		return fmt.Errorf("failed to read issue: %w", err)
	}

	issue, err := markdownToIssue(from, data)
	if err != nil {
		return fmt.Errorf("failed to parse issue: %w", err)
	}

	// Check if dependency already exists
	for _, dep := range issue.Dependencies {
		if dep.DependsOnID == to {
			// Dependency already exists, update type if different
			if string(dep.Type) != depType {
				dep.Type = types.DependencyType(depType)
			} else {
				// Already exists with same type, nothing to do
				return nil
			}
			break
		}
	}

	// Add new dependency
	issue.Dependencies = append(issue.Dependencies, &types.Dependency{
		IssueID:     from,
		DependsOnID: to,
		Type:        types.DependencyType(depType),
	})

	// Update timestamp
	issue.UpdatedAt = time.Now()

	// Convert to markdown
	updatedData, err := issueToMarkdown(issue)
	if err != nil {
		return fmt.Errorf("failed to convert to markdown: %w", err)
	}

	// Write to temp file
	tempPath := m.getTempPath(from)
	if err := os.WriteFile(tempPath, updatedData, 0640); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Commit changes
	if err := m.commitFile(lock, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	lock = nil
	return nil
}

// DeleteDependency deletes a dependency
func (m *MarkdownStorage) DeleteDependency(ctx context.Context, from, to string) error {
	// Lock the "from" issue
	lock, err := m.lockFile(from)
	if err != nil {
		return fmt.Errorf("failed to lock issue: %w", err)
	}
	defer func() {
		if lock != nil {
			_ = m.unlockFile(lock)
		}
	}()

	// Read current issue
	data, err := os.ReadFile(lock.lockPath)
	if err != nil {
		return fmt.Errorf("failed to read issue: %w", err)
	}

	issue, err := markdownToIssue(from, data)
	if err != nil {
		return fmt.Errorf("failed to parse issue: %w", err)
	}

	// Find and remove the dependency
	found := false
	newDeps := make([]*types.Dependency, 0, len(issue.Dependencies))
	for _, dep := range issue.Dependencies {
		if dep.DependsOnID == to {
			found = true
			continue
		}
		newDeps = append(newDeps, dep)
	}

	if !found {
		// Dependency doesn't exist, nothing to do
		return nil
	}

	issue.Dependencies = newDeps

	// Update timestamp
	issue.UpdatedAt = time.Now()

	// Convert to markdown
	updatedData, err := issueToMarkdown(issue)
	if err != nil {
		return fmt.Errorf("failed to convert to markdown: %w", err)
	}

	// Write to temp file
	tempPath := m.getTempPath(from)
	if err := os.WriteFile(tempPath, updatedData, 0640); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Commit changes
	if err := m.commitFile(lock, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	lock = nil
	return nil
}

// GetDependencies returns all dependencies of an issue (as Issue objects)
func (m *MarkdownStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	// Get the issue
	issue, err := m.GetIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	// Get all the dependent issues
	var dependencies []*types.Issue
	for _, dep := range issue.Dependencies {
		depIssue, err := m.GetIssue(ctx, dep.DependsOnID)
		if err != nil {
			// Skip dependencies that can't be found
			continue
		}
		dependencies = append(dependencies, depIssue)
	}

	return dependencies, nil
}

// GetDependents returns all issues that depend on this issue (as Issue objects)
func (m *MarkdownStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	// Scan all issues to find ones that depend on this issue
	entries, err := os.ReadDir(m.issuesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read issues directory: %w", err)
	}

	var dependents []*types.Issue
	for _, entry := range entries {
		// Skip non-markdown files
		if entry.IsDir() || !hasSuffix(entry.Name(), ".md") {
			continue
		}

		// Skip lock/temp/trash files
		if contains(entry.Name(), ".lock.") || contains(entry.Name(), ".tmp.") || contains(entry.Name(), ".trash.") {
			continue
		}

		// Get issue ID from filename
		otherID := entry.Name()[:len(entry.Name())-3]

		// Get the issue
		otherIssue, err := m.GetIssue(ctx, otherID)
		if err != nil {
			continue
		}

		// Check if this issue depends on the target issue
		for _, dep := range otherIssue.Dependencies {
			if dep.DependsOnID == issueID {
				dependents = append(dependents, otherIssue)
				break
			}
		}
	}

	return dependents, nil
}

// RenameDependencyPrefix updates dependencies when renaming prefix
func (m *MarkdownStorage) RenameDependencyPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	return fmt.Errorf("not yet implemented")
}

// Comment operations - not yet supported
func (m *MarkdownStorage) CreateComment(ctx context.Context, comment *types.Comment) error {
	return fmt.Errorf("comments not yet supported in markdown backend")
}

func (m *MarkdownStorage) AddComment(ctx context.Context, issueID, author, text string) error {
	return fmt.Errorf("comments not yet supported in markdown backend")
}

func (m *MarkdownStorage) GetComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	return nil, fmt.Errorf("comments not yet supported in markdown backend")
}

func (m *MarkdownStorage) UpdateComment(ctx context.Context, id string, updates map[string]interface{}) error {
	return fmt.Errorf("comments not yet supported in markdown backend")
}

func (m *MarkdownStorage) DeleteComment(ctx context.Context, id string) error {
	return fmt.Errorf("comments not yet supported in markdown backend")
}

// RecordEvent records an event
func (m *MarkdownStorage) RecordEvent(ctx context.Context, event *types.Event) error {
	return fmt.Errorf("not yet implemented")
}

// GetEvents retrieves events for an issue
func (m *MarkdownStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// Config/Metadata operations
func (m *MarkdownStorage) GetConfig(ctx context.Context, key string) (string, error) {
	return "", fmt.Errorf("not yet implemented")
}

func (m *MarkdownStorage) SetConfig(ctx context.Context, key, value string) error {
	return fmt.Errorf("not yet implemented")
}

func (m *MarkdownStorage) GetMetadata(ctx context.Context, key string) (string, error) {
	return "", fmt.Errorf("not yet implemented")
}

func (m *MarkdownStorage) SetMetadata(ctx context.Context, key, value string) error {
	return fmt.Errorf("not yet implemented")
}

// Counter operations
func (m *MarkdownStorage) IncrementCounter(ctx context.Context, prefix string) (int, error) {
	return 0, fmt.Errorf("not yet implemented")
}

func (m *MarkdownStorage) GetCounter(ctx context.Context, prefix string) (int, error) {
	return 0, fmt.Errorf("not yet implemented")
}

func (m *MarkdownStorage) RenameCounterPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	return fmt.Errorf("not yet implemented")
}

func (m *MarkdownStorage) SyncAllCounters(ctx context.Context) error {
	return fmt.Errorf("not yet implemented")
}

// GetLabels returns labels for an issue
func (m *MarkdownStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return nil, fmt.Errorf("not yet implemented")
}
