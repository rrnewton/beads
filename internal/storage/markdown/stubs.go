package markdown

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// CloseIssue closes an issue
func (m *MarkdownStorage) CloseIssue(ctx context.Context, id string, reason string, actor string) error {
	now := time.Now()

	// Use UpdateIssue to set status and closed_at
	updates := map[string]interface{}{
		"status":     types.StatusClosed,
		"closed_at":  now,
		"updated_at": now,
	}

	return m.UpdateIssue(ctx, id, updates, actor)
}

// AddDependency adds a dependency between issues
func (m *MarkdownStorage) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	// AddDependency is a wrapper around CreateDependency
	// dep.IssueID depends on dep.DependsOnID with type dep.Type
	return m.CreateDependency(ctx, dep.IssueID, dep.DependsOnID, string(dep.Type))
}

// RemoveDependency removes a dependency
func (m *MarkdownStorage) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	// RemoveDependency is a wrapper around DeleteDependency
	return m.DeleteDependency(ctx, issueID, dependsOnID)
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

// GetAllDependencyRecords returns all dependency records grouped by issue ID
func (m *MarkdownStorage) GetAllDependencyRecords(ctx context.Context) (map[string][]*types.Dependency, error) {
	// Get all issues
	issues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}

	// Build map of dependencies by issue ID
	depsMap := make(map[string][]*types.Dependency)
	for _, issue := range issues {
		if len(issue.Dependencies) > 0 {
			depsMap[issue.ID] = issue.Dependencies
		}
	}

	return depsMap, nil
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
	// Get the current issue
	issue, err := m.GetIssue(ctx, issueID)
	if err != nil {
		return err
	}

	// Check if label already exists
	for _, existingLabel := range issue.Labels {
		if existingLabel == label {
			return nil // Already exists, no-op
		}
	}

	// Add the label
	newLabels := append(issue.Labels, label)
	updates := map[string]interface{}{
		"labels": newLabels,
	}

	return m.UpdateIssue(ctx, issueID, updates, actor)
}

// RemoveLabel removes a label from an issue
func (m *MarkdownStorage) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	// Get the current issue
	issue, err := m.GetIssue(ctx, issueID)
	if err != nil {
		return err
	}

	// Filter out the label
	newLabels := make([]string, 0, len(issue.Labels))
	found := false
	for _, existingLabel := range issue.Labels {
		if existingLabel != label {
			newLabels = append(newLabels, existingLabel)
		} else {
			found = true
		}
	}

	if !found {
		return nil // Label didn't exist, no-op
	}

	updates := map[string]interface{}{
		"labels": newLabels,
	}

	return m.UpdateIssue(ctx, issueID, updates, actor)
}

// GetIssuesByLabel returns issues with a specific label
func (m *MarkdownStorage) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	// Get all issues
	allIssues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	// Filter issues that have the label
	var result []*types.Issue
	for _, issue := range allIssues {
		for _, issueLabel := range issue.Labels {
			if issueLabel == label {
				result = append(result, issue)
				break
			}
		}
	}

	return result, nil
}

// GetReadyWork returns issues ready to work on (no blocking dependencies)
func (m *MarkdownStorage) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	// Default to open OR in_progress if not specified
	statusFilter := types.IssueFilter{}
	if filter.Status == "" {
		// We'll filter manually for open OR in_progress
	} else {
		status := types.Status(filter.Status)
		statusFilter.Status = &status
	}

	if filter.Priority != nil {
		statusFilter.Priority = filter.Priority
	}

	if filter.Assignee != nil {
		statusFilter.Assignee = filter.Assignee
	}

	// Get all issues matching the basic filter
	allIssues, err := m.ListIssues(ctx, statusFilter)
	if err != nil {
		return nil, err
	}

	// Filter by status if default (open OR in_progress)
	var candidates []*types.Issue
	if filter.Status == "" {
		for _, issue := range allIssues {
			if issue.Status == types.StatusOpen || issue.Status == types.StatusInProgress {
				candidates = append(candidates, issue)
			}
		}
	} else {
		candidates = allIssues
	}

	// Build map of all issues for quick lookup
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Find blocked issues
	blockedSet := make(map[string]bool)

	// Direct blocking: issues with 'blocks' dependencies to open/in_progress/blocked issues
	for _, issue := range candidates {
		for _, dep := range issue.Dependencies {
			if dep.Type == "blocks" {
				blocker, exists := issueMap[dep.DependsOnID]
				if exists && (blocker.Status == types.StatusOpen ||
					blocker.Status == types.StatusInProgress ||
					blocker.Status == types.StatusBlocked) {
					blockedSet[issue.ID] = true
					break
				}
			}
		}
	}

	// Transitive blocking through parent-child relationships
	// If a parent is blocked, all children are blocked
	changed := true
	for changed {
		changed = false
		for _, issue := range candidates {
			if blockedSet[issue.ID] {
				continue // Already blocked
			}

			// Check if any parent is blocked
			for _, dep := range issue.Dependencies {
				if dep.Type == "parent-child" && blockedSet[dep.DependsOnID] {
					blockedSet[issue.ID] = true
					changed = true
					break
				}
			}
		}
	}

	// Filter out blocked issues
	var ready []*types.Issue
	for _, issue := range candidates {
		if !blockedSet[issue.ID] {
			ready = append(ready, issue)
		}
	}

	// Sort by priority (ascending), then created_at
	// TODO: Implement full sort policy support (hybrid, oldest)
	for i := 0; i < len(ready); i++ {
		for j := i + 1; j < len(ready); j++ {
			if ready[i].Priority > ready[j].Priority ||
				(ready[i].Priority == ready[j].Priority && ready[i].CreatedAt.After(ready[j].CreatedAt)) {
				ready[i], ready[j] = ready[j], ready[i]
			}
		}
	}

	// Apply limit
	if filter.Limit > 0 && len(ready) > filter.Limit {
		ready = ready[:filter.Limit]
	}

	return ready, nil
}

// GetBlockedIssues returns blocked issues
func (m *MarkdownStorage) GetBlockedIssues(ctx context.Context) ([]*types.BlockedIssue, error) {
	// Get all open/in_progress/blocked issues
	statusOpen := types.StatusOpen
	statusInProgress := types.StatusInProgress
	statusBlocked := types.StatusBlocked

	allIssues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	// Build map for quick lookup
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Find blocked issues
	var blocked []*types.BlockedIssue
	for _, issue := range allIssues {
		// Only consider open/in_progress/blocked issues
		if issue.Status != statusOpen && issue.Status != statusInProgress && issue.Status != statusBlocked {
			continue
		}

		// Check for blocking dependencies
		var blockers []string
		for _, dep := range issue.Dependencies {
			if dep.Type == "blocks" {
				blocker, exists := issueMap[dep.DependsOnID]
				if exists && (blocker.Status == statusOpen ||
					blocker.Status == statusInProgress ||
					blocker.Status == statusBlocked) {
					blockers = append(blockers, dep.DependsOnID)
				}
			}
		}

		if len(blockers) > 0 {
			blockedIssue := &types.BlockedIssue{
				Issue: types.Issue{
					ID:                 issue.ID,
					Title:              issue.Title,
					Description:        issue.Description,
					Design:             issue.Design,
					AcceptanceCriteria: issue.AcceptanceCriteria,
					Notes:              issue.Notes,
					Status:             issue.Status,
					Priority:           issue.Priority,
					IssueType:          issue.IssueType,
					Assignee:           issue.Assignee,
					EstimatedMinutes:   issue.EstimatedMinutes,
					CreatedAt:          issue.CreatedAt,
					UpdatedAt:          issue.UpdatedAt,
					ClosedAt:           issue.ClosedAt,
					ExternalRef:        issue.ExternalRef,
					Labels:             issue.Labels,
					Dependencies:       issue.Dependencies,
				},
				BlockedBy:      blockers,
				BlockedByCount: len(blockers),
			}
			blocked = append(blocked, blockedIssue)
		}
	}

	// Sort by priority
	for i := 0; i < len(blocked); i++ {
		for j := i + 1; j < len(blocked); j++ {
			if blocked[i].Priority > blocked[j].Priority {
				blocked[i], blocked[j] = blocked[j], blocked[i]
			}
		}
	}

	return blocked, nil
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
// Markdown backend doesn't support comments yet, so return empty list
func (m *MarkdownStorage) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	return []*types.Comment{}, nil
}

// GetStatistics returns repository statistics
func (m *MarkdownStorage) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	allIssues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	stats := &types.Statistics{}
	stats.TotalIssues = len(allIssues)

	// Build map for dependency lookups
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Count by status and calculate lead time
	var totalLeadTime float64
	var closedCount int

	for _, issue := range allIssues {
		switch issue.Status {
		case types.StatusOpen:
			stats.OpenIssues++
		case types.StatusInProgress:
			stats.InProgressIssues++
		case types.StatusClosed:
			stats.ClosedIssues++
		}

		// Calculate lead time for closed issues
		if issue.Status == types.StatusClosed && issue.ClosedAt != nil {
			leadTime := issue.ClosedAt.Sub(issue.CreatedAt).Hours()
			totalLeadTime += leadTime
			closedCount++
		}
	}

	// Average lead time
	if closedCount > 0 {
		stats.AverageLeadTime = totalLeadTime / float64(closedCount)
	}

	// Count blocked issues
	blockedSet := make(map[string]bool)
	for _, issue := range allIssues {
		if issue.Status != types.StatusOpen &&
			issue.Status != types.StatusInProgress &&
			issue.Status != types.StatusBlocked {
			continue
		}

		for _, dep := range issue.Dependencies {
			if dep.Type == "blocks" {
				blocker, exists := issueMap[dep.DependsOnID]
				if exists && (blocker.Status == types.StatusOpen ||
					blocker.Status == types.StatusInProgress ||
					blocker.Status == types.StatusBlocked) {
					blockedSet[issue.ID] = true
					break
				}
			}
		}
	}
	stats.BlockedIssues = len(blockedSet)

	// Count ready issues (open with no blockers)
	for _, issue := range allIssues {
		if issue.Status == types.StatusOpen && !blockedSet[issue.ID] {
			stats.ReadyIssues++
		}
	}

	return stats, nil
}

// GetDirtyIssues returns issues that need syncing
// For markdown backend, we don't track dirty flags separately since each update
// writes directly to disk. To support auto-flush to JSONL, we return all issue IDs.
func (m *MarkdownStorage) GetDirtyIssues(ctx context.Context) ([]string, error) {
	issues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}

	return issueIDs, nil
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
