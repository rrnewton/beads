package markdown

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// CloseIssue closes an issue
func (m *MarkdownStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// AddDependency adds a dependency between issues
func (m *MarkdownStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// RemoveDependency removes a dependency
func (m *MarkdownStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// GetDependencyRecords returns raw dependency records
func (m *MarkdownStorage) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	// Get the issue
	issue, err := m.GetIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue: %w", err)
	}

	return issue.Dependencies, nil
}

// GetAllDependencyRecords returns all dependency records
func (m *MarkdownStorage) GetAllDependencyRecords(ctx context.Context) (map[string][]*types.Dependency, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetDependencyTree returns dependency tree
func (m *MarkdownStorage) GetDependencyTree(ctx context.Context, issueID string, maxDepth int, showAllPaths bool) ([]*types.TreeNode, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// DetectCycles detects dependency cycles
func (m *MarkdownStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// AddLabel adds a label to an issue
func (m *MarkdownStorage) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// RemoveLabel removes a label from an issue
func (m *MarkdownStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return fmt.Errorf("not yet implemented")
}

// GetIssuesByLabel returns issues with a specific label
func (m *MarkdownStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetReadyWork returns issues ready to work on
func (m *MarkdownStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetBlockedIssues returns blocked issues
func (m *MarkdownStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetEpicsEligibleForClosure returns epics that can be closed
func (m *MarkdownStorage) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// AddIssueComment adds a comment (returns Comment)
func (m *MarkdownStorage) AddIssueComment(ctx context.Context, issueID, author, text string) (*types.Comment, error) {
	return nil, fmt.Errorf("comments not yet supported in markdown backend")
}

// GetIssueComments returns comments for an issue
func (m *MarkdownStorage) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	return nil, fmt.Errorf("comments not yet supported in markdown backend")
}

// GetStatistics returns repository statistics
func (m *MarkdownStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// GetDirtyIssues returns issues that need syncing
func (m *MarkdownStorage) GetDirtyIssues(ctx context.Context) ([]string, error) {
	// Not applicable for markdown backend - all issues are always synced
	return nil, nil
}

// ClearDirtyIssues clears all dirty flags
func (m *MarkdownStorage) ClearDirtyIssues(ctx context.Context) error {
	// Not applicable for markdown backend
	return nil
}

// ClearDirtyIssuesByID clears dirty flags for specific issues
func (m *MarkdownStorage) ClearDirtyIssuesByID(ctx context.Context, issueIDs []string) error {
	// Not applicable for markdown backend
	return nil
}

// GetAllConfig returns all config values
func (m *MarkdownStorage) GetAllConfig(ctx context.Context) (map[string]string, error) {
	return nil, fmt.Errorf("not yet implemented")
}

// DeleteConfig deletes a config value
func (m *MarkdownStorage) DeleteConfig(ctx context.Context, key string) error {
	return fmt.Errorf("not yet implemented")
}

// UnderlyingConn returns the underlying SQL connection (not applicable)
func (m *MarkdownStorage) UnderlyingConn(ctx context.Context) (*sql.Conn, error) {
	return nil, fmt.Errorf("markdown backend has no SQL connection")
}
