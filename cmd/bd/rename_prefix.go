package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

var renamePrefixCmd = &cobra.Command{
	Use:   "rename-prefix <new-prefix>",
	Short: "Rename the issue prefix for all issues",
	Long: `Rename the issue prefix for all issues in the database.
This will update all issue IDs and all text references across all fields.

Prefix validation rules:
- Max length: 8 characters
- Allowed characters: lowercase letters, numbers, hyphens
- Must start with a letter
- Must end with a hyphen (e.g., 'kw-', 'work-')
- Cannot be empty or just a hyphen

Multiple prefix detection and repair:
If issues have multiple prefixes (corrupted database), use --repair to consolidate them.
The --repair flag will rename all issues with incorrect prefixes to the new prefix,
preserving issues that already have the correct prefix.

Example:
  bd rename-prefix kw-         # Rename from 'knowledge-work-' to 'kw-'
  bd rename-prefix mtg- --repair  # Consolidate multiple prefixes into 'mtg-'`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		newPrefix := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		repair, _ := cmd.Flags().GetBool("repair")

		ctx := context.Background()

		// rename-prefix requires direct mode (not supported by daemon)
		if daemonClient != nil {
			if err := ensureDirectMode("daemon does not support rename-prefix command"); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else if store == nil {
			if err := ensureStoreActive(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		if err := validatePrefix(newPrefix); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		oldPrefix, err := store.GetConfig(ctx, "issue_prefix")
		if err != nil || oldPrefix == "" {
			fmt.Fprintf(os.Stderr, "Error: failed to get current prefix: %v\n", err)
			os.Exit(1)
		}

		newPrefix = strings.TrimRight(newPrefix, "-")

		// Check for multiple prefixes first
		issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to list issues: %v\n", err)
			os.Exit(1)
		}

		prefixes := detectPrefixes(issues)

		if len(prefixes) > 1 {
			// Multiple prefixes detected - requires repair mode
			red := color.New(color.FgRed).SprintFunc()
			yellow := color.New(color.FgYellow).SprintFunc()

			fmt.Fprintf(os.Stderr, "%s Multiple prefixes detected in database:\n", red("✗"))
			for prefix, count := range prefixes {
				fmt.Fprintf(os.Stderr, "  - %s: %d issues\n", yellow(prefix), count)
			}
			fmt.Fprintf(os.Stderr, "\n")

			if !repair {
				fmt.Fprintf(os.Stderr, "Error: cannot rename with multiple prefixes. Use --repair to consolidate.\n")
				fmt.Fprintf(os.Stderr, "Example: bd rename-prefix %s --repair\n", newPrefix)
				os.Exit(1)
			}

			// Repair mode: consolidate all prefixes to newPrefix
			if err := repairPrefixes(ctx, store, actor, newPrefix, issues, prefixes, dryRun); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to repair prefixes: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Single prefix case - check if trying to rename to same prefix
		if len(prefixes) == 1 && oldPrefix == newPrefix {
			fmt.Fprintf(os.Stderr, "Error: new prefix is the same as current prefix: %s\n", oldPrefix)
			os.Exit(1)
		}

		// issues already fetched above
		if len(issues) == 0 {
			fmt.Printf("No issues to rename. Updating prefix to %s\n", newPrefix)
			if !dryRun {
				if err := store.SetConfig(ctx, "issue_prefix", newPrefix); err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to update prefix: %v\n", err)
					os.Exit(1)
				}
			}
			return
		}

		if dryRun {
			cyan := color.New(color.FgCyan).SprintFunc()
			fmt.Printf("DRY RUN: Would rename %d issues from prefix '%s' to '%s'\n\n", len(issues), oldPrefix, newPrefix)
			fmt.Printf("Sample changes:\n")
			for i, issue := range issues {
				if i >= 5 {
					fmt.Printf("... and %d more issues\n", len(issues)-5)
					break
				}
				oldID := fmt.Sprintf("%s-%s", oldPrefix, strings.TrimPrefix(issue.ID, oldPrefix+"-"))
				newID := fmt.Sprintf("%s-%s", newPrefix, strings.TrimPrefix(issue.ID, oldPrefix+"-"))
				fmt.Printf("  %s -> %s\n", cyan(oldID), cyan(newID))
			}
			return
		}

		green := color.New(color.FgGreen).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()

		fmt.Printf("Renaming %d issues from prefix '%s' to '%s'...\n", len(issues), oldPrefix, newPrefix)

		if err := renamePrefixInDB(ctx, oldPrefix, newPrefix, issues); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to rename prefix: %v\n", err)
			os.Exit(1)
		}

		// Schedule full export (IDs changed, incremental won't work)
		markDirtyAndScheduleFullExport()

		fmt.Printf("%s Successfully renamed prefix from %s to %s\n", green("✓"), cyan(oldPrefix), cyan(newPrefix))

		if jsonOutput {
			result := map[string]interface{}{
				"old_prefix":   oldPrefix,
				"new_prefix":   newPrefix,
				"issues_count": len(issues),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(result)
		}
	},
}

func validatePrefix(prefix string) error {
	prefix = strings.TrimRight(prefix, "-")

	if prefix == "" {
		return fmt.Errorf("prefix cannot be empty")
	}

	if len(prefix) > 8 {
		return fmt.Errorf("prefix too long (max 8 characters): %s", prefix)
	}

	matched, _ := regexp.MatchString(`^[a-z][a-z0-9-]*$`, prefix)
	if !matched {
		return fmt.Errorf("prefix must start with a lowercase letter and contain only lowercase letters, numbers, and hyphens: %s", prefix)
	}

	if strings.HasPrefix(prefix, "-") || strings.HasSuffix(prefix, "--") {
		return fmt.Errorf("prefix has invalid hyphen placement: %s", prefix)
	}

	return nil
}

// detectPrefixes analyzes all issues and returns a map of prefix -> count
func detectPrefixes(issues []*types.Issue) map[string]int {
	prefixes := make(map[string]int)
	for _, issue := range issues {
		prefix := utils.ExtractIssuePrefix(issue.ID)
		if prefix != "" {
			prefixes[prefix]++
		}
	}
	return prefixes
}

// issueSort is used for sorting issues by prefix and number
type issueSort struct {
	issue  *types.Issue
	prefix string
	number int
}

// repairPrefixes consolidates multiple prefixes into a single target prefix
// Issues with the correct prefix are left unchanged.
// Issues with incorrect prefixes are sorted and renumbered sequentially.
func repairPrefixes(ctx context.Context, st storage.Storage, actorName string, targetPrefix string, issues []*types.Issue, prefixes map[string]int, dryRun bool) error {
	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	// Separate issues into correct and incorrect prefix groups
	var correctIssues []*types.Issue
	var incorrectIssues []issueSort

	maxCorrectNumber := 0
	for _, issue := range issues {
		prefix := utils.ExtractIssuePrefix(issue.ID)
		number := utils.ExtractIssueNumber(issue.ID)

		if prefix == targetPrefix {
			correctIssues = append(correctIssues, issue)
			if number > maxCorrectNumber {
				maxCorrectNumber = number
			}
		} else {
			incorrectIssues = append(incorrectIssues, issueSort{
				issue:  issue,
				prefix: prefix,
				number: number,
			})
		}
	}

	// Sort incorrect issues: first by prefix lexicographically, then by number
	sort.Slice(incorrectIssues, func(i, j int) bool {
		if incorrectIssues[i].prefix != incorrectIssues[j].prefix {
			return incorrectIssues[i].prefix < incorrectIssues[j].prefix
		}
		return incorrectIssues[i].number < incorrectIssues[j].number
	})

	if dryRun {
		fmt.Printf("DRY RUN: Would repair %d issues with incorrect prefixes\n\n", len(incorrectIssues))
		fmt.Printf("Issues with correct prefix (%s): %d (highest number: %d)\n", cyan(targetPrefix), len(correctIssues), maxCorrectNumber)
		fmt.Printf("Issues to repair: %d\n\n", len(incorrectIssues))

		fmt.Printf("Planned renames (showing first 10):\n")
		nextNumber := maxCorrectNumber + 1
		for i, is := range incorrectIssues {
			if i >= 10 {
				fmt.Printf("... and %d more\n", len(incorrectIssues)-10)
				break
			}
			oldID := is.issue.ID
			newID := fmt.Sprintf("%s-%d", targetPrefix, nextNumber)
			fmt.Printf("  %s -> %s\n", yellow(oldID), cyan(newID))
			nextNumber++
		}
		return nil
	}

	// Perform the repairs
	fmt.Printf("Repairing database with multiple prefixes...\n")
	fmt.Printf("  Issues with correct prefix (%s): %d (highest: %s-%d)\n",
		cyan(targetPrefix), len(correctIssues), targetPrefix, maxCorrectNumber)
	fmt.Printf("  Issues to repair: %d\n\n", len(incorrectIssues))

	oldPrefixPattern := regexp.MustCompile(`\b[a-z][a-z0-9-]*-(\d+)\b`)

	// Build a map of all renames for text replacement
	renameMap := make(map[string]string)
	nextNumber := maxCorrectNumber + 1
	for _, is := range incorrectIssues {
		oldID := is.issue.ID
		newID := fmt.Sprintf("%s-%d", targetPrefix, nextNumber)
		renameMap[oldID] = newID
		nextNumber++
	}

	// Rename each issue
	for _, is := range incorrectIssues {
		oldID := is.issue.ID
		newID := renameMap[oldID]

		// Apply text replacements in all issue fields
		issue := is.issue
		issue.ID = newID

		// Replace all issue IDs in text fields using the rename map
		replaceFunc := func(match string) string {
			if newID, ok := renameMap[match]; ok {
				return newID
			}
			return match
		}

		issue.Title = oldPrefixPattern.ReplaceAllStringFunc(issue.Title, replaceFunc)
		issue.Description = oldPrefixPattern.ReplaceAllStringFunc(issue.Description, replaceFunc)
		if issue.Design != "" {
			issue.Design = oldPrefixPattern.ReplaceAllStringFunc(issue.Design, replaceFunc)
		}
		if issue.AcceptanceCriteria != "" {
			issue.AcceptanceCriteria = oldPrefixPattern.ReplaceAllStringFunc(issue.AcceptanceCriteria, replaceFunc)
		}
		if issue.Notes != "" {
			issue.Notes = oldPrefixPattern.ReplaceAllStringFunc(issue.Notes, replaceFunc)
		}

		// Update the issue in the database
		if err := st.UpdateIssueID(ctx, oldID, newID, issue, actorName); err != nil {
			return fmt.Errorf("failed to update issue %s -> %s: %w", oldID, newID, err)
		}

		fmt.Printf("  Renamed %s -> %s\n", yellow(oldID), cyan(newID))
	}

	// Update all dependencies to use new prefix
	for oldPrefix := range prefixes {
		if oldPrefix != targetPrefix {
			if err := st.RenameDependencyPrefix(ctx, oldPrefix, targetPrefix); err != nil {
				return fmt.Errorf("failed to update dependencies for prefix %s: %w", oldPrefix, err)
			}
		}
	}

	// Update counters for all old prefixes
	for oldPrefix := range prefixes {
		if oldPrefix != targetPrefix {
			if err := st.RenameCounterPrefix(ctx, oldPrefix, targetPrefix); err != nil {
				return fmt.Errorf("failed to update counter for prefix %s: %w", oldPrefix, err)
			}
		}
	}

	// Set the new prefix in config
	if err := st.SetConfig(ctx, "issue_prefix", targetPrefix); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	// Schedule full export (IDs changed, incremental won't work)
	markDirtyAndScheduleFullExport()

	fmt.Printf("\n%s Successfully consolidated %d prefixes into %s\n",
		green("✓"), len(prefixes), cyan(targetPrefix))
	fmt.Printf("  %d issues repaired, %d issues unchanged\n", len(incorrectIssues), len(correctIssues))

	if jsonOutput {
		result := map[string]interface{}{
			"target_prefix":     targetPrefix,
			"prefixes_found":    len(prefixes),
			"issues_repaired":   len(incorrectIssues),
			"issues_unchanged":  len(correctIssues),
			"highest_number":    nextNumber - 1,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	}

	return nil
}

func renamePrefixInDB(ctx context.Context, oldPrefix, newPrefix string, issues []*types.Issue) error {
	// NOTE: Each issue is updated in its own transaction. A failure mid-way could leave
	// the database in a mixed state with some issues renamed and others not.
	// For production use, consider implementing a single atomic RenamePrefix() method
	// in the storage layer that wraps all updates in one transaction.

	oldPrefixPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldPrefix) + `-(\d+)\b`)

	replaceFunc := func(match string) string {
		return strings.Replace(match, oldPrefix+"-", newPrefix+"-", 1)
	}

	for _, issue := range issues {
		oldID := issue.ID
		numPart := strings.TrimPrefix(oldID, oldPrefix+"-")
		newID := fmt.Sprintf("%s-%s", newPrefix, numPart)

		issue.ID = newID

		issue.Title = oldPrefixPattern.ReplaceAllStringFunc(issue.Title, replaceFunc)
		issue.Description = oldPrefixPattern.ReplaceAllStringFunc(issue.Description, replaceFunc)
		if issue.Design != "" {
			issue.Design = oldPrefixPattern.ReplaceAllStringFunc(issue.Design, replaceFunc)
		}
		if issue.AcceptanceCriteria != "" {
			issue.AcceptanceCriteria = oldPrefixPattern.ReplaceAllStringFunc(issue.AcceptanceCriteria, replaceFunc)
		}
		if issue.Notes != "" {
			issue.Notes = oldPrefixPattern.ReplaceAllStringFunc(issue.Notes, replaceFunc)
		}

		if err := store.UpdateIssueID(ctx, oldID, newID, issue, actor); err != nil {
			return fmt.Errorf("failed to update issue %s: %w", oldID, err)
		}
	}

	if err := store.RenameDependencyPrefix(ctx, oldPrefix, newPrefix); err != nil {
		return fmt.Errorf("failed to update dependencies: %w", err)
	}

	if err := store.RenameCounterPrefix(ctx, oldPrefix, newPrefix); err != nil {
		return fmt.Errorf("failed to update counter: %w", err)
	}

	if err := store.SetConfig(ctx, "issue_prefix", newPrefix); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	return nil
}

func init() {
	renamePrefixCmd.Flags().Bool("dry-run", false, "Preview changes without applying them")
	renamePrefixCmd.Flags().Bool("repair", false, "Repair database with multiple prefixes by consolidating them")
	rootCmd.AddCommand(renamePrefixCmd)
}
