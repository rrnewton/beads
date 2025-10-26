package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <issue-id>",
	Short: "Restore full history of a compacted issue from git",
	Long: `Restore full history of a compacted issue from git version control.

When an issue is compacted, the git commit hash is saved. This command:
1. Reads the compacted_at_commit from the database
2. Checks out that commit temporarily
3. Reads the full issue from JSONL at that point in history
4. Displays the full issue history (description, events, etc.)
5. Returns to the current git state

This is read-only and does not modify the database or git state.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		issueID := args[0]
		ctx := context.Background()

		// Check if we're in a git repository
		if !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: not in a git repository\n")
			fmt.Fprintf(os.Stderr, "Hint: restore requires git to access historical versions\n")
			os.Exit(1)
		}

		// Get the issue
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: issue %s not found: %v\n", issueID, err)
			os.Exit(1)
		}

		// Check if issue is compacted
		if issue.CompactedAtCommit == nil || *issue.CompactedAtCommit == "" {
			fmt.Fprintf(os.Stderr, "Error: issue %s is not compacted (no git commit saved)\n", issueID)
			fmt.Fprintf(os.Stderr, "Hint: only compacted issues can be restored from git history\n")
			os.Exit(1)
		}

		commitHash := *issue.CompactedAtCommit

		// Find JSONL path
		jsonlPath := findJSONLPath()
		if jsonlPath == "" {
			fmt.Fprintf(os.Stderr, "Error: not in a bd workspace (no .beads directory found)\n")
			os.Exit(1)
		}

		// Get current git HEAD for restoration
		currentHead, err := getCurrentGitHead()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot determine current git HEAD: %v\n", err)
			os.Exit(1)
		}

		// Check for uncommitted changes
		hasChanges, err := gitHasUncommittedChanges()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking git status: %v\n", err)
			os.Exit(1)
		}
		if hasChanges {
			fmt.Fprintf(os.Stderr, "Error: you have uncommitted changes\n")
			fmt.Fprintf(os.Stderr, "Hint: commit or stash changes before running restore\n")
			os.Exit(1)
		}

		// Checkout the historical commit
		if err := gitCheckout(commitHash); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking out commit %s: %v\n", commitHash, err)
			os.Exit(1)
		}

		// Ensure we return to current state
		defer func() {
			if err := gitCheckout(currentHead); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to return to %s: %v\n", currentHead, err)
			}
		}()

		// Read the issue from JSONL at this commit
		historicalIssue, err := readIssueFromJSONL(jsonlPath, issueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading issue from historical JSONL: %v\n", err)
			os.Exit(1)
		}

		if historicalIssue == nil {
			fmt.Fprintf(os.Stderr, "Error: issue %s not found in JSONL at commit %s\n", issueID, commitHash)
			os.Exit(1)
		}

		// Display the restored issue
		displayRestoredIssue(historicalIssue, commitHash)
	},
}

func init() {
	rootCmd.AddCommand(restoreCmd)
}

// getCurrentGitHead returns the current HEAD reference (branch or commit)
func getCurrentGitHead() (string, error) {
	// Try to get symbolic ref (branch name) first
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	if output, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// If not on a branch, get commit hash
	cmd = exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// gitHasUncommittedChanges checks for any uncommitted changes
func gitHasUncommittedChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status failed: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// gitCheckout checks out a specific commit or branch
func gitCheckout(ref string) error {
	cmd := exec.Command("git", "checkout", ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %w\n%s", err, output)
	}
	return nil
}

// readIssueFromJSONL reads a specific issue from JSONL file
func readIssueFromJSONL(jsonlPath, issueID string) (*types.Issue, error) {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSONL: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for large issues
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max

	for scanner.Scan() {
		var issue types.Issue
		if err := json.Unmarshal(scanner.Bytes(), &issue); err != nil {
			continue // Skip malformed lines
		}
		if issue.ID == issueID {
			return &issue, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading JSONL: %w", err)
	}

	return nil, nil // Not found
}

// displayRestoredIssue displays the restored issue in a readable format
func displayRestoredIssue(issue *types.Issue, commitHash string) {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	fmt.Printf("\n%s %s (restored from git commit %s)\n", cyan("📜"), bold(issue.ID), yellow(commitHash[:8]))
	fmt.Printf("%s\n\n", bold(issue.Title))

	if issue.Description != "" {
		fmt.Printf("%s\n%s\n\n", bold("Description:"), issue.Description)
	}

	if issue.Design != "" {
		fmt.Printf("%s\n%s\n\n", bold("Design:"), issue.Design)
	}

	if issue.AcceptanceCriteria != "" {
		fmt.Printf("%s\n%s\n\n", bold("Acceptance Criteria:"), issue.AcceptanceCriteria)
	}

	if issue.Notes != "" {
		fmt.Printf("%s\n%s\n\n", bold("Notes:"), issue.Notes)
	}

	fmt.Printf("%s %s | %s %d | %s %s\n",
		bold("Status:"), issue.Status,
		bold("Priority:"), issue.Priority,
		bold("Type:"), issue.IssueType,
	)

	if issue.Assignee != "" {
		fmt.Printf("%s %s\n", bold("Assignee:"), issue.Assignee)
	}

	if len(issue.Labels) > 0 {
		fmt.Printf("%s %s\n", bold("Labels:"), strings.Join(issue.Labels, ", "))
	}

	fmt.Printf("\n%s %s\n", bold("Created:"), issue.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("%s %s\n", bold("Updated:"), issue.UpdatedAt.Format("2006-01-02 15:04:05"))
	if issue.ClosedAt != nil {
		fmt.Printf("%s %s\n", bold("Closed:"), issue.ClosedAt.Format("2006-01-02 15:04:05"))
	}

	if len(issue.Dependencies) > 0 {
		fmt.Printf("\n%s\n", bold("Dependencies:"))
		for _, dep := range issue.Dependencies {
			fmt.Printf("  %s %s (%s)\n", green("→"), dep.DependsOnID, dep.Type)
		}
	}

	if issue.CompactionLevel > 0 {
		fmt.Printf("\n%s Level %d", yellow("⚠️  This issue was compacted:"), issue.CompactionLevel)
		if issue.CompactedAt != nil {
			fmt.Printf(" at %s", issue.CompactedAt.Format("2006-01-02 15:04:05"))
		}
		if issue.OriginalSize > 0 {
			currentSize := len(issue.Description) + len(issue.Design) + len(issue.AcceptanceCriteria) + len(issue.Notes)
			reduction := 100 * (1 - float64(currentSize)/float64(issue.OriginalSize))
			fmt.Printf(" (%.1f%% size reduction)", reduction)
		}
		fmt.Println()
	}

	fmt.Println()
}
