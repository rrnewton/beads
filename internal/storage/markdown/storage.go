package markdown

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"gopkg.in/yaml.v3"
)

// MarkdownStorage implements storage.Storage using markdown files
type MarkdownStorage struct {
	rootDir   string           // .beads/markdown_db
	issuesDir string           // .beads/markdown_db/issues
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
	// Validate issue before creating
	if err := issue.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Set timestamps
	now := time.Now()
	issue.CreatedAt = now
	issue.UpdatedAt = now

	// Generate ID if not set
	if issue.ID == "" {
		// Get prefix from global config (.beads/config.yaml)
		prefix := config.GetString("issue-prefix")
		if prefix == "" {
			// Config not set - derive prefix from path as fallback
			prefix = derivePrefixFromMarkdownPath(m.rootDir)
		}

		// Get next ID using counter
		nextID, err := m.IncrementCounter(ctx, prefix)
		if err != nil {
			return fmt.Errorf("failed to generate issue ID: %w", err)
		}

		issue.ID = fmt.Sprintf("%s-%d", prefix, nextID)
	}

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

// derivePrefixFromMarkdownPath derives a prefix from the markdown storage path
func derivePrefixFromMarkdownPath(rootPath string) string {
	// Try to get from parent directory name
	// .beads/markdown_db -> look at .beads parent
	beadsDir := filepath.Dir(rootPath)
	projectDir := filepath.Dir(beadsDir)
	projectName := filepath.Base(projectDir)

	// Clean up the project name to make a valid prefix
	prefix := strings.ToLower(projectName)
	prefix = strings.ReplaceAll(prefix, " ", "-")
	prefix = strings.ReplaceAll(prefix, "_", "-")

	// Fallback if project name is weird
	if prefix == "" || prefix == "." || prefix == ".." {
		prefix = "bd"
	}

	return prefix
}

// CreateIssues creates multiple issues atomically
func (m *MarkdownStorage) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	// For markdown backend, we don't have true atomicity across multiple files
	// But we can create them one by one
	for _, issue := range issues {
		if err := m.CreateIssue(ctx, issue, actor); err != nil {
			return fmt.Errorf("failed to create issue %s: %w", issue.ID, err)
		}
	}
	return nil
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
					// Issue doesn't exist - return nil to match SQLite behavior
					return nil, nil
				}
			} else {
				// Issue doesn't exist - return nil to match SQLite behavior
				return nil, nil
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
	// Lock the old issue file
	lock, err := m.lockFile(oldID)
	if err != nil {
		return fmt.Errorf("failed to lock issue: %w", err)
	}
	defer func() {
		if lock != nil {
			_ = m.unlockFile(lock)
		}
	}()

	// Update timestamp
	issue.UpdatedAt = time.Now()

	// Convert updated issue to markdown
	updatedData, err := issueToMarkdown(issue)
	if err != nil {
		return fmt.Errorf("failed to convert to markdown: %w", err)
	}

	// Write to new file location
	newIssuePath := m.getIssuePath(newID)
	tempPath := m.getTempPath(newID)

	if err := os.WriteFile(tempPath, updatedData, 0640); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomically rename temp to new location
	if err := os.Rename(tempPath, newIssuePath); err != nil {
		_ = os.Remove(tempPath) // Cleanup temp file
		return fmt.Errorf("failed to create new issue file: %w", err)
	}

	// Remove old lock file (which contains the old issue)
	if err := os.Remove(lock.lockPath); err != nil {
		// Try to cleanup new file
		_ = os.Remove(newIssuePath)
		return fmt.Errorf("failed to delete old issue file: %w", err)
	}

	// Remove from locks map
	m.locksMu.Lock()
	delete(m.locks, oldID)
	m.locksMu.Unlock()

	// Update dependencies that reference this issue
	// Scan all issues to find and update dependencies
	entries, err := os.ReadDir(m.issuesDir)
	if err != nil {
		return fmt.Errorf("failed to read issues directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !hasSuffix(entry.Name(), ".md") {
			continue
		}

		// Skip lock/temp/trash files
		if contains(entry.Name(), ".lock.") || contains(entry.Name(), ".tmp.") || contains(entry.Name(), ".trash.") {
			continue
		}

		// Get issue ID from filename
		otherID := entry.Name()[:len(entry.Name())-3]

		// Skip the issue we just renamed
		if otherID == newID {
			continue
		}

		// Read the issue
		otherIssue, err := m.GetIssue(ctx, otherID)
		if err != nil {
			continue
		}

		// Check if any dependencies reference the old ID
		needsUpdate := false
		for _, dep := range otherIssue.Dependencies {
			if dep.IssueID == oldID {
				dep.IssueID = newID
				needsUpdate = true
			}
			if dep.DependsOnID == oldID {
				dep.DependsOnID = newID
				needsUpdate = true
			}
		}

		// Update the issue file directly if needed
		if needsUpdate {
			// Lock the issue
			otherLock, err := m.lockFile(otherID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to lock %s: %v\n", otherID, err)
				continue
			}

			// Update timestamp
			otherIssue.UpdatedAt = time.Now()

			// Convert to markdown with updated dependencies
			updatedData, err := issueToMarkdown(otherIssue)
			if err != nil {
				_ = m.unlockFile(otherLock)
				fmt.Fprintf(os.Stderr, "Warning: failed to convert %s to markdown: %v\n", otherID, err)
				continue
			}

			// Write to temp file
			tempPath := m.getTempPath(otherID)
			if err := os.WriteFile(tempPath, updatedData, 0640); err != nil {
				_ = m.unlockFile(otherLock)
				fmt.Fprintf(os.Stderr, "Warning: failed to write temp file for %s: %v\n", otherID, err)
				continue
			}

			// Commit changes
			if err := m.commitFile(otherLock, tempPath); err != nil {
				_ = os.Remove(tempPath)
				fmt.Fprintf(os.Stderr, "Warning: failed to commit changes for %s: %v\n", otherID, err)
				continue
			}
		}
	}

	lock = nil // Prevent defer from trying to unlock
	return nil
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
	// Delete each issue individually
	// Note: This is not atomic across all issues, but markdown backend doesn't support transactions
	var errors []string
	for _, id := range ids {
		if err := m.DeleteIssue(ctx, id, actor); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", id, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to delete some issues: %s", strings.Join(errors, "; "))
	}

	return nil
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
	// For markdown backend, we just use ListIssues with filters
	// The query parameter can be used for full-text search in the future
	// For now, we support title search via filter.TitleSearch
	if query != "" && filter.TitleSearch == "" {
		filter.TitleSearch = query
	}

	return m.ListIssues(ctx, filter)
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
// For markdown backend, dependency updates are handled in UpdateIssueID
func (m *MarkdownStorage) RenameDependencyPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	// No-op: dependencies are already updated in UpdateIssueID
	return nil
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
	// Create events file path for this issue
	eventsPath := filepath.Join(m.rootDir, "events", event.IssueID+".jsonl")

	// Set timestamp if not set
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Append to events file (create if doesn't exist)
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return fmt.Errorf("failed to open events file: %w", err)
	}
	defer f.Close()

	// Write event as JSONL (one line per event)
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	return nil
}

// GetEvents retrieves events for an issue
func (m *MarkdownStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	eventsPath := filepath.Join(m.rootDir, "events", issueID+".jsonl")

	// Check if events file exists
	if _, err := os.Stat(eventsPath); os.IsNotExist(err) {
		// No events for this issue yet
		return []*types.Event{}, nil
	}

	// Read events file
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read events file: %w", err)
	}

	// Parse JSONL
	lines := strings.Split(string(data), "\n")
	events := make([]*types.Event, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event types.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Skip malformed lines
			continue
		}

		events = append(events, &event)
	}

	// Apply limit (return last N events)
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}

	return events, nil
}

// Config/Metadata operations
func (m *MarkdownStorage) GetConfig(ctx context.Context, key string) (string, error) {
	configPath := filepath.Join(m.rootDir, "config.yaml")
	return m.getYAMLValue(configPath, key)
}

func (m *MarkdownStorage) SetConfig(ctx context.Context, key, value string) error {
	configPath := filepath.Join(m.rootDir, "config.yaml")
	return m.setYAMLValue(configPath, key, value)
}

func (m *MarkdownStorage) GetMetadata(ctx context.Context, key string) (string, error) {
	metadataPath := filepath.Join(m.rootDir, "metadata.yaml")
	return m.getYAMLValue(metadataPath, key)
}

func (m *MarkdownStorage) SetMetadata(ctx context.Context, key, value string) error {
	metadataPath := filepath.Join(m.rootDir, "metadata.yaml")
	return m.setYAMLValue(metadataPath, key, value)
}

// Counter operations
func (m *MarkdownStorage) IncrementCounter(ctx context.Context, prefix string) (int, error) {
	// Lock to prevent concurrent ID generation
	m.locksMu.Lock()
	defer m.locksMu.Unlock()

	// Scan all markdown files to find the maximum ID for this prefix
	maxID, err := m.getMaxIDForPrefix(prefix)
	if err != nil {
		return 0, fmt.Errorf("failed to scan files for max ID: %w", err)
	}

	// Return next ID
	return maxID + 1, nil
}

// getMaxIDForPrefix scans all issue files and returns the maximum ID number for a given prefix
func (m *MarkdownStorage) getMaxIDForPrefix(prefix string) (int, error) {
	entries, err := os.ReadDir(m.issuesDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read issues directory: %w", err)
	}

	maxID := 0
	for _, entry := range entries {
		if entry.IsDir() || !hasSuffix(entry.Name(), ".md") {
			continue
		}

		// Skip lock/temp/trash files
		if contains(entry.Name(), ".lock.") || contains(entry.Name(), ".tmp.") || contains(entry.Name(), ".trash.") {
			continue
		}

		// Extract issue ID from filename (remove .md extension)
		issueID := entry.Name()[:len(entry.Name())-3]

		// Parse prefix and number
		parts := strings.Split(issueID, "-")
		if len(parts) >= 2 {
			filePrefix := strings.Join(parts[:len(parts)-1], "-")
			if filePrefix == prefix {
				if num, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
					if num > maxID {
						maxID = num
					}
				}
			}
		}
	}

	return maxID, nil
}

func (m *MarkdownStorage) GetCounter(ctx context.Context, prefix string) (int, error) {
	// Scan files to get the current max ID for this prefix
	return m.getMaxIDForPrefix(prefix)
}

func (m *MarkdownStorage) RenameCounterPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	// For markdown backend, counters are derived from filenames
	// No separate counter state to update
	return nil
}

func (m *MarkdownStorage) SyncAllCounters(ctx context.Context) error {
	// For markdown backend, counters are always in sync with files
	// No separate counter state to synchronize
	return nil
}

// GetLabels returns labels for an issue
func (m *MarkdownStorage) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	issue, err := m.GetIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	return issue.Labels, nil
}

// getYAMLValue reads a value from a YAML file
func (m *MarkdownStorage) getYAMLValue(filePath, key string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("key not found: %s", key)
		}
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("failed to parse YAML: %w", err)
	}

	value, exists := values[key]
	if !exists {
		return "", fmt.Errorf("key not found: %s", key)
	}

	// Convert to string
	return fmt.Sprintf("%v", value), nil
}

// setYAMLValue writes a value to a YAML file
func (m *MarkdownStorage) setYAMLValue(filePath, key, value string) error {
	// Read existing values or create new map
	var values map[string]interface{}
	data, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	} else {
		values = make(map[string]interface{})
	}

	// Set the value
	values[key] = value

	// Write back to file
	newData, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	if err := os.WriteFile(filePath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
