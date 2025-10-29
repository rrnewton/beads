package sqlite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// CollisionResult categorizes incoming issues by their relationship to existing DB state
type CollisionResult struct {
	ExactMatches []string           // IDs that match exactly (idempotent import)
	Collisions   []*CollisionDetail // Issues with same ID but different content
	NewIssues    []string           // IDs that don't exist in DB yet
	Renames      []*RenameDetail    // Issues with same content but different ID (renames)
}

// RenameDetail captures a rename/remap detected during collision detection
type RenameDetail struct {
	OldID string        // ID in database (to be deleted)
	NewID string        // ID in incoming (to be created)
	Issue *types.Issue  // The issue with new ID
}

// CollisionDetail provides detailed information about a collision
type CollisionDetail struct {
	ID                string        // The issue ID that collided
	IncomingIssue     *types.Issue  // The issue from the import file
	ExistingIssue     *types.Issue  // The issue currently in the database
	ConflictingFields []string      // List of field names that differ
	RemapIncoming     bool          // If true, remap incoming; if false, remap existing
}

// DetectCollisions compares incoming JSONL issues against DB state
// It distinguishes between:
//  1. Exact match (idempotent) - ID and content are identical
//  2. ID match but different content (collision) - same ID, different fields
//  3. New issue - ID doesn't exist in DB
//  4. Rename detected - Different ID but same content (from prior remap)
//
// Returns a CollisionResult categorizing all incoming issues.
func DetectCollisions(ctx context.Context, s *SQLiteStorage, incomingIssues []*types.Issue) (*CollisionResult, error) {
	result := &CollisionResult{
		ExactMatches: make([]string, 0),
		Collisions:   make([]*CollisionDetail, 0),
		NewIssues:    make([]string, 0),
	}

	// Phase 1: Deduplicate within incoming batch
	// Group by content hash to find duplicates with different IDs
	deduped := deduplicateIncomingIssues(incomingIssues)

	// Phase 2: Build content hash map of all DB issues
	// This allows us to detect renames (different ID, same content)
	dbIssues, err := s.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to get all DB issues: %w", err)
	}
	
	contentToDBIssue := make(map[string]*types.Issue)
	for _, dbIssue := range dbIssues {
		hash := hashIssueContent(dbIssue)
		contentToDBIssue[hash] = dbIssue
	}

	// Phase 3: Process each incoming issue
	for _, incoming := range deduped {
		incomingHash := hashIssueContent(incoming)
		
		// Check if issue exists in database by ID
		existing, err := s.GetIssue(ctx, incoming.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check issue %s: %w", incoming.ID, err)
		}

		if existing == nil {
			// Issue doesn't exist by ID - check for rename by content
			if dbMatch, found := contentToDBIssue[incomingHash]; found {
				// Same content, different ID - this is a rename/remap
				// The incoming ID is the NEW canonical ID, existing DB ID is OLD
				// Record this as a rename to be handled later (read-only detection)
				result.Renames = append(result.Renames, &RenameDetail{
					OldID: dbMatch.ID,
					NewID: incoming.ID,
					Issue: incoming,
				})
				// Don't add to NewIssues - will be handled by ApplyCollisionResolution
			} else {
				// Truly new issue
				result.NewIssues = append(result.NewIssues, incoming.ID)
			}
			continue
		}

		// Issue exists by ID - compare content
		conflicts := compareIssues(existing, incoming)
		if len(conflicts) == 0 {
			// No differences - exact match (idempotent)
			result.ExactMatches = append(result.ExactMatches, incoming.ID)
		} else {
			// Same ID but different content - collision
			result.Collisions = append(result.Collisions, &CollisionDetail{
				ID:                incoming.ID,
				IncomingIssue:     incoming,
				ExistingIssue:     existing,
				ConflictingFields: conflicts,
			})
		}
	}

	return result, nil
}

// compareIssues compares two issues and returns a list of field names that differ
// Timestamps (CreatedAt, UpdatedAt, ClosedAt) are intentionally not compared
// Dependencies are also not compared (handled separately in import)
func compareIssues(existing, incoming *types.Issue) []string {
	conflicts := make([]string, 0)

	// Compare all relevant fields
	if existing.Title != incoming.Title {
		conflicts = append(conflicts, "title")
	}
	if existing.Description != incoming.Description {
		conflicts = append(conflicts, "description")
	}
	if existing.Design != incoming.Design {
		conflicts = append(conflicts, "design")
	}
	if existing.AcceptanceCriteria != incoming.AcceptanceCriteria {
		conflicts = append(conflicts, "acceptance_criteria")
	}
	if existing.Notes != incoming.Notes {
		conflicts = append(conflicts, "notes")
	}
	if existing.Status != incoming.Status {
		conflicts = append(conflicts, "status")
	}
	if existing.Priority != incoming.Priority {
		conflicts = append(conflicts, "priority")
	}
	if existing.IssueType != incoming.IssueType {
		conflicts = append(conflicts, "issue_type")
	}
	if existing.Assignee != incoming.Assignee {
		conflicts = append(conflicts, "assignee")
	}

	// Compare EstimatedMinutes (handle nil cases)
	if !equalIntPtr(existing.EstimatedMinutes, incoming.EstimatedMinutes) {
		conflicts = append(conflicts, "estimated_minutes")
	}

	// Compare ExternalRef (handle nil cases)
	if !equalStringPtr(existing.ExternalRef, incoming.ExternalRef) {
		conflicts = append(conflicts, "external_ref")
	}

	return conflicts
}

// equalIntPtr compares two *int pointers for equality
func equalIntPtr(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// equalStringPtr compares two *string pointers for equality
func equalStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// hashIssueContent creates a deterministic hash of an issue's content.
// Uses all substantive fields (excluding timestamps and ID) to ensure
// that identical content produces identical hashes across all clones.
func hashIssueContent(issue *types.Issue) string {
	h := sha256.New()
	
	// Hash all substantive fields in a stable order
	h.Write([]byte(issue.Title))
	h.Write([]byte{0}) // separator
	h.Write([]byte(issue.Description))
	h.Write([]byte{0})
	h.Write([]byte(issue.Design))
	h.Write([]byte{0})
	h.Write([]byte(issue.AcceptanceCriteria))
	h.Write([]byte{0})
	h.Write([]byte(issue.Notes))
	h.Write([]byte{0})
	h.Write([]byte(issue.Status))
	h.Write([]byte{0})
	h.Write([]byte(fmt.Sprintf("%d", issue.Priority)))
	h.Write([]byte{0})
	h.Write([]byte(issue.IssueType))
	h.Write([]byte{0})
	h.Write([]byte(issue.Assignee))
	h.Write([]byte{0})
	
	if issue.ExternalRef != nil {
		h.Write([]byte(*issue.ExternalRef))
	}
	
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ApplyCollisionResolution applies the modifications detected during collision detection.
// This function handles:
// 1. Rename deletions (delete old IDs for renamed issues)
// 2. Creating remapped issues (based on mapping)
// 3. Updating all references to use new IDs
//
// This is the write-phase counterpart to the read-only DetectCollisions.
func ApplyCollisionResolution(ctx context.Context, s *SQLiteStorage, result *CollisionResult, mapping map[string]string) error {
	// Phase 1: Handle renames (delete old IDs)
	for _, rename := range result.Renames {
		if err := s.DeleteIssue(ctx, rename.OldID); err != nil {
			return fmt.Errorf("failed to delete renamed issue %s (renamed to %s): %w",
				rename.OldID, rename.NewID, err)
		}
	}

	// Phase 2: Update references using the mapping
	if len(mapping) > 0 {
		if err := updateReferences(ctx, s, mapping); err != nil {
			return fmt.Errorf("failed to update references: %w", err)
		}
	}

	return nil
}

// ScoreCollisions determines which version of each colliding issue to keep vs. remap.
// Uses deterministic content-based hashing to ensure all clones make the same decision.
//
// Decision process:
//  1. Hash both versions (existing and incoming) based on content
//  2. Keep the version with the lexicographically LOWER hash
//  3. Mark the other version for remapping
//
// This ensures:
//  - Deterministic: same collision always produces same result
//  - Symmetric: works regardless of which clone syncs first
//  - Idempotent: converges to same state across all clones
func ScoreCollisions(ctx context.Context, s *SQLiteStorage, collisions []*CollisionDetail, allIssues []*types.Issue) error {
	// Determine which version to keep for each collision
	for _, collision := range collisions {
		existingHash := hashIssueContent(collision.ExistingIssue)
		incomingHash := hashIssueContent(collision.IncomingIssue)
		
		// Keep the version with lower hash (deterministic winner)
		// If incoming has lower hash, we need to remap existing and keep incoming
		// If existing has lower hash, we need to remap incoming and keep existing
		collision.RemapIncoming = existingHash < incomingHash
	}

	return nil
}

// deduplicateIncomingIssues removes content-duplicate issues within the incoming batch
// Returns deduplicated slice, keeping the first issue ID (lexicographically) for each unique content
func deduplicateIncomingIssues(issues []*types.Issue) []*types.Issue {
	// Group issues by content hash (ignoring ID and timestamps)
	type contentKey struct {
		title              string
		description        string
		design             string
		acceptanceCriteria string
		notes              string
		status             string
		priority           int
		issueType          string
		assignee           string
	}

	seen := make(map[contentKey]*types.Issue)
	result := make([]*types.Issue, 0, len(issues))

	for _, issue := range issues {
		key := contentKey{
			title:              issue.Title,
			description:        issue.Description,
			design:             issue.Design,
			acceptanceCriteria: issue.AcceptanceCriteria,
			notes:              issue.Notes,
			status:             string(issue.Status),
			priority:           issue.Priority,
			issueType:          string(issue.IssueType),
			assignee:           issue.Assignee,
		}

		if existing, found := seen[key]; found {
			// Duplicate found - keep the one with lexicographically smaller ID
			if issue.ID < existing.ID {
				// Replace existing with this one (smaller ID)
				for i, r := range result {
					if r.ID == existing.ID {
						result[i] = issue
						break
					}
				}
				seen[key] = issue
			}
			// Otherwise skip this duplicate
		} else {
			// First time seeing this content
			seen[key] = issue
			result = append(result, issue)
		}
	}

	return result
}

// RemapCollisions handles ID remapping for colliding issues based on content hash.
// For each collision, either the incoming or existing issue is remapped (determined by ScoreCollisions).
// Returns a map of old ID -> new ID for reporting.
//
// Process:
//  1. If RemapIncoming=true: remap incoming issue, keep existing
//  2. If RemapIncoming=false: remap existing issue, replace with incoming
//
// This ensures deterministic, symmetric collision resolution across all clones.
//
// NOTE: This function is not atomic - it performs multiple separate database operations.
// If an error occurs partway through, some issues may be created without their references
// being updated. This is a known limitation that requires storage layer refactoring to fix.
// See issue bd-25 for transaction support.
func RemapCollisions(ctx context.Context, s *SQLiteStorage, collisions []*CollisionDetail, _ []*types.Issue) (map[string]string, error) {
	idMapping := make(map[string]string)

	// Sync counters before remapping to avoid ID collisions
	if err := s.SyncAllCounters(ctx); err != nil {
		return nil, fmt.Errorf("failed to sync ID counters: %w", err)
	}

	// Step 1: Collect ALL dependencies before any modifications
	// This prevents CASCADE DELETE from losing dependency information
	allDepsBeforeRemap, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all dependencies: %w", err)
	}

	// Step 2: Process each collision based on which version should be remapped
	for _, collision := range collisions {
		// Skip collisions with nil issues (shouldn't happen but be defensive)
		if collision.IncomingIssue == nil {
			return nil, fmt.Errorf("collision %s has nil IncomingIssue", collision.ID)
		}
		if collision.ExistingIssue == nil {
			return nil, fmt.Errorf("collision %s has nil ExistingIssue", collision.ID)
		}

		oldID := collision.ID

		// Allocate new ID using atomic counter
		prefix := config.GetIssuePrefix()
		if prefix == "" {
			prefix = "bd"
		}
		nextID, err := s.getNextIDForPrefix(ctx, prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to generate new ID for collision %s: %w", oldID, err)
		}
		newID := fmt.Sprintf("%s-%d", prefix, nextID)

		if collision.RemapIncoming {
			// Incoming has higher hash -> remap incoming, keep existing
			// Record mapping
			idMapping[oldID] = newID
			
			// Update incoming issue ID
			collision.IncomingIssue.ID = newID
			
			// Create incoming issue with new ID
			if err := s.CreateIssue(ctx, collision.IncomingIssue, "import-remap"); err != nil {
				return nil, fmt.Errorf("failed to create remapped incoming issue %s -> %s: %w", oldID, newID, err)
			}
		} else {
			// Existing has higher hash -> remap existing, replace with incoming
			// Record mapping FIRST before any operations
			idMapping[oldID] = newID
			
			// Create a copy of existing issue with new ID
			existingCopy := *collision.ExistingIssue
			existingCopy.ID = newID
			
			if err := s.CreateIssue(ctx, &existingCopy, "import-remap"); err != nil {
				return nil, fmt.Errorf("failed to create remapped existing issue %s -> %s: %w", oldID, newID, err)
			}
			
			// Create incoming issue with original ID (this will REPLACE when we delete old ID)
			// We do this BEFORE deleting so both issues exist temporarily
			// Note: This will fail if incoming ID already exists, which is expected in this flow
			// So we skip this step and do it after deletion
			
			// Note: We do NOT copy dependencies here - DeleteIssue will cascade delete them
			// But we've already recorded the mapping, so updateReferences will fix everything
			// after all collisions are processed
			
			// Delete the existing issue with old ID (this will cascade delete old dependencies)
			if err := s.DeleteIssue(ctx, oldID); err != nil {
				return nil, fmt.Errorf("failed to delete old existing issue %s: %w", oldID, err)
			}
			
			// NOW create incoming issue with original ID (replaces the deleted one)
			if err := s.CreateIssue(ctx, collision.IncomingIssue, "import-replace"); err != nil {
				return nil, fmt.Errorf("failed to create incoming issue %s: %w", oldID, err)
			}
		}
	}

	// Step 3: Recreate dependencies with updated IDs
	// For each dependency that involved a remapped issue, recreate it with new IDs
	for issueID, deps := range allDepsBeforeRemap {
		for _, dep := range deps {
			// Determine new IDs (use mapping if available, otherwise keep original)
			newIssueID := issueID
			if mappedID, ok := idMapping[issueID]; ok {
				newIssueID = mappedID
			}
			
			newDependsOnID := dep.DependsOnID
			if mappedID, ok := idMapping[dep.DependsOnID]; ok {
				newDependsOnID = mappedID
			}
			
			// Only recreate if at least one ID was remapped
			if newIssueID != issueID || newDependsOnID != dep.DependsOnID {
				// Check if both issues still exist (the source might have been replaced)
				sourceExists, err := s.GetIssue(ctx, newIssueID)
				if err != nil || sourceExists == nil {
					continue // Skip if source was deleted/replaced
				}
				
				targetExists, err := s.GetIssue(ctx, newDependsOnID)
				if err != nil || targetExists == nil {
					continue // Skip if target doesn't exist
				}
				
				// Create the dependency with new IDs
				newDep := &types.Dependency{
					IssueID:     newIssueID,
					DependsOnID: newDependsOnID,
					Type:        dep.Type,
				}
				if err := s.addDependencyUnchecked(ctx, newDep, "import-remap"); err != nil {
					// Ignore duplicate dependency errors
					continue
				}
			}
		}
	}

	// Step 4: Update all text field references
	if err := updateReferences(ctx, s, idMapping); err != nil {
		return nil, fmt.Errorf("failed to update references: %w", err)
	}

	return idMapping, nil
}

// updateReferences updates all text field references and dependency records
// to point to new IDs based on the idMapping
func updateReferences(ctx context.Context, s *SQLiteStorage, idMapping map[string]string) error {
	// Pre-compile all regexes once for the entire operation
	// This avoids recompiling the same patterns for each text field
	cache, err := BuildReplacementCache(idMapping)
	if err != nil {
		return fmt.Errorf("failed to build replacement cache: %w", err)
	}

	// Update text fields in all issues (both DB and incoming)
	// We need to update issues in the database
	dbIssues, err := s.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("failed to get all issues from DB: %w", err)
	}

	for _, issue := range dbIssues {
		updates := make(map[string]interface{})

		// Update description using cached regexes
		newDesc := ReplaceIDReferencesWithCache(issue.Description, cache)
		if newDesc != issue.Description {
			updates["description"] = newDesc
		}

		// Update design using cached regexes
		newDesign := ReplaceIDReferencesWithCache(issue.Design, cache)
		if newDesign != issue.Design {
			updates["design"] = newDesign
		}

		// Update notes using cached regexes
		newNotes := ReplaceIDReferencesWithCache(issue.Notes, cache)
		if newNotes != issue.Notes {
			updates["notes"] = newNotes
		}

		// Update acceptance criteria using cached regexes
		newAC := ReplaceIDReferencesWithCache(issue.AcceptanceCriteria, cache)
		if newAC != issue.AcceptanceCriteria {
			updates["acceptance_criteria"] = newAC
		}

		// If there are updates, apply them
		if len(updates) > 0 {
			if err := s.UpdateIssue(ctx, issue.ID, updates, "import-remap"); err != nil {
				return fmt.Errorf("failed to update references in issue %s: %w", issue.ID, err)
			}
		}
	}

	// Update dependency records
	if err := updateDependencyReferences(ctx, s, idMapping); err != nil {
		return fmt.Errorf("failed to update dependency references: %w", err)
	}

	return nil
}

// idReplacementCache stores pre-compiled regexes for ID replacements
// This avoids recompiling the same regex patterns for each text field
type idReplacementCache struct {
	oldID       string
	newID       string
	placeholder string
	regex       *regexp.Regexp
}

// BuildReplacementCache pre-compiles all regex patterns for an ID mapping
// This cache should be created once per ID mapping and reused for all text replacements
func BuildReplacementCache(idMapping map[string]string) ([]*idReplacementCache, error) {
	cache := make([]*idReplacementCache, 0, len(idMapping))
	i := 0
	for oldID, newID := range idMapping {
		// Use word boundary regex for exact matching
		pattern := fmt.Sprintf(`\b%s\b`, regexp.QuoteMeta(oldID))
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile regex for %s: %w", oldID, err)
		}

		cache = append(cache, &idReplacementCache{
			oldID:       oldID,
			newID:       newID,
			placeholder: fmt.Sprintf("\x00REMAP\x00_%d_\x00", i),
			regex:       re,
		})
		i++
	}
	return cache, nil
}

// ReplaceIDReferencesWithCache replaces all occurrences of old IDs with new IDs using a pre-compiled cache
// Uses a two-phase approach to avoid replacement conflicts: first replace with placeholders, then replace with new IDs
func ReplaceIDReferencesWithCache(text string, cache []*idReplacementCache) string {
	if len(cache) == 0 || text == "" {
		return text
	}

	// Phase 1: Replace all old IDs with unique placeholders
	result := text
	for _, entry := range cache {
		result = entry.regex.ReplaceAllString(result, entry.placeholder)
	}

	// Phase 2: Replace all placeholders with new IDs
	for _, entry := range cache {
		result = strings.ReplaceAll(result, entry.placeholder, entry.newID)
	}

	return result
}

// replaceIDReferences replaces all occurrences of old IDs with new IDs in text
// Uses word-boundary regex to ensure exact matches (bd-10 but not bd-100)
// Uses a two-phase approach to avoid replacement conflicts: first replace with
// placeholders, then replace placeholders with new IDs
//
// Note: This function compiles regexes on every call. For better performance when
// processing multiple text fields with the same ID mapping, use BuildReplacementCache()
// and ReplaceIDReferencesWithCache() instead.
func replaceIDReferences(text string, idMapping map[string]string) string {
	// Build cache (compiles regexes)
	cache, err := BuildReplacementCache(idMapping)
	if err != nil {
		// Fallback to no replacement if regex compilation fails
		return text
	}
	return ReplaceIDReferencesWithCache(text, cache)
}

// updateDependencyReferences updates dependency records to use new IDs
// This handles both IssueID and DependsOnID fields
// IMPORTANT: Only updates dependencies belonging to REMAPPED issues (with new IDs from idMapping).
// Dependencies belonging to existing issues are left untouched.
//
// NOTE: During normal import flow, this is effectively a no-op because imported dependencies
// haven't been added to the database yet when RemapCollisions runs. Dependencies are imported
// later in Phase 5 of import_shared.go. However, this function still serves as a safety guard
// and handles edge cases where dependencies might exist with the new remapped IDs.
func updateDependencyReferences(ctx context.Context, s *SQLiteStorage, idMapping map[string]string) error {
	// Build set of NEW remapped IDs (idMapping values)
	// Only dependencies with these IDs as IssueID should be updated
	newRemappedIDs := make(map[string]bool)
	for _, newID := range idMapping {
		newRemappedIDs[newID] = true
	}

	// Get all dependency records
	allDeps, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all dependencies: %w", err)
	}

	// Phase 1: Collect all changes to avoid race conditions while iterating
	type depUpdate struct {
		oldIssueID     string
		oldDependsOnID string
		newDep         *types.Dependency
	}
	var updates []depUpdate

	for _, deps := range allDeps {
		for _, dep := range deps {
			// CRITICAL FIX: Only update dependencies that belong to REMAPPED issues
			// A dependency belongs to a remapped issue if its IssueID is a NEW remapped ID
			// (one of the VALUES in idMapping, not the keys)
			//
			// We must NOT check against idMapping keys (old IDs) because those are the same
			// as existing issue IDs in the database, and we'd incorrectly modify their dependencies.
			if !newRemappedIDs[dep.IssueID] {
				// This dependency does not belong to a remapped issue - skip it
				continue
			}

			needsUpdate := false
			newIssueID := dep.IssueID
			newDependsOnID := dep.DependsOnID

			// Check if either ID was remapped
			if mappedID, ok := idMapping[dep.IssueID]; ok {
				newIssueID = mappedID
				needsUpdate = true
			}
			if mappedID, ok := idMapping[dep.DependsOnID]; ok {
				newDependsOnID = mappedID
				needsUpdate = true
			}

			if needsUpdate {
				updates = append(updates, depUpdate{
					oldIssueID:     dep.IssueID,
					oldDependsOnID: dep.DependsOnID,
					newDep: &types.Dependency{
						IssueID:     newIssueID,
						DependsOnID: newDependsOnID,
						Type:        dep.Type,
					},
				})
			}
		}
	}

	// Phase 2: Apply all collected changes
	for _, update := range updates {
		// Remove old dependency - use RemoveDependencyIfExists which doesn't error on missing deps
		if err := s.removeDependencyIfExists(ctx, update.oldIssueID, update.oldDependsOnID, "import-remap"); err != nil {
			return fmt.Errorf("failed to remove old dependency %s -> %s: %w",
				update.oldIssueID, update.oldDependsOnID, err)
		}

		// Add new dependency with updated IDs
		// Use addDependencyUnchecked to skip semantic validation (like parent-child direction)
		// since we're just remapping existing dependencies that were already validated
		if err := s.addDependencyUnchecked(ctx, update.newDep, "import-remap"); err != nil {
			return fmt.Errorf("failed to add updated dependency %s -> %s: %w",
				update.newDep.IssueID, update.newDep.DependsOnID, err)
		}
	}

	return nil
}
