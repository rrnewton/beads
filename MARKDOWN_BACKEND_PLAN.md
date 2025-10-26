# Markdown Backend Implementation Plan

## Overview

Create a new storage backend for beads using exploded markdown files instead of SQLite. Each issue is a separate `.md` file, making merge conflicts easier to resolve for humans and AI agents.

## Goals

1. **Drop-in replacement**: Implement `storage.Storage` interface identically to SQLite
2. **Human-friendly format**: YAML frontmatter + Markdown for readability
3. **Git-friendly**: Each issue is a separate file for better merge conflict resolution
4. **Concurrent-safe**: File-based locking protocol for multi-process safety
5. **Configurable**: Toggle via `.beads/config.yaml` setting `backend: sqlite | markdown`

## File Format Specification

### Directory Structure

```
.beads/
├── config.yaml              # Contains backend: markdown
├── markdown.db/             # Root directory for markdown backend
│   ├── issues/
│   │   ├── prefix-1.md
│   │   ├── prefix-2.md
│   │   └── ...
│   ├── comments/
│   │   ├── prefix-1/
│   │   │   ├── comment-1.md
│   │   │   └── comment-2.md
│   │   └── ...
│   ├── events/
│   │   ├── prefix-1.log     # Audit trail per issue
│   │   └── ...
│   ├── metadata.yaml        # Config values (issue_prefix, etc.)
│   └── counters.yaml        # ID generation counters
```

### Issue File Format: `prefix-123.md`

**YAML Frontmatter** (short fields):
```yaml
---
title: "Issue title"
status: open                    # open | in_progress | blocked | closed
priority: 2                     # 0-4
issue_type: task                # bug | feature | task | epic | chore
assignee: "username"            # optional
external_ref: "gh-42"          # optional
labels:                         # optional
  - backend
  - priority-high
depends_on:                     # optional - dependencies FROM this issue
  - prefix-5: blocks           # this issue is blocked by prefix-5
  - prefix-10: related         # this issue is related to prefix-10
created_at: "2025-01-15T10:30:00Z"
updated_at: "2025-01-15T14:20:00Z"
closed_at: "2025-01-16T09:00:00Z"  # optional, only if closed
---
```

**Markdown Body** (long fields):
```markdown
# Description

This is the main description of the issue.
Multiple paragraphs are supported.

# Design

Design notes and architecture decisions.

# Acceptance Criteria

- [ ] Test passes
- [ ] Code is reviewed
- [ ] Documentation updated

# Notes

Additional implementation notes.
```

**Rules**:
- Only include frontmatter keys with non-empty values
- Only include markdown sections with non-empty content
- One blank line before each section header
- Description is the primary section (expected on most issues)
- Other long sections are optional

### Dependencies (Embedded in Issue Frontmatter)

Dependencies are stored directly in each issue's frontmatter using the `depends_on` field:

```yaml
depends_on:
  - prefix-5: blocks           # This issue depends on (is blocked by) prefix-5
  - prefix-10: related         # This issue is related to prefix-10
  - prefix-8: parent-child     # This issue is a child of prefix-8 (epic)
  - prefix-15: discovered-from # This issue was discovered while working on prefix-15
```

**Dependency semantics**:
- `blocks` - This issue is blocked by the target issue
- `related` - This issue is related to the target issue
- `parent-child` - This issue is a subtask of the target issue (epic)
- `discovered-from` - This issue was discovered while working on the target issue

**Finding dependents**: To find issues that depend on `prefix-5`, scan all issues and check their `depends_on` lists.

### Comments: `comments/prefix-123/comment-N.md`

```yaml
---
id: comment-1
author: username
created_at: "2025-01-15T10:30:00Z"
---

Comment text goes here.
```

### Events: `events/prefix-123.log`

JSONL format (one event per line):
```json
{"event":"created","actor":"user","timestamp":"2025-01-15T10:30:00Z"}
{"event":"updated","actor":"user","field":"status","old_value":"open","new_value":"in_progress","timestamp":"2025-01-15T14:20:00Z"}
{"event":"closed","actor":"user","timestamp":"2025-01-16T09:00:00Z"}
```

### Metadata: `metadata.yaml`

```yaml
issue_prefix: prefix
bd_version: "0.1.0"
```

### Counters: `counters.yaml`

```yaml
counters:
  prefix: 125
  comment: 450
```

## Locking Protocol

### Single-File Lock

**Acquisition**:
1. Check if `prefix-123.md` exists
2. Atomically rename: `prefix-123.md` → `prefix-123.md.lock.<pid>`
3. If rename fails, another process holds the lock → retry with backoff

**Modification**:
1. Create temp file: `prefix-123.md.tmp.<pid>`
2. Write changes to temp file
3. Fsync temp file

**Commit** (if `renameat2` available):
```go
renameat2(
    "prefix-123.md.tmp.<pid>" → "prefix-123.md",
    "prefix-123.md.lock.<pid>" → "prefix-123.md.trash.<pid>",
    RENAME_EXCHANGE
)
```

**Commit** (fallback to two renames):
```go
rename("prefix-123.md.tmp.<pid>" → "prefix-123.md")
rename("prefix-123.md.lock.<pid>" → "prefix-123.md.trash.<pid>")
```

**Cleanup**:
```go
os.Remove("prefix-123.md.trash.<pid>")
```

### Multi-File Lock Ordering

**Lock Acquisition Protocol**:
1. Sort issues by ID lexicographically: `[prefix-1, prefix-10, prefix-2]`
2. Attempt to acquire locks in order
3. If we encounter a lock held by a higher-priority PID (lower number):
   - Release all our locks
   - Wait random duration (exponential backoff)
   - Retry from step 1
4. If we encounter a lock held by lower-priority PID:
   - Wait with timeout
   - If timeout expires, break the lock (assume stale)

**Stale Lock Detection**:
- Lock file pattern: `prefix-123.md.lock.<pid>`
- Check if PID exists: `kill -0 <pid>` or `/proc/<pid>` on Linux
- If process doesn't exist: remove stale lock
- Grace period: 30 seconds before breaking locks of running processes

### Lock Cleanup on Startup

Scan for lock files and trash files:
```bash
.beads/markdown.db/issues/*.lock.*
.beads/markdown.db/issues/*.trash.*
.beads/markdown.db/issues/*.tmp.*
```

Remove any belonging to dead processes.

## Storage Interface Implementation

### Core Issue Operations

```go
type MarkdownStorage struct {
    rootDir    string           // .beads/markdown.db
    issuesDir  string           // .beads/markdown.db/issues
    pid        int              // current process ID
    locks      map[string]*lock // active locks held by this process
}

func (m *MarkdownStorage) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error
func (m *MarkdownStorage) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error
func (m *MarkdownStorage) GetIssue(ctx context.Context, id string) (*types.Issue, error)
func (m *MarkdownStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error
func (m *MarkdownStorage) UpdateIssueID(ctx context.Context, oldID, newID string, issue *types.Issue, actor string) error
func (m *MarkdownStorage) DeleteIssue(ctx context.Context, id string, actor string) error
func (m *MarkdownStorage) DeleteIssues(ctx context.Context, ids []string, actor string) error
```

### Query Operations

```go
func (m *MarkdownStorage) ListIssues(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error)
func (m *MarkdownStorage) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error)
```

**Implementation**:
- Scan `issues/` directory
- Parse each `.md` file
- Apply filters in memory
- For search: scan Description section for query string

**Note**: No index optimization - we accept O(n) scans for simplicity

### Dependency Operations

```go
func (m *MarkdownStorage) CreateDependency(ctx context.Context, from, to, depType string) error
func (m *MarkdownStorage) DeleteDependency(ctx context.Context, from, to string) error
func (m *MarkdownStorage) GetDependencies(ctx context.Context, issueID string) ([]*types.Dependency, error)
func (m *MarkdownStorage) GetDependents(ctx context.Context, issueID string) ([]*types.Dependency, error)
func (m *MarkdownStorage) RenameDependencyPrefix(ctx context.Context, oldPrefix, newPrefix string) error
```

**Implementation**:
- **CreateDependency**: Lock `from` issue, add to its `depends_on` list
- **DeleteDependency**: Lock `from` issue, remove from its `depends_on` list
- **GetDependencies**: Read `issueID` file, parse `depends_on` from frontmatter
- **GetDependents**: Scan all issues, filter where `depends_on` contains `issueID`
- **RenameDependencyPrefix**: Scan all issues, update any dependencies with old prefix

### Comment Operations

```go
func (m *MarkdownStorage) CreateComment(ctx context.Context, comment *types.Comment) error
func (m *MarkdownStorage) GetComments(ctx context.Context, issueID string) ([]*types.Comment, error)
func (m *MarkdownStorage) UpdateComment(ctx context.Context, id string, updates map[string]interface{}) error
func (m *MarkdownStorage) DeleteComment(ctx context.Context, id string) error
```

**Implementation**:
- **Phase 1-5**: Return `fmt.Errorf("comments not yet supported in markdown backend")`
- **Phase 6**: Implement comment support
  - Comments stored in `comments/<issue-id>/comment-N.md`
  - Lock the comment file for updates/deletes
  - Use comment counter for ID generation

### Event Operations

```go
func (m *MarkdownStorage) RecordEvent(ctx context.Context, event *types.Event) error
func (m *MarkdownStorage) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error)
```

**Implementation**:
- Append to `events/<issue-id>.log` (JSONL format)
- Use file append for atomic writes
- Lock file when reading to prevent partial line reads

### Config/Metadata Operations

```go
func (m *MarkdownStorage) GetConfig(ctx context.Context, key string) (string, error)
func (m *MarkdownStorage) SetConfig(ctx context.Context, key, value string) error
func (m *MarkdownStorage) GetMetadata(ctx context.Context, key string) (string, error)
func (m *MarkdownStorage) SetMetadata(ctx context.Context, key, value string) error
```

**Implementation**:
- Lock `metadata.yaml`
- Load, modify, write back

### Counter Operations

```go
func (m *MarkdownStorage) IncrementCounter(ctx context.Context, prefix string) (int, error)
func (m *MarkdownStorage) RenameCounterPrefix(ctx context.Context, oldPrefix, newPrefix string) error
func (m *MarkdownStorage) SyncAllCounters(ctx context.Context) error
```

**Implementation**:
- Lock `counters.yaml`
- Increment counter atomically
- For sync: scan all issues and update counters to max ID found

## Implementation Status (Updated 2025-10-26)

### Phase 1: Core Infrastructure ✅ COMPLETE
- ✅ Create `internal/storage/markdown/` package structure
- ✅ Implement file locking primitives (lock.go)
  - ✅ Single-file lock/unlock
  - ✅ Multi-file lock ordering
  - ✅ Stale lock detection
- ✅ Implement markdown parsing/serialization (storage.go)
  - ✅ YAML frontmatter parser
  - ✅ Markdown section parser
  - ✅ Issue → Markdown converter
  - ✅ Markdown → Issue converter
- ✅ Basic file operations (read/write with locks)

### Phase 2: Core Issue Operations ✅ COMPLETE
- ✅ Implement CreateIssue
- ✅ Implement CreateIssues (batch)
- ✅ Implement GetIssue
- ✅ Implement UpdateIssue
- ✅ Implement UpdateIssueID
- ✅ Implement DeleteIssue
- ✅ Implement DeleteIssues (batch)
- ✅ Implement ListIssues with filters
- ✅ Implement SearchIssues with full-text search
- ✅ Add counter management for ID generation
- ✅ Comprehensive unit tests

### Phase 3: Dependencies ✅ COMPLETE
- ✅ Implement CreateDependency (embedded in frontmatter)
- ✅ Implement DeleteDependency
- ✅ Implement GetDependencies
- ✅ Implement GetDependents (scan all issues)
- ✅ Implement GetDependencyRecords
- ✅ Implement GetAllDependencyRecords
- ✅ Implement RenameDependencyPrefix
- ✅ Add tests for dependencies

### Phase 4: Command Support 🚧 IN PROGRESS
**P0 - Blocking common workflows:**
- ❌ Implement CloseIssue (used by: close, epic, merge commands)
- ❌ Implement AddLabel/RemoveLabel (used by: label command)
- ❌ Implement AddDependency/RemoveDependency wrappers (used by: dep command)

**P1 - MCP server & advanced workflows:**
- ❌ Implement GetReadyWork (used by: ready command, MCP server)
- ❌ Implement GetBlockedIssues (used by: blocked command, MCP server)
- ❌ Implement GetStatistics (used by: stats command, MCP server)

**P2 - Nice to have:**
- ❌ Implement GetEpicsEligibleForClosure (used by: epic command)
- ❌ Implement GetIssuesByLabel (used by: label list command)
- ❌ Implement GetDependencyTree (used by: dep tree command)
- ❌ Implement DetectCycles (used by: dep cycles command)

**P3 - Low priority:**
- ❌ Implement GetAllConfig/DeleteConfig (used by: config commands)

### Phase 5: Integration & Migration ✅ COMPLETE
- ✅ Add backend configuration to config.yaml
- ✅ Implement backend factory pattern (init.go)
- ✅ Update CLI to support backend selection (--backend flag)
- ✅ Daemon skip for markdown backend
- ✅ Auto-flush/auto-import support
- ⚠️ Migration tools (deferred - use JSONL for migration)
  - Export from SQLite: `bd export -o issues.jsonl`
  - Init markdown: `bd init --backend markdown`
  - Import to markdown: `bd import -i issues.jsonl`

### Phase 6: Comments & Polish 🔮 FUTURE
- ⚠️ Comment operations (stubbed out - return "not yet supported")
- ⚠️ Event logging (stubbed out - no-op)
- ⚠️ Fsync configuration options (always enabled)
- ⚠️ Performance benchmarks vs SQLite
- ⚠️ Documentation and examples
- ⚠️ Update AGENTS.md with backend details

## Testing Strategy

### Unit Tests
- Lock acquisition/release under contention
- Markdown parsing (all field combinations)
- Markdown serialization (all field combinations)
- Stale lock cleanup
- Counter increment concurrency
- Dependency graph modifications

### Integration Tests
- Full CRUD workflow with markdown backend
- Switch backends mid-session
- Concurrent writers (spawn multiple processes)
- Lock ordering with artificial contention
- Migration roundtrip: SQLite → Markdown → SQLite

### Stress Tests
- 1000+ issues in repository
- 10+ concurrent writers
- Lock timeout and retry behavior
- Stale lock recovery after process kill

## Migration Strategy

### SQLite → Markdown

```go
func MigrateToMarkdown(sqliteDB, markdownDir string) error {
    src := sqlite.New(sqliteDB)
    dst := markdown.New(markdownDir)

    // Migrate issues
    issues, _ := src.ListIssues(ctx, types.IssueFilter{})
    for _, issue := range issues {
        dst.CreateIssue(ctx, issue, "migration")
    }

    // Migrate dependencies
    // Migrate comments
    // Migrate events
    // Migrate config/metadata

    return nil
}
```

### Markdown → SQLite

```go
func MigrateToSQLite(markdownDir, sqliteDB string) error {
    src := markdown.New(markdownDir)
    dst := sqlite.New(sqliteDB)

    // Reverse of above

    return nil
}
```

### Compatibility with JSONL

Both SQLite and Markdown backends should continue to support:
- Export to `issues.jsonl` (via `bd export`)
- Import from `issues.jsonl` (via `bd import`)

## Configuration

### `.beads/config.yaml`

```yaml
# Backend selection
backend: markdown  # sqlite | markdown

# Markdown backend settings
markdown:
  # Lock timeout for acquiring locks (default: 30s)
  lock-timeout: 30s

  # Enable fsync for every write (default: true)
  # Setting to false improves performance but risks data loss on crash
  fsync: true
```

### Backend Factory

```go
func NewStorage(configPath string) (storage.Storage, error) {
    cfg := loadConfig(configPath)

    switch cfg.Backend {
    case "sqlite":
        return sqlite.New(cfg.SQLite.Path)
    case "markdown":
        return markdown.New(cfg.Markdown.Path)
    default:
        return nil, fmt.Errorf("unknown backend: %s", cfg.Backend)
    }
}
```

## Open Questions

### 1. Performance Characteristics

**Question**: How will markdown backend perform compared to SQLite for common operations?

**Expected**:
- CreateIssue: Similar (single file write)
- GetIssue: Similar (single file read)
- ListIssues: Slower (must scan directory and parse all files)
- SearchIssues: Much slower (must scan all file contents)

**Mitigation**:
- Accept O(n) performance for queries
- For repos with >500 issues, recommend SQLite backend instead

### 2. Merge Conflict Resolution

**Question**: How do we handle merge conflicts in markdown files?

**Answer**: Git's default text merge should work well:
- YAML frontmatter conflicts are line-based (clear to resolve)
- Markdown section conflicts are paragraph-based
- Dependency conflicts are embedded in each issue (better merge resolution than central file)

Consider providing a `bd resolve-conflicts` command to help merge conflicting issue files.

### 3. Binary Data / Attachments

**Question**: How to handle issue attachments or embedded images?

**Answer**: Phase 1 won't support attachments. Future phases:
- Store attachments in `markdown.db/attachments/<issue-id>/`
- Reference in markdown with relative links: `![diagram](attachments/prefix-123/diagram.png)`

### 4. Atomicity Guarantees

**Question**: What atomicity guarantees do we provide?

**Answer**:
- Single issue: Atomic updates (via lock + atomic rename)
- Multiple issues: Not atomic across issues (but each issue update is atomic)
- Dependencies: Atomic (single file lock)
- Events: Append-only, eventual consistency (may see partial state during concurrent reads)

### 5. Windows Compatibility

**Question**: How do file locks work on Windows?

**Answer**:
- Atomic rename works on Windows (MoveFileEx with MOVEFILE_REPLACE_EXISTING)
- PID-based lock detection works (via tasklist or WMI)
- `renameat2` not available on Windows (use two-rename fallback)

**Testing**: Must test on Windows to ensure lock protocol works correctly.

## Success Criteria

1. **Functional**: All storage interface methods implemented and tested
2. **Concurrent-safe**: Passes stress tests with 10+ concurrent writers
3. **Git-friendly**: Merge conflicts are human-readable and resolvable
4. **Performance**: Acceptable for repositories with <500 issues (sub-second operations)
5. **Compatible**: Can migrate between SQLite and Markdown without data loss
6. **Documented**: Clear examples and migration guide

## Future Enhancements

- **Watch mode**: Use filesystem watch to detect external changes
- **Compression**: Optionally compress large issues
- **Sharding**: Split issues into subdirectories for very large repositories
- **Git integration**: Auto-commit on issue changes (optional feature)
- **Conflict resolution UI**: Interactive CLI tool for resolving merge conflicts

## References

- POSIX atomic rename: https://pubs.opengroup.org/onlinepubs/9699919799/functions/rename.html
- Linux renameat2: https://man7.org/linux/man-pages/man2/renameat2.2.html
- Git merge algorithms: https://git-scm.com/docs/merge-strategies
- YAML frontmatter spec: https://jekyllrb.com/docs/front-matter/
