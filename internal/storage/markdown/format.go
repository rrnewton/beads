package markdown

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
	"gopkg.in/yaml.v3"
)

// Frontmatter represents the YAML frontmatter of an issue file
type Frontmatter struct {
	Title        string            `yaml:"title"`
	Status       string            `yaml:"status"`
	Priority     int               `yaml:"priority"`
	IssueType    string            `yaml:"issue_type"`
	Assignee     string            `yaml:"assignee,omitempty"`
	ExternalRef  string            `yaml:"external_ref,omitempty"`
	Labels       []string          `yaml:"labels,omitempty"`
	DependsOn    map[string]string `yaml:"depends_on,omitempty"` // issueID -> depType
	CreatedAt    string            `yaml:"created_at"`
	UpdatedAt    string            `yaml:"updated_at"`
	ClosedAt     string            `yaml:"closed_at,omitempty"`
}

// Sections represents the markdown sections in the body
type Sections struct {
	Description        string
	Design             string
	Notes              string
	AcceptanceCriteria string
}

// issueToMarkdown converts an Issue to markdown format
func issueToMarkdown(issue *types.Issue) ([]byte, error) {
	var buf bytes.Buffer

	// Build frontmatter
	fm := Frontmatter{
		Title:      issue.Title,
		Status:     string(issue.Status),
		Priority:   issue.Priority,
		IssueType:  string(issue.IssueType),
		Assignee:   issue.Assignee,
		Labels:     issue.Labels,
		CreatedAt:  issue.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  issue.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if issue.ExternalRef != nil {
		fm.ExternalRef = *issue.ExternalRef
	}

	if issue.ClosedAt != nil && !issue.ClosedAt.IsZero() {
		fm.ClosedAt = issue.ClosedAt.Format("2006-01-02T15:04:05Z07:00")
	}

	// Convert dependencies to map
	if len(issue.Dependencies) > 0 {
		fm.DependsOn = make(map[string]string)
		for _, dep := range issue.Dependencies {
			fm.DependsOn[dep.DependsOnID] = string(dep.Type)
		}
	}

	// Write YAML frontmatter
	buf.WriteString("---\n")
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&fm); err != nil {
		return nil, fmt.Errorf("failed to encode frontmatter: %w", err)
	}
	encoder.Close()
	buf.WriteString("---\n")

	// Write markdown sections
	if issue.Description != "" {
		buf.WriteString("\n# Description\n\n")
		buf.WriteString(issue.Description)
		buf.WriteString("\n")
	}

	if issue.Design != "" {
		buf.WriteString("\n# Design\n\n")
		buf.WriteString(issue.Design)
		buf.WriteString("\n")
	}

	if issue.AcceptanceCriteria != "" {
		buf.WriteString("\n# Acceptance Criteria\n\n")
		buf.WriteString(issue.AcceptanceCriteria)
		buf.WriteString("\n")
	}

	if issue.Notes != "" {
		buf.WriteString("\n# Notes\n\n")
		buf.WriteString(issue.Notes)
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// markdownToIssue parses markdown format into an Issue
func markdownToIssue(issueID string, data []byte) (*types.Issue, error) {
	// Split frontmatter and body
	parts := bytes.SplitN(data, []byte("---\n"), 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid markdown format: missing frontmatter")
	}

	// Parse frontmatter
	var fm Frontmatter
	if err := yaml.Unmarshal(parts[1], &fm); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Parse body sections
	sections := parseSections(string(parts[2]))

	// Build Issue
	issue := &types.Issue{
		ID:                 issueID,
		Title:              fm.Title,
		Description:        sections.Description,
		Design:             sections.Design,
		Notes:              sections.Notes,
		AcceptanceCriteria: sections.AcceptanceCriteria,
		Status:             types.Status(fm.Status),
		Priority:           fm.Priority,
		IssueType:          types.IssueType(fm.IssueType),
		Assignee:           fm.Assignee,
		Labels:             fm.Labels,
	}

	if fm.ExternalRef != "" {
		issue.ExternalRef = &fm.ExternalRef
	}

	// Parse timestamps
	if fm.CreatedAt != "" {
		createdAt, err := parseTimestamp(fm.CreatedAt)
		if err == nil {
			issue.CreatedAt = createdAt
		}
	}

	if fm.UpdatedAt != "" {
		updatedAt, err := parseTimestamp(fm.UpdatedAt)
		if err == nil {
			issue.UpdatedAt = updatedAt
		}
	}

	if fm.ClosedAt != "" {
		closedAt, err := parseTimestamp(fm.ClosedAt)
		if err == nil {
			issue.ClosedAt = &closedAt
		}
	}

	// Convert dependencies from map to slice
	if len(fm.DependsOn) > 0 {
		issue.Dependencies = make([]*types.Dependency, 0, len(fm.DependsOn))
		for dependsOnID, depType := range fm.DependsOn {
			issue.Dependencies = append(issue.Dependencies, &types.Dependency{
				IssueID:     issueID,
				DependsOnID: dependsOnID,
				Type:        types.DependencyType(depType),
			})
		}
	}

	return issue, nil
}

// parseSections extracts markdown sections from the body
func parseSections(body string) Sections {
	sections := Sections{}

	// Split by headers
	lines := strings.Split(body, "\n")
	var currentSection string
	var currentContent strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a header line
		if strings.HasPrefix(trimmed, "# ") {
			// Save previous section
			if currentSection != "" {
				content := strings.TrimSpace(currentContent.String())
				switch currentSection {
				case "Description":
					sections.Description = content
				case "Design":
					sections.Design = content
				case "Acceptance Criteria":
					sections.AcceptanceCriteria = content
				case "Notes":
					sections.Notes = content
				}
			}

			// Start new section
			currentSection = strings.TrimPrefix(trimmed, "# ")
			currentContent.Reset()
		} else if currentSection != "" {
			// Add line to current section
			if currentContent.Len() > 0 {
				currentContent.WriteString("\n")
			}
			currentContent.WriteString(line)
		}
	}

	// Save last section
	if currentSection != "" {
		content := strings.TrimSpace(currentContent.String())
		switch currentSection {
		case "Description":
			sections.Description = content
		case "Design":
			sections.Design = content
		case "Acceptance Criteria":
			sections.AcceptanceCriteria = content
		case "Notes":
			sections.Notes = content
		}
	}

	return sections
}

// parseTimestamp parses a timestamp string
func parseTimestamp(s string) (time.Time, error) {
	// Try RFC3339 format first
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}

	// Try other common formats
	formats := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		t, err := time.Parse(format, s)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("failed to parse timestamp: %s", s)
}
