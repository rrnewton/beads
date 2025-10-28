package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

var createCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new issue (or multiple issues from markdown file)",
	Args:  cobra.MinimumNArgs(0), // Changed to allow no args when using -f
	Run: func(cmd *cobra.Command, args []string) {
		file, _ := cmd.Flags().GetString("file")

		// If file flag is provided, parse markdown and create multiple issues
		if file != "" {
			if len(args) > 0 {
				fmt.Fprintf(os.Stderr, "Error: cannot specify both title and --file flag\n")
				os.Exit(1)
			}
			createIssuesFromMarkdown(cmd, file)
			return
		}

		// Original single-issue creation logic
		// Get title from flag or positional argument
		titleFlag, _ := cmd.Flags().GetString("title")
		var title string

		if len(args) > 0 && titleFlag != "" {
			// Both provided - check if they match
			if args[0] != titleFlag {
				fmt.Fprintf(os.Stderr, "Error: cannot specify different titles as both positional argument and --title flag\n")
				fmt.Fprintf(os.Stderr, "  Positional: %q\n", args[0])
				fmt.Fprintf(os.Stderr, "  --title:    %q\n", titleFlag)
				os.Exit(1)
			}
			title = args[0] // They're the same, use either
		} else if len(args) > 0 {
			title = args[0]
		} else if titleFlag != "" {
			title = titleFlag
		} else {
			fmt.Fprintf(os.Stderr, "Error: title required (or use --file to create from markdown)\n")
			os.Exit(1)
		}
		description, _ := cmd.Flags().GetString("description")
		design, _ := cmd.Flags().GetString("design")
		acceptance, _ := cmd.Flags().GetString("acceptance")
		priority, _ := cmd.Flags().GetInt("priority")
		issueType, _ := cmd.Flags().GetString("type")
		assignee, _ := cmd.Flags().GetString("assignee")
		labels, _ := cmd.Flags().GetStringSlice("labels")
		explicitID, _ := cmd.Flags().GetString("id")
		externalRef, _ := cmd.Flags().GetString("external-ref")
		deps, _ := cmd.Flags().GetStringSlice("deps")
		forceCreate, _ := cmd.Flags().GetBool("force")

		// Validate explicit ID format if provided (prefix-number)
		if explicitID != "" {
			// Check format: must contain hyphen and have numeric suffix
			parts := strings.Split(explicitID, "-")
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Error: invalid ID format '%s' (expected format: prefix-number, e.g., 'bd-42')\n", explicitID)
				os.Exit(1)
			}
			// Validate numeric suffix
			if _, err := fmt.Sscanf(parts[1], "%d", new(int)); err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid ID format '%s' (numeric suffix required, e.g., 'bd-42')\n", explicitID)
				os.Exit(1)
			}

			// Validate prefix matches database prefix (unless --force is used)
			if !forceCreate {
				requestedPrefix := parts[0]
				ctx := context.Background()

				// Get database prefix from config
				var dbPrefix string
				if daemonClient != nil {
					// Using daemon - need to get config via RPC
					// For now, skip validation in daemon mode (needs RPC enhancement)
				} else {
					// Direct mode - check config
					dbPrefix, _ = store.GetConfig(ctx, "issue_prefix")
				}

				if dbPrefix != "" && dbPrefix != requestedPrefix {
					fmt.Fprintf(os.Stderr, "Error: prefix mismatch detected\n")
					fmt.Fprintf(os.Stderr, "  This database uses prefix '%s-', but you specified '%s-'\n", dbPrefix, requestedPrefix)
					fmt.Fprintf(os.Stderr, "  Did you mean to create '%s-%s'?\n", dbPrefix, parts[1])
					fmt.Fprintf(os.Stderr, "  Use --force to create with mismatched prefix anyway\n")
					os.Exit(1)
				}
			}
		}

		var externalRefPtr *string
		if externalRef != "" {
			externalRefPtr = &externalRef
		}

		// If daemon is running, use RPC
		if daemonClient != nil {
			createArgs := &rpc.CreateArgs{
				ID:                 explicitID,
				Title:              title,
				Description:        description,
				IssueType:          issueType,
				Priority:           priority,
				Design:             design,
				AcceptanceCriteria: acceptance,
				Assignee:           assignee,
				Labels:             labels,
				Dependencies:       deps,
			}

			resp, err := daemonClient.Create(createArgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				fmt.Println(string(resp.Data))
			} else {
				var issue types.Issue
				if err := json.Unmarshal(resp.Data, &issue); err != nil {
					fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
					os.Exit(1)
				}
				green := color.New(color.FgGreen).SprintFunc()
				fmt.Printf("%s Created issue: %s\n", green("✓"), issue.ID)
				fmt.Printf("  Title: %s\n", issue.Title)
				fmt.Printf("  Priority: P%d\n", issue.Priority)
				fmt.Printf("  Status: %s\n", issue.Status)
			}
			return
		}

		// Direct mode
		issue := &types.Issue{
			ID:                 explicitID, // Set explicit ID if provided (empty string if not)
			Title:              title,
			Description:        description,
			Design:             design,
			AcceptanceCriteria: acceptance,
			Status:             types.StatusOpen,
			Priority:           priority,
			IssueType:          types.IssueType(issueType),
			Assignee:           assignee,
			ExternalRef:        externalRefPtr,
		}

		ctx := context.Background()
		if err := store.CreateIssue(ctx, issue, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Add labels if specified
		for _, label := range labels {
			if err := store.AddLabel(ctx, issue.ID, label, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add label %s: %v\n", label, err)
			}
		}

		// Add dependencies if specified (format: type:id or just id for default "blocks" type)
		for _, depSpec := range deps {
			// Skip empty specs (e.g., from trailing commas)
			depSpec = strings.TrimSpace(depSpec)
			if depSpec == "" {
				continue
			}

			var depType types.DependencyType
			var dependsOnID string

			// Parse format: "type:id" or just "id" (defaults to "blocks")
			if strings.Contains(depSpec, ":") {
				parts := strings.SplitN(depSpec, ":", 2)
				if len(parts) != 2 {
					fmt.Fprintf(os.Stderr, "Warning: invalid dependency format '%s', expected 'type:id' or 'id'\n", depSpec)
					continue
				}
				depType = types.DependencyType(strings.TrimSpace(parts[0]))
				dependsOnID = strings.TrimSpace(parts[1])
			} else {
				// Default to "blocks" if no type specified
				depType = types.DepBlocks
				dependsOnID = depSpec
			}

			// Validate dependency type
			if !depType.IsValid() {
				fmt.Fprintf(os.Stderr, "Warning: invalid dependency type '%s' (valid: blocks, related, parent-child, discovered-from)\n", depType)
				continue
			}

			// Add the dependency
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: dependsOnID,
				Type:        depType,
			}
			if err := store.AddDependency(ctx, dep, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add dependency %s -> %s: %v\n", issue.ID, dependsOnID, err)
			}
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(issue)
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Created issue: %s\n", green("✓"), issue.ID)
			fmt.Printf("  Title: %s\n", issue.Title)
			fmt.Printf("  Priority: P%d\n", issue.Priority)
			fmt.Printf("  Status: %s\n", issue.Status)
		}
	},
}

func init() {
	createCmd.Flags().StringP("file", "f", "", "Create multiple issues from markdown file")
	createCmd.Flags().String("title", "", "Issue title (alternative to positional argument)")
	createCmd.Flags().StringP("description", "d", "", "Issue description")
	createCmd.Flags().String("design", "", "Design notes")
	createCmd.Flags().String("acceptance", "", "Acceptance criteria")
	createCmd.Flags().IntP("priority", "p", 2, "Priority (0-4, 0=highest)")
	createCmd.Flags().StringP("type", "t", "task", "Issue type (bug|feature|task|epic|chore)")
	createCmd.Flags().StringP("assignee", "a", "", "Assignee")
	createCmd.Flags().StringSliceP("labels", "l", []string{}, "Labels (comma-separated)")
	createCmd.Flags().String("id", "", "Explicit issue ID (e.g., 'bd-42' for partitioning)")
	createCmd.Flags().String("external-ref", "", "External reference (e.g., 'gh-9', 'jira-ABC')")
	createCmd.Flags().StringSlice("deps", []string{}, "Dependencies in format 'type:id' or 'id' (e.g., 'discovered-from:bd-20,blocks:bd-15' or 'bd-20')")
	createCmd.Flags().Bool("force", false, "Force creation even if prefix doesn't match database prefix")
	rootCmd.AddCommand(createCmd)
}
