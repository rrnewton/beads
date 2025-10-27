package markdown

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
	"gopkg.in/yaml.v3"
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
	if maxDepth <= 0 {
		maxDepth = 50
	}

	// Get all issues
	allIssues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	// Build map for quick lookup
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Build adjacency list: issue -> dependencies (issues this one depends on)
	adjacency := make(map[string][]string)
	for _, issue := range allIssues {
		for _, dep := range issue.Dependencies {
			adjacency[issue.ID] = append(adjacency[issue.ID], dep.DependsOnID)
		}
	}

	// BFS to build tree
	type queueItem struct {
		issueID  string
		depth    int
		parentID string
		path     map[string]bool // Track visited nodes in this path to detect cycles
	}

	queue := []queueItem{{
		issueID:  issueID,
		depth:    0,
		parentID: issueID,
		path:     map[string]bool{issueID: true},
	}}

	seen := make(map[string]int) // Track minimum depth for deduplication
	var nodes []*types.TreeNode

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		issue, exists := issueMap[current.issueID]
		if !exists {
			continue
		}

		// Deduplication: skip if we've seen this at a shallower depth (unless showAllPaths)
		if !showAllPaths {
			if prevDepth, seen := seen[current.issueID]; seen && prevDepth < current.depth {
				continue
			}
			seen[current.issueID] = current.depth
		}

		// Create tree node (TreeNode embeds Issue, so we need to copy it)
		node := &types.TreeNode{
			Issue:     *issue,
			Depth:     current.depth,
			Truncated: current.depth == maxDepth,
		}
		nodes = append(nodes, node)

		// Add children if not at max depth
		if current.depth < maxDepth {
			for _, depID := range adjacency[current.issueID] {
				// Skip if this would create a cycle
				if current.path[depID] {
					continue
				}

				// Create new path for child
				newPath := make(map[string]bool)
				for k, v := range current.path {
					newPath[k] = v
				}
				newPath[depID] = true

				queue = append(queue, queueItem{
					issueID:  depID,
					depth:    current.depth + 1,
					parentID: current.issueID,
					path:     newPath,
				})
			}
		}
	}

	return nodes, nil
}

// DetectCycles detects dependency cycles
func (m *MarkdownStorage) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	// Get all issues
	allIssues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	// Build map for quick lookup
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Build adjacency list
	adjacency := make(map[string][]string)
	for _, issue := range allIssues {
		for _, dep := range issue.Dependencies {
			adjacency[issue.ID] = append(adjacency[issue.ID], dep.DependsOnID)
		}
	}

	// DFS to find cycles
	var cycles [][]*types.Issue
	seenCycles := make(map[string]bool)

	var dfs func(issueID string, path []string, visited map[string]bool) bool
	dfs = func(issueID string, path []string, visited map[string]bool) bool {
		// Check if we've found a cycle
		for i, id := range path {
			if id == issueID {
				// Found a cycle! Extract the cycle portion
				cyclePath := path[i:]
				cycleKey := getCycleKey(cyclePath)

				if !seenCycles[cycleKey] {
					seenCycles[cycleKey] = true

					// Build the cycle issues list
					var cycleIssues []*types.Issue
					for _, cid := range cyclePath {
						if issue, exists := issueMap[cid]; exists {
							cycleIssues = append(cycleIssues, issue)
						}
					}

					if len(cycleIssues) > 0 {
						cycles = append(cycles, cycleIssues)
					}
				}
				return true
			}
		}

		// Already fully explored this node
		if visited[issueID] {
			return false
		}

		// Add to current path
		path = append(path, issueID)

		// Explore dependencies
		for _, depID := range adjacency[issueID] {
			dfs(depID, path, visited)
		}

		// Mark as fully visited after exploring all paths through it
		visited[issueID] = true
		return false
	}

	// Start DFS from each issue
	globalVisited := make(map[string]bool)
	for _, issue := range allIssues {
		if !globalVisited[issue.ID] {
			dfs(issue.ID, []string{}, globalVisited)
		}
	}

	return cycles, nil
}

// getCycleKey creates a normalized key for a cycle to detect duplicates
// Cycles like [A,B,C] and [B,C,A] and [C,A,B] are the same cycle
func getCycleKey(path []string) string {
	if len(path) == 0 {
		return ""
	}

	// Find the lexicographically smallest ID in the cycle
	minIdx := 0
	for i := 1; i < len(path); i++ {
		if path[i] < path[minIdx] {
			minIdx = i
		}
	}

	// Build normalized path starting from smallest ID
	normalized := make([]string, len(path))
	for i := 0; i < len(path); i++ {
		normalized[i] = path[(minIdx+i)%len(path)]
	}

	// Join to create unique key
	return strings.Join(normalized, "→")
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
	// Get all issues
	allIssues, err := m.ListIssues(ctx, types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	// Build map of all issues for quick lookup
	issueMap := make(map[string]*types.Issue)
	for _, issue := range allIssues {
		issueMap[issue.ID] = issue
	}

	// Build map of epic -> children counts
	epicChildren := make(map[string]struct {
		total  int
		closed int
	})

	// Find all parent-child dependencies
	for _, issue := range allIssues {
		for _, dep := range issue.Dependencies {
			if dep.Type == "parent-child" {
				epicID := dep.DependsOnID
				stats := epicChildren[epicID]
				stats.total++
				if issue.Status == types.StatusClosed {
					stats.closed++
				}
				epicChildren[epicID] = stats
			}
		}
	}

	// Find all open epics and check if eligible for closure
	var results []*types.EpicStatus
	for _, issue := range allIssues {
		if issue.IssueType != types.TypeEpic || issue.Status == types.StatusClosed {
			continue
		}

		stats := epicChildren[issue.ID]
		eligibleForClose := false
		if stats.total > 0 && stats.closed == stats.total {
			eligibleForClose = true
		}

		results = append(results, &types.EpicStatus{
			Epic:             issue,
			TotalChildren:    stats.total,
			ClosedChildren:   stats.closed,
			EligibleForClose: eligibleForClose,
		})
	}

	// Sort by priority, then created_at
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Epic.Priority > results[j].Epic.Priority ||
				(results[i].Epic.Priority == results[j].Epic.Priority &&
					results[i].Epic.CreatedAt.After(results[j].Epic.CreatedAt)) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results, nil
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
	configPath := filepath.Join(m.rootDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file yet, return empty map
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Convert all values to strings
	result := make(map[string]string)
	for key, value := range values {
		result[key] = fmt.Sprintf("%v", value)
	}

	return result, nil
}

// DeleteConfig deletes a config value
func (m *MarkdownStorage) DeleteConfig(ctx context.Context, key string) error {
	configPath := filepath.Join(m.rootDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("failed to parse config YAML: %w", err)
	}

	// Delete the key
	delete(values, key)

	// Write back to file
	newData, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to marshal config YAML: %w", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// UnderlyingConn returns the underlying SQL connection (not applicable)
func (m *MarkdownStorage) UnderlyingConn(ctx context.Context) (*sql.Conn, error) {
	return nil, fmt.Errorf("markdown backend has no SQL connection")
}
