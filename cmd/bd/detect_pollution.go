package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

var detectPollutionCmd = &cobra.Command{
	Use:   "detect-pollution",
	Short: "Detect test issues that leaked into production database",
	Long: `Detect test issues using pattern matching:
- Titles starting with 'test', 'benchmark', 'sample', 'tmp', 'temp'
- Sequential numbering (test-1, test-2, ...)
- Generic descriptions or no description
- Created in rapid succession

Example:
  bd detect-pollution                 # Show potential test issues
  bd detect-pollution --clean         # Delete test issues (with confirmation)
  bd detect-pollution --clean --yes   # Delete without confirmation
  bd detect-pollution --json          # Output in JSON format`,
	Run: func(cmd *cobra.Command, _ []string) {
		// Check daemon mode - not supported yet (uses direct storage access)
		if daemonClient != nil {
			fmt.Fprintf(os.Stderr, "Error: detect-pollution command not yet supported in daemon mode\n")
			fmt.Fprintf(os.Stderr, "Use: bd --no-daemon detect-pollution\n")
			os.Exit(1)
		}

		clean, _ := cmd.Flags().GetBool("clean")
		yes, _ := cmd.Flags().GetBool("yes")

		ctx := context.Background()

		// Get all issues
		allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching issues: %v\n", err)
			os.Exit(1)
		}

		// Detect pollution
		polluted := detectTestPollution(allIssues)

		if len(polluted) == 0 {
			if !jsonOutput {
				fmt.Println("No test pollution detected!")
			} else {
				outputJSON(map[string]interface{}{
					"polluted_count": 0,
					"issues":         []interface{}{},
				})
			}
			return
		}

		// Categorize by confidence
		highConfidence := []pollutionResult{}
		mediumConfidence := []pollutionResult{}
		
		for _, p := range polluted {
			if p.score >= 0.9 {
				highConfidence = append(highConfidence, p)
			} else {
				mediumConfidence = append(mediumConfidence, p)
			}
		}

		if jsonOutput {
			result := map[string]interface{}{
				"polluted_count":    len(polluted),
				"high_confidence":   len(highConfidence),
				"medium_confidence": len(mediumConfidence),
				"issues":            []map[string]interface{}{},
			}

			for _, p := range polluted {
				result["issues"] = append(result["issues"].([]map[string]interface{}), map[string]interface{}{
					"id":         p.issue.ID,
					"title":      p.issue.Title,
					"score":      p.score,
					"reasons":    p.reasons,
					"created_at": p.issue.CreatedAt,
				})
			}

			outputJSON(result)
			return
		}

		// Human-readable output
		fmt.Printf("Found %d potential test issues:\n\n", len(polluted))
		
		if len(highConfidence) > 0 {
			fmt.Printf("High Confidence (score ≥ 0.9):\n")
			for _, p := range highConfidence {
				fmt.Printf("  %s: %q (score: %.2f)\n", p.issue.ID, p.issue.Title, p.score)
				for _, reason := range p.reasons {
					fmt.Printf("    - %s\n", reason)
				}
			}
			fmt.Printf("  (Total: %d issues)\n\n", len(highConfidence))
		}
		
		if len(mediumConfidence) > 0 {
			fmt.Printf("Medium Confidence (score 0.7-0.9):\n")
			for _, p := range mediumConfidence {
				fmt.Printf("  %s: %q (score: %.2f)\n", p.issue.ID, p.issue.Title, p.score)
				for _, reason := range p.reasons {
					fmt.Printf("    - %s\n", reason)
				}
			}
			fmt.Printf("  (Total: %d issues)\n\n", len(mediumConfidence))
		}

		if !clean {
			fmt.Printf("Run 'bd detect-pollution --clean' to delete these issues (with confirmation).\n")
			return
		}

		// Confirmation prompt
		if !yes {
			fmt.Printf("\nDelete %d test issues? [y/N] ", len(polluted))
			var response string
			_, _ = fmt.Scanln(&response) // Ignore error - empty input defaults to "no"
			if strings.ToLower(response) != "y" {
				fmt.Println("Canceled.")
				return
			}
		}

		// Backup to JSONL before deleting
		backupPath := ".beads/pollution-backup.jsonl"
		if err := backupPollutedIssues(polluted, backupPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error backing up issues: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Backed up %d issues to %s\n", len(polluted), backupPath)

		// Delete issues
		fmt.Printf("\nDeleting %d issues...\n", len(polluted))
		deleted := 0
		for _, p := range polluted {
			if err := deleteIssue(ctx, p.issue.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Error deleting %s: %v\n", p.issue.ID, err)
				continue
			}
			deleted++
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s Deleted %d test issues\n", green("✓"), deleted)
		fmt.Printf("\nCleanup complete. To restore, run: bd import %s\n", backupPath)
	},
}

type pollutionResult struct {
	issue   *types.Issue
	score   float64
	reasons []string
}

func detectTestPollution(issues []*types.Issue) []pollutionResult {
	var results []pollutionResult
	
	// Patterns for test issue titles
	testPrefixPattern := regexp.MustCompile(`^(test|benchmark|sample|tmp|temp|debug|dummy)[-_\s]`)
	sequentialPattern := regexp.MustCompile(`^[a-z]+-\d+$`)
	
	// Group issues by creation time to detect rapid succession
	issuesByMinute := make(map[int64][]*types.Issue)
	for _, issue := range issues {
		minute := issue.CreatedAt.Unix() / 60
		issuesByMinute[minute] = append(issuesByMinute[minute], issue)
	}
	
	for _, issue := range issues {
		score := 0.0
		var reasons []string
		
		title := strings.ToLower(issue.Title)
		
		// Check for test prefixes (strong signal)
		if testPrefixPattern.MatchString(title) {
			score += 0.7
			reasons = append(reasons, "Title starts with test prefix")
		}
		
		// Check for sequential numbering (medium signal)
		if sequentialPattern.MatchString(issue.ID) && len(issue.Description) < 20 {
			score += 0.4
			reasons = append(reasons, "Sequential ID with minimal description")
		}
		
		// Check for generic/empty description (weak signal)
		if len(strings.TrimSpace(issue.Description)) == 0 {
			score += 0.2
			reasons = append(reasons, "No description")
		} else if len(issue.Description) < 20 {
			score += 0.1
			reasons = append(reasons, "Very short description")
		}
		
		// Check for rapid creation (created with many others in same minute)
		minute := issue.CreatedAt.Unix() / 60
		if len(issuesByMinute[minute]) >= 10 {
			score += 0.3
			reasons = append(reasons, fmt.Sprintf("Created with %d other issues in same minute", len(issuesByMinute[minute])-1))
		}
		
		// Check for generic test titles
		if strings.Contains(title, "issue for testing") ||
		   strings.Contains(title, "test issue") ||
		   strings.Contains(title, "sample issue") {
			score += 0.5
			reasons = append(reasons, "Generic test title")
		}
		
		// Only include if score is above threshold
		if score >= 0.7 {
			results = append(results, pollutionResult{
				issue:   issue,
				score:   score,
				reasons: reasons,
			})
		}
	}
	
	return results
}

func backupPollutedIssues(polluted []pollutionResult, path string) error {
	// Create backup file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() { _ = file.Close() }()
	
	// Write each issue as JSONL
	for _, p := range polluted {
		data, err := json.Marshal(p.issue)
		if err != nil {
			return fmt.Errorf("failed to marshal issue %s: %w", p.issue.ID, err)
		}
		
		if _, err := file.WriteString(string(data) + "\n"); err != nil {
			return fmt.Errorf("failed to write issue %s: %w", p.issue.ID, err)
		}
	}
	
	return nil
}

func init() {
	detectPollutionCmd.Flags().Bool("clean", false, "Delete detected test issues")
	detectPollutionCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	rootCmd.AddCommand(detectPollutionCmd)
}
