# Markdown Backend Design Document

## Executive Summary

The markdown backend implements an alternative file-based storage system for beads that replaces SQLite with individual markdown files for each issue. This design choice prioritizes **human readability** and **git-friendly merge conflict resolution** over raw performance. Each issue becomes a self-contained `.md` file with YAML frontmatter for structured data and markdown sections for long-form content.

**Key Benefits:**
- **Git-Friendly**: Each issue is a separate file, making merge conflicts easier to resolve than SQLite database conflicts
- **Human-Readable**: Issues can be read, edited, and understood in any text editor
- **AI-Friendly**: Claude and other AI agents can easily read and understand the markdown format
- **Transparent**: No hidden state - everything is visible in the filesystem
- **Version Control**: Direct git history of individual issues

**Trade-offs:**
- **Performance**: O(n) scans for queries vs O(log n) SQLite indexes
- **Atomicity**: No multi-issue transactions (each issue update is atomic, but not across issues)
- **Recommended Use**: Projects with <500 issues for best performance

## Architecture Overview

### Storage Interface

The markdown backend implements the same `storage.Storage` interface as the SQLite backend, ensuring **drop-in compatibility** with zero changes to command code:

```go
type Storage interface {
    CreateIssue(ctx context.Context, issue *Issue, actor string) error
    GetIssue(ctx context.Context, id string) (*Issue, error)
    UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error
    DeleteIssue(ctx context.Context, id string, actor string) error
    ListIssues(ctx context.Context, filter IssueFilter) ([]*Issue, error)
    // ... and 40+ other methods
}
```

Both SQLite and markdown backends implement this interface completely, allowing runtime selection via configuration.

### Directory Structure

```
.beads/
├── config.yaml              # Contains: backend: markdown
├── markdown_db/             # Root directory for markdown storage
│   ├── issues/
│   │   ├── bd-1.md
│   │   ├── bd-2.md
│   │   ├── bd-3.md.lock.12345    # Locked file (PID 12345)
│   │   └── bd-4.md.tmp.12345     # Temp file during write
│   ├── comments/            # Reserved for future comment support
│   ├── events/
│   │   ├── bd-1.jsonl       # Event log per issue
│   │   └── bd-2.jsonl
│   ├── config.yaml          # Backend-specific config
│   └── metadata.yaml        # Backend-specific metadata
```

**Design Notes:**
- **Single directory for issues**: All issues live in `issues/` (no sharding by prefix)
- **Event logs are JSONL**: Simple append-only logs for audit trail
- **Comments deferred**: Stubbed out for now, reserved directory structure exists
- **Lock files in same directory**: Simplified cleanup and detection

## File Format Specification

### Issue File: `bd-123.md`

Issues use **YAML frontmatter** for structured fields and **markdown sections** for long-form content.

```markdown
---
title: "Add markdown backend for human-readable storage"
status: in_progress
priority: 2
issue_type: feature
assignee: claude
labels:
  - backend
  - storage
depends_on:
  bd-5: blocks           # This issue is blocked by bd-5
  bd-10: related         # This issue is related to bd-10
created_at: "2025-01-15T10:30:00Z"
updated_at: "2025-01-15T14:20:00Z"
---

# Description

Implement a markdown-based storage backend that uses individual .md files
for each issue instead of SQLite. This makes issues human-readable and
easier to merge in git.

# Design

The implementation uses a lock-based protocol for concurrency safety:
1. Lock the issue file by renaming to `.lock.<pid>`
2. Write changes to `.tmp.<pid>` file
3. Atomically swap temp and lock files
4. Remove lock file

# Acceptance Criteria

- [ ] All storage interface methods implemented
- [ ] Tests pass with markdown backend
- [ ] Performance acceptable for <500 issues
- [ ] Migration path from SQLite documented

# Notes

Consider using filesystem watch in the future for real-time updates.
```

**Format Rules:**
1. **Frontmatter**: Only include keys with non-empty values (omit optional fields)
2. **Sections**: Only include sections with non-empty content
3. **Dependencies**: Embedded in frontmatter as `depends_on` map (issueID → depType)
4. **Timestamps**: RFC3339 format with timezone
5. **Labels**: YAML array (empty array omitted)

### Event Log: `events/bd-123.jsonl`

Events are stored as **JSONL** (JSON Lines) - one event per line:

```jsonl
{"event":"created","actor":"claude","timestamp":"2025-01-15T10:30:00Z"}
{"event":"updated","actor":"claude","field":"status","old_value":"open","new_value":"in_progress","timestamp":"2025-01-15T14:20:00Z"}
{"event":"commented","actor":"alice","timestamp":"2025-01-15T16:00:00Z"}
```

**Design Choice**: JSONL is simple, append-only, and parseable line-by-line. No locking required for appends.

## Concurrency & Locking Protocol

### Problem Statement

Multiple processes (or `bd` invocations) may attempt to modify the same issue simultaneously. Without locking, we risk:
- **Lost updates**: Process B overwrites Process A's changes
- **Partial reads**: Process B reads half-written data from Process A
- **File corruption**: Concurrent writes destroy file structure

### File-Based Locking Solution

The markdown backend uses a **PID-based lock protocol** inspired by POSIX lock files:

#### Lock Acquisition

```
1. Check if bd-123.md exists
2. Atomically rename: bd-123.md → bd-123.md.lock.<pid>
   - If rename fails → another process holds lock → retry with exponential backoff
   - If successful → we now hold the lock
3. Original file is now "locked" and held at .lock.<pid> path
```

**Key Insight**: Renaming is atomic on POSIX systems. Only one process can successfully rename the file.

#### Modification

```
4. Read issue content from bd-123.md.lock.<pid>
5. Apply updates to in-memory Issue struct
6. Create temp file: bd-123.md.tmp.<pid>
7. Write updated markdown to temp file
8. Fsync temp file to disk
```

#### Commit (Atomic Swap)

**Option A** (Linux with `renameat2` support):
```go
renameat2(
    "bd-123.md.tmp.<pid>"   → "bd-123.md",
    "bd-123.md.lock.<pid>"  → "bd-123.md.trash.<pid>",
    RENAME_EXCHANGE  // Atomic simultaneous swap
)
```

**Option B** (Fallback for macOS/Windows):
```go
rename("bd-123.md.tmp.<pid>" → "bd-123.md")
rename("bd-123.md.lock.<pid>" → "bd-123.md.trash.<pid>")
// Two renames: small window where both files exist, but safe
```

#### Cleanup

```
9. Remove trash file: bd-123.md.trash.<pid>
10. Clear lock from in-memory map
```

### Stale Lock Detection

**Problem**: If a process crashes while holding a lock, the `.lock.<pid>` file remains, blocking future access.

**Solution**: Check if lock holder PID is still alive:

```go
func isProcessAlive(pid int) bool {
    // Unix: send signal 0 (no-op signal that checks existence)
    err := syscall.Kill(pid, 0)
    return err == nil

    // Windows: check task list or /proc/<pid>
}
```

**Grace Period**: Wait 30 seconds before breaking locks of running processes (in case of legitimate long operations).

### Multi-File Lock Ordering

For operations that modify multiple issues (e.g., rename prefix, batch updates), use **lock ordering** to prevent deadlocks:

```go
1. Sort issue IDs lexicographically: [bd-1, bd-10, bd-2]
2. Acquire locks in order
3. If lock held by higher-priority PID (lower PID number):
   - Release all our locks
   - Wait random duration (exponential backoff)
   - Retry from step 1
4. Perform modifications
5. Release locks in reverse order
```

**Priority Rule**: Lower PID wins. This ensures deterministic resolution when two processes compete.

## Configuration Integration

### Global Config: `.beads/config.yaml`

The global config file determines which backend to use:

```yaml
backend: markdown        # sqlite | markdown
issue-prefix: bd         # Canonical source of truth for issue IDs
no-json: false          # Maintain backward compatibility with JSONL
```

**Key Insight**: `issue-prefix` is now a **global config setting**, not stored per-backend. This eliminated the `nodb_prefix.txt` hack and provides a single source of truth.

### Backend Factory Pattern

Backend selection happens at initialization time in `cmd/bd/main.go`:

```go
func initStorage() (storage.Storage, error) {
    backend := config.GetString("backend")  // From config.yaml or --backend flag

    switch backend {
    case "sqlite":
        dbPath := config.GetString("db")
        return sqlite.New(dbPath)

    case "markdown":
        markdownPath := filepath.Join(".beads", "markdown_db")
        return markdown.New(markdownPath)

    default:
        return nil, fmt.Errorf("unknown backend: %s", backend)
    }
}
```

**Precedence Order**:
1. Command-line flag: `--backend markdown`
2. Environment variable: `BD_BACKEND=markdown`
3. Config file: `backend: markdown` in `.beads/config.yaml`
4. Default: `sqlite`

### Viper Integration

The config system uses Viper for unified configuration management:

```go
// internal/config/config.go

v.SetEnvPrefix("BD")
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
v.AutomaticEnv()

v.SetDefault("backend", "sqlite")
v.SetDefault("issue-prefix", "")
v.SetDefault("no-json", false)
```

**Backward Compatibility**: Old config keys are automatically migrated:
- `json` → `json-output`
- `issue_prefix` → `issue-prefix`

## Implementation Details

### Core Operations

#### CreateIssue

```go
func (m *MarkdownStorage) CreateIssue(ctx context.Context, issue *Issue, actor string) error {
    // 1. Generate ID if not set
    if issue.ID == "" {
        prefix := config.GetString("issue-prefix")
        nextID, _ := m.IncrementCounter(ctx, prefix)
        issue.ID = fmt.Sprintf("%s-%d", prefix, nextID)
    }

    // 2. Set timestamps
    issue.CreatedAt = time.Now()
    issue.UpdatedAt = time.Now()

    // 3. Convert to markdown
    data, _ := issueToMarkdown(issue)

    // 4. Write to temp file
    tempPath := fmt.Sprintf("%s/issues/%s.md.tmp.%d", m.rootDir, issue.ID, m.pid)
    os.WriteFile(tempPath, data, 0640)

    // 5. Atomically rename to final location
    issuePath := fmt.Sprintf("%s/issues/%s.md", m.rootDir, issue.ID)
    os.Rename(tempPath, issuePath)

    return nil
}
```

**Key Points**:
- No locking needed (new file, no conflicts)
- Temp file prevents partial writes from being visible
- Atomic rename ensures all-or-nothing semantics

#### UpdateIssue

```go
func (m *MarkdownStorage) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
    // 1. Lock the issue
    lock, _ := m.lockFile(id)  // Renames bd-1.md → bd-1.md.lock.<pid>
    defer m.unlockFile(lock)

    // 2. Read from lock file
    data, _ := os.ReadFile(lock.lockPath)
    issue, _ := markdownToIssue(id, data)

    // 3. Apply updates
    applyUpdates(issue, updates)
    issue.UpdatedAt = time.Now()

    // 4. Write to temp file
    updatedData, _ := issueToMarkdown(issue)
    tempPath := fmt.Sprintf("%s/issues/%s.md.tmp.%d", m.rootDir, id, m.pid)
    os.WriteFile(tempPath, updatedData, 0640)

    // 5. Commit (atomic swap)
    m.commitFile(lock, tempPath)  // Swaps temp→actual, lock→trash

    return nil
}
```

**Key Points**:
- Lock prevents concurrent modifications
- Read-modify-write cycle is atomic from other processes' perspective
- Commit is atomic (single rename or RENAME_EXCHANGE)

#### IncrementCounter (ID Generation)

```go
func (m *MarkdownStorage) IncrementCounter(ctx context.Context, prefix string) (int, error) {
    m.locksMu.Lock()
    defer m.locksMu.Unlock()

    // Scan all .md files in issues/ directory
    maxID := 0
    entries, _ := os.ReadDir(m.issuesDir)
    for _, entry := range entries {
        if strings.HasSuffix(entry.Name(), ".md") {
            // Parse: bd-123.md → prefix="bd", number=123
            issueID := entry.Name()[:len(entry.Name())-3]
            parts := strings.Split(issueID, "-")
            if len(parts) >= 2 && parts[0] == prefix {
                num, _ := strconv.Atoi(parts[len(parts)-1])
                if num > maxID {
                    maxID = num
                }
            }
        }
    }

    return maxID + 1, nil
}
```

**Design Decision**: No separate `counters.yaml` file. The source of truth is the filenames themselves. This eliminates the risk of counter desync.

**Performance**: O(n) scan of files, but acceptable for <500 issues. Protected by mutex to prevent race conditions.

### Dependency Management

Dependencies are **embedded in each issue's frontmatter** rather than stored in a separate table:

```yaml
depends_on:
  bd-5: blocks
  bd-10: related
  bd-3: parent-child
```

**Operations**:

- **CreateDependency**: Lock `from` issue → add to its `depends_on` map → commit
- **DeleteDependency**: Lock `from` issue → remove from its `depends_on` map → commit
- **GetDependencies**: Read issue → parse `depends_on` → fetch each dependency issue
- **GetDependents**: Scan all issues → filter where `depends_on` contains target ID

**Trade-off**: GetDependents is O(n) (must scan all files), but avoids central dependency table that could become a merge conflict hotspot.

### Parsing & Serialization

#### issueToMarkdown

```go
func issueToMarkdown(issue *Issue) ([]byte, error) {
    var buf bytes.Buffer

    // Write YAML frontmatter
    buf.WriteString("---\n")
    fm := Frontmatter{
        Title:     issue.Title,
        Status:    string(issue.Status),
        Priority:  issue.Priority,
        // ... other fields
    }
    yaml.NewEncoder(&buf).Encode(&fm)
    buf.WriteString("---\n")

    // Write markdown sections (only non-empty)
    if issue.Description != "" {
        buf.WriteString("\n# Description\n\n")
        buf.WriteString(issue.Description)
        buf.WriteString("\n")
    }

    // ... other sections

    return buf.Bytes(), nil
}
```

**Key Points**:
- Only include non-empty frontmatter fields (clean YAML)
- Only include non-empty sections (clean markdown)
- Consistent formatting (2-space indent for YAML)

#### markdownToIssue

```go
func markdownToIssue(issueID string, data []byte) (*Issue, error) {
    // Split by "---" delimiter
    parts := bytes.SplitN(data, []byte("---\n"), 3)
    if len(parts) < 3 {
        return nil, fmt.Errorf("invalid markdown format")
    }

    // Parse frontmatter (YAML)
    var fm Frontmatter
    yaml.Unmarshal(parts[1], &fm)

    // Parse body sections (markdown)
    sections := parseSections(string(parts[2]))

    // Build Issue struct
    issue := &Issue{
        ID:          issueID,
        Title:       fm.Title,
        Description: sections.Description,
        Design:      sections.Design,
        // ... other fields
    }

    return issue, nil
}
```

**Key Points**:
- Robust parsing (handles missing sections gracefully)
- Flexible timestamp parsing (tries multiple formats)
- Dependencies converted from map to slice

## Integration with MCP Server

The MCP server (Model Context Protocol) provides AI agents access to beads. It needs to support **both SQLite and markdown backends** transparently.

### Database Discovery

The MCP server's `set_context` tool now discovers the database by reading `.beads/config.yaml`:

```python
def _find_beads_db(workspace_root: str) -> str | None:
    beads_dir = os.path.join(workspace_root, ".beads")
    config_path = os.path.join(beads_dir, "config.yaml")

    # Read config to determine backend
    backend = "sqlite"  # Default
    if os.path.exists(config_path):
        with open(config_path) as f:
            config = yaml.safe_load(f)
            backend = config.get("backend", "sqlite")

    # Find database based on backend
    if backend == "markdown":
        markdown_db = os.path.join(beads_dir, "markdown_db")
        if os.path.isdir(markdown_db):
            return markdown_db
    else:
        # SQLite: find any .db file
        db_files = glob.glob(os.path.join(beads_dir, "*.db"))
        if db_files:
            return db_files[0]

    return None
```

**Rationale**: MCP server sets `BEADS_DB` environment variable, which the Go CLI reads to locate the database. For markdown backend, this points to the `markdown_db` directory instead of a `.db` file.

### PyYAML Dependency

Added to `pyproject.toml`:

```toml
dependencies = [
    "fastmcp==2.12.4",
    "pydantic==2.12.0",
    "pydantic-settings==2.11.0",
    "pyyaml>=6.0",          # New: for config.yaml parsing
]
```

## Known Issues & Future Work

### Issue 1: `--no-db` Mode Still Uses `nodb_prefix.txt`

**Problem**: The `--no-db` (JSONL-only) mode predates the markdown backend and still uses a legacy `nodb_prefix.txt` file to store the issue prefix.

**Status**: Not yet fixed in this commit.

**Proposed Fix**:
1. Remove `nodb_prefix.txt` completely
2. Use `config.GetString("issue-prefix")` from `.beads/config.yaml`
3. Update `cmd/bd/nodb.go` to read/write from global config

**Location**: `cmd/bd/nodb.go:readNoDBPrefix()` and `writeNoDBPrefix()`

### Issue 2: Config.yaml Changes Not Separated into Feature Branch

**Problem**: The transition to using `.beads/config.yaml` as the canonical source of truth for `issue-prefix` is mixed into the markdown backend work. Ideally, this should be a separate high-priority feature branch to merge first.

**Rationale**: Making `issue-prefix` a global config setting benefits **all backends** (SQLite, markdown, no-db). It should be merged independently before the markdown backend.

**Proposed Approach**:
1. Create `feature-config-canonical-prefix` branch
2. Extract config.yaml changes:
   - Add `issue-prefix` to global config
   - Remove backend-specific prefix storage
   - Update all backends to read from global config
3. Merge this branch first
4. Rebase markdown backend on top

### Issue 3: DRY Violations in Issue ID Parsing

**Problem**: Multiple files have duplicate utility functions for parsing issue IDs:

- `cmd/bd/rename_prefix.go`: `extractIssuePrefix()`, `extractIssueNumber()`
- `cmd/bd/import_phases.go`: `extractPrefix()`

All use identical logic: `strings.SplitN(issueID, "-", 2)`.

**Proposed Fix**:
1. Create `internal/utils/issue_id.go` with:
   ```go
   func ExtractIssuePrefix(issueID string) string {
       parts := strings.SplitN(issueID, "-", 2)
       if len(parts) < 2 {
           return ""
       }
       return parts[0]
   }

   func ExtractIssueNumber(issueID string) int {
       parts := strings.SplitN(issueID, "-", 2)
       if len(parts) < 2 {
           return 0
       }
       num, _ := strconv.Atoi(parts[1])
       return num
   }
   ```
2. Replace all duplicate implementations with calls to shared functions

### Issue 4: Comments Not Implemented

**Status**: Stubbed out. All comment methods return `fmt.Errorf("comments not yet supported in markdown backend")`.

**Proposed Design** (for future):
```
markdown_db/
├── comments/
│   ├── bd-1/
│   │   ├── comment-1.md
│   │   ├── comment-2.md
│   │   └── ...
```

Each comment is a markdown file with YAML frontmatter:
```yaml
---
id: comment-1
author: alice
created_at: "2025-01-15T10:30:00Z"
---

Comment text goes here.
```

### Issue 5: Daemon Mode Disabled for Markdown Backend

**Problem**: The daemon mode (auto-flush, auto-import) requires SQLite-specific functionality.

**Current Solution**: Daemon mode is skipped when using markdown backend:

```go
if backend == "markdown" {
    // Skip daemon for markdown backend
    return nil
}
```

**Future Work**: Consider implementing a markdown-aware daemon that:
- Watches for filesystem changes (`fsnotify`)
- Auto-commits to git on issue changes (optional feature)
- Provides real-time notifications to CLI clients

## Performance Characteristics

### Benchmark Expectations

| Operation | SQLite | Markdown | Notes |
|-----------|--------|----------|-------|
| CreateIssue | O(1) + lock | O(1) + lock | Similar performance |
| GetIssue | O(1) | O(1) | Similar performance |
| UpdateIssue | O(1) + lock | O(1) + lock | Similar performance |
| DeleteIssue | O(1) + lock | O(1) + lock | Similar performance |
| ListIssues | O(n) with index | O(n) scan | Markdown slower |
| SearchIssues | O(n) with FTS | O(n) scan | Much slower for markdown |
| GetDependents | O(n) join | O(n) scan | Similar performance |
| IncrementCounter | O(1) | O(n) scan | Markdown slower |

**Recommendations**:
- **<100 issues**: Markdown and SQLite perform similarly
- **100-500 issues**: Markdown acceptable, noticeable lag on `bd list` and `bd ready`
- **>500 issues**: Use SQLite for better performance

### Scalability Limits

**Filesystem Limits**:
- Most filesystems handle 10,000+ files per directory efficiently
- Modern filesystems (ext4, XFS, APFS) optimize directory lookups

**Git Performance**:
- Git handles thousands of files well
- Merge conflicts scale linearly with number of changed issues
- For very large projects (>1000 issues), consider sparse checkout

## Migration Strategy

### SQLite → Markdown

```bash
# Step 1: Export from SQLite
bd export -o issues.jsonl

# Step 2: Initialize markdown backend
bd init --backend markdown

# Step 3: Import to markdown
bd import -i issues.jsonl
```

**Notes**:
- Dependencies are preserved (encoded in JSONL)
- Events are lost (not included in JSONL export)
- Comments are lost (not yet supported)

### Markdown → SQLite

```bash
# Step 1: Export from markdown
bd export -o issues.jsonl

# Step 2: Initialize SQLite backend (or switch config)
bd init --backend sqlite

# Step 3: Import to SQLite
bd import -i issues.jsonl
```

**Notes**:
- Same limitations as above
- Events can be preserved if we enhance export/import

## Testing Strategy

### Unit Tests (`internal/storage/markdown/storage_test.go`)

- Lock acquisition/release
- Stale lock detection
- Markdown parsing (all field combinations)
- Markdown serialization (all field combinations)
- Counter increment concurrency
- Dependency operations

### Integration Tests

- Full CRUD workflow with markdown backend
- Switch backends mid-session (export → init → import)
- Concurrent writers (spawn multiple `bd` processes)
- Lock ordering with artificial contention

### Stress Tests

- 1000+ issues in repository
- 10+ concurrent writers
- Lock timeout and retry behavior
- Stale lock recovery after process kill

## Success Criteria

1. **Functional**: All storage interface methods implemented and tested
2. **Concurrent-safe**: Passes stress tests with 10+ concurrent writers
3. **Git-friendly**: Merge conflicts are human-readable and resolvable
4. **Performance**: Acceptable for repositories with <500 issues (sub-second operations)
5. **Compatible**: Can migrate between SQLite and Markdown without data loss
6. **Drop-in replacement**: No changes required to command code

## Appendix: File Format Examples

### Minimal Issue

```markdown
---
title: "Fix typo in README"
status: open
priority: 3
issue_type: task
created_at: "2025-01-15T10:30:00Z"
updated_at: "2025-01-15T10:30:00Z"
---

# Description

Change "teh" to "the" in README.md line 42.
```

### Complex Issue

```markdown
---
title: "Implement markdown backend for human-readable storage"
status: in_progress
priority: 1
issue_type: feature
assignee: claude
external_ref: "gh-42"
labels:
  - backend
  - storage
  - p1
depends_on:
  bd-5: blocks
  bd-10: related
  bd-3: parent-child
created_at: "2025-01-10T09:00:00Z"
updated_at: "2025-01-15T14:20:00Z"
---

# Description

Implement a markdown-based storage backend that uses individual .md files
for each issue instead of SQLite. This makes issues human-readable and
easier to merge in git.

The markdown format uses YAML frontmatter for structured data and markdown
sections for long-form content like descriptions and design notes.

# Design

The implementation consists of three main components:

1. **File locking protocol**: Uses PID-based lock files to prevent concurrent
   modifications. Lock acquisition is atomic via filesystem rename.

2. **Markdown parser**: Converts between Issue structs and markdown format.
   Handles YAML frontmatter and markdown sections.

3. **Counter management**: Derives next issue ID by scanning existing files
   to find maximum ID. No separate counter state.

See MARKDOWN_BACKEND_PLAN.md for full architectural details.

# Acceptance Criteria

- [ ] All storage interface methods implemented
- [ ] Unit tests pass
- [ ] Integration tests with concurrent writers pass
- [ ] Performance acceptable for <500 issues
- [ ] Migration path from SQLite documented
- [ ] MCP server supports markdown backend

# Notes

Future enhancements:
- Filesystem watch for real-time updates
- Auto-commit to git on issue changes
- Comment support
- Attachment support
```

## Conclusion

The markdown backend represents a significant architectural addition to beads, providing a human-readable, git-friendly alternative to SQLite storage. While it trades some performance for transparency and mergeability, it aligns well with beads' philosophy of being AI-agent-friendly and version-control-first.

The implementation is **complete and functional** for all core operations, with comments and events supported (events are implemented, comments are stubbed). The known issues (nodb_prefix.txt, config.yaml separation, DRY violations) are organizational and do not affect the functionality of the markdown backend itself.

**Recommendation**: Merge this feature branch after addressing the config.yaml separation issue, then tackle the remaining cleanup tasks in follow-up PRs.
