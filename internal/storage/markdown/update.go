package markdown

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// applyUpdates applies a map of updates to an Issue
func applyUpdates(issue *types.Issue, updates map[string]interface{}) error {
	for key, value := range updates {
		switch key {
		case "title":
			if v, ok := value.(string); ok {
				issue.Title = v
			} else {
				return fmt.Errorf("invalid type for title: expected string")
			}

		case "description":
			if v, ok := value.(string); ok {
				issue.Description = v
			} else {
				return fmt.Errorf("invalid type for description: expected string")
			}

		case "design":
			if v, ok := value.(string); ok {
				issue.Design = v
			} else {
				return fmt.Errorf("invalid type for design: expected string")
			}

		case "notes":
			if v, ok := value.(string); ok {
				issue.Notes = v
			} else {
				return fmt.Errorf("invalid type for notes: expected string")
			}

		case "acceptance_criteria":
			if v, ok := value.(string); ok {
				issue.AcceptanceCriteria = v
			} else {
				return fmt.Errorf("invalid type for acceptance_criteria: expected string")
			}

		case "status":
			// Handle both string and types.Status
			switch v := value.(type) {
			case string:
				issue.Status = types.Status(v)
			case types.Status:
				issue.Status = v
			default:
				return fmt.Errorf("invalid type for status: expected string or types.Status")
			}

		case "priority":
			// Handle both int and float64 (from JSON)
			switch v := value.(type) {
			case int:
				issue.Priority = v
			case int64:
				issue.Priority = int(v)
			case float64:
				issue.Priority = int(v)
			default:
				return fmt.Errorf("invalid type for priority: expected int")
			}

		case "issue_type":
			// Handle both string and types.IssueType
			switch v := value.(type) {
			case string:
				issue.IssueType = types.IssueType(v)
			case types.IssueType:
				issue.IssueType = v
			default:
				return fmt.Errorf("invalid type for issue_type: expected string or types.IssueType")
			}

		case "assignee":
			// Handle both string and nil
			if value == nil {
				issue.Assignee = ""
			} else if v, ok := value.(string); ok {
				issue.Assignee = v
			} else {
				return fmt.Errorf("invalid type for assignee: expected string or nil")
			}

		case "external_ref":
			if v, ok := value.(string); ok {
				issue.ExternalRef = &v
			} else if value == nil {
				issue.ExternalRef = nil
			} else {
				return fmt.Errorf("invalid type for external_ref: expected string or nil")
			}

		case "labels":
			if v, ok := value.([]string); ok {
				issue.Labels = v
			} else if v, ok := value.([]interface{}); ok {
				// Handle []interface{} from JSON
				labels := make([]string, len(v))
				for i, label := range v {
					if s, ok := label.(string); ok {
						labels[i] = s
					} else {
						return fmt.Errorf("invalid type for label at index %d: expected string", i)
					}
				}
				issue.Labels = labels
			} else {
				return fmt.Errorf("invalid type for labels: expected []string")
			}

		case "closed_at":
			// Handle time.Time, *time.Time, and nil
			if value == nil {
				issue.ClosedAt = nil
			} else if v, ok := value.(time.Time); ok {
				issue.ClosedAt = &v
			} else if v, ok := value.(*time.Time); ok {
				issue.ClosedAt = v
			} else {
				return fmt.Errorf("invalid type for closed_at: expected time.Time or nil")
			}

		case "updated_at":
			// Handle time.Time (UpdatedAt is not a pointer)
			if v, ok := value.(time.Time); ok {
				issue.UpdatedAt = v
			} else {
				return fmt.Errorf("invalid type for updated_at: expected time.Time")
			}

		default:
			return fmt.Errorf("unknown field for update: %s", key)
		}
	}

	return nil
}

// Helper functions for string operations
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func hasSuffix(s, suffix string) bool {
	return strings.HasSuffix(s, suffix)
}

// matchesFilter checks if an issue matches the filter criteria
func matchesFilter(issue *types.Issue, filter types.IssueFilter) bool {
	// Check status
	if filter.Status != nil && issue.Status != *filter.Status {
		return false
	}

	// Check issue type
	if filter.IssueType != nil && issue.IssueType != *filter.IssueType {
		return false
	}

	// Check priority
	if filter.Priority != nil && issue.Priority != *filter.Priority {
		return false
	}

	// Check assignee
	if filter.Assignee != nil && issue.Assignee != *filter.Assignee {
		return false
	}

	// Check labels (AND semantics)
	if len(filter.Labels) > 0 {
		// Issue must have all specified labels
		for _, filterLabel := range filter.Labels {
			found := false
			for _, issueLabel := range issue.Labels {
				if issueLabel == filterLabel {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	// Check labels (OR semantics)
	if len(filter.LabelsAny) > 0 {
		// Issue must have at least one of these labels
		found := false
		for _, filterLabel := range filter.LabelsAny {
			for _, issueLabel := range issue.Labels {
				if issueLabel == filterLabel {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check IDs
	if len(filter.IDs) > 0 {
		found := false
		for _, id := range filter.IDs {
			if issue.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check title search
	if filter.TitleSearch != "" {
		if !contains(strings.ToLower(issue.Title), strings.ToLower(filter.TitleSearch)) {
			return false
		}
	}

	return true
}
