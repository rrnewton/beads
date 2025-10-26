package markdown

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	return fmt.Errorf("not yet implemented")
}

// CreateIssues creates multiple issues atomically
func (m *MarkdownStorage) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// GetIssue retrieves an issue by ID
func (m *MarkdownStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// UpdateIssue updates an existing issue
func (m *MarkdownStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// UpdateIssueID renames an issue's ID and updates all references
func (m *MarkdownStorage) UpdateIssueID(ctx context.Context, oldID, newID string, issue *types.Issue, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// DeleteIssue deletes an issue
func (m *MarkdownStorage) DeleteIssue(ctx context.Context, id string, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// DeleteIssues deletes multiple issues
func (m *MarkdownStorage) DeleteIssues(ctx context.Context, ids []string, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// ListIssues lists all issues matching the filter
func (m *MarkdownStorage) ListIssues(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// SearchIssues searches issues by query string
func (m *MarkdownStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// CreateDependency creates a dependency between two issues
func (m *MarkdownStorage) CreateDependency(ctx context.Context, from, to, depType string) error {
	return fmt.Errorf("not yet implemented")
}

// DeleteDependency deletes a dependency
func (m *MarkdownStorage) DeleteDependency(ctx context.Context, from, to string) error {
	return fmt.Errorf("not yet implemented")
}

// GetDependencies returns all dependencies of an issue (as Issue objects)
func (m *MarkdownStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetDependents returns all issues that depend on this issue (as Issue objects)
func (m *MarkdownStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
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
