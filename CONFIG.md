# Configuration System

bd has two complementary configuration systems:

1. **Tool-level configuration** (Viper): User preferences for tool behavior (flags, output format)
2. **Project-level configuration** (`bd config`): Integration data and project-specific settings

## Tool-Level Configuration (Viper)

### Overview

Tool preferences control how `bd` behaves globally or per-user. These are stored in config files or environment variables and managed by [Viper](https://github.com/spf13/viper).

**Configuration precedence** (highest to lowest):
1. Command-line flags (`--json`, `--no-daemon`, etc.)
2. Environment variables (`BD_JSON`, `BD_NO_DAEMON`, etc.)
3. Config file (`~/.config/bd/config.yaml` or `.beads/config.yaml`)
4. Defaults

### Config File Locations

Viper searches for `config.yaml` in these locations (in order):
1. `.beads/config.yaml` - Project-specific tool settings (version-controlled)
2. `~/.config/bd/config.yaml` - User-specific tool settings
3. `~/.beads/config.yaml` - Legacy user settings

### Supported Settings

Tool-level settings you can configure:

| Setting | Flag | Environment Variable | Default | Description |
|---------|------|---------------------|---------|-------------|
| `json` | `--json` | `BD_JSON` | `false` | Output in JSON format |
| `no-daemon` | `--no-daemon` | `BD_NO_DAEMON` | `false` | Force direct mode, bypass daemon |
| `no-auto-flush` | `--no-auto-flush` | `BD_NO_AUTO_FLUSH` | `false` | Disable auto JSONL export |
| `no-auto-import` | `--no-auto-import` | `BD_NO_AUTO_IMPORT` | `false` | Disable auto JSONL import |
| `db` | `--db` | `BD_DB` | (auto-discover) | Database path |
| `actor` | `--actor` | `BD_ACTOR` | `$USER` | Actor name for audit trail |
| `flush-debounce` | - | `BEADS_FLUSH_DEBOUNCE` | `5s` | Debounce time for auto-flush |
| `auto-start-daemon` | - | `BEADS_AUTO_START_DAEMON` | `true` | Auto-start daemon if not running |

### Example Config File

`~/.config/bd/config.yaml`:
```yaml
# Default to JSON output for scripting
json: true

# Disable daemon for single-user workflows
no-daemon: true

# Custom debounce for auto-flush (default 5s)
flush-debounce: 10s

# Auto-start daemon (default true)
auto-start-daemon: true
```

`.beads/config.yaml` (project-specific):
```yaml
# Project team prefers longer flush delay
flush-debounce: 15s
```

### Why Two Systems?

**Tool settings (Viper)** are user preferences:
- How should I see output? (`--json`)
- Should I use the daemon? (`--no-daemon`)
- How should the CLI behave?

**Project config (`bd config`)** is project data:
- What's our Jira URL?
- What are our Linear tokens?
- How do we map statuses?

This separation is correct: **tool settings are user-specific, project config is team-shared**.

Agents benefit from `bd config`'s structured CLI interface over manual YAML editing.

## Project-Level Configuration (`bd config`)

### Overview

Project configuration is:
- **Per-project**: Isolated to each `.beads/*.db` database
- **Version-control-friendly**: Stored in SQLite, queryable and scriptable
- **Machine-readable**: JSON output for automation
- **Namespace-based**: Organized by integration or purpose

## Commands

### Set Configuration

```bash
bd config set <key> <value>
bd config set --json <key> <value>  # JSON output
```

Examples:
```bash
bd config set jira.url "https://company.atlassian.net"
bd config set jira.project "PROJ"
bd config set jira.status_map.todo "open"
```

### Get Configuration

```bash
bd config get <key>
bd config get --json <key>  # JSON output
```

Examples:
```bash
bd config get jira.url
# Output: https://company.atlassian.net

bd config get --json jira.url
# Output: {"key":"jira.url","value":"https://company.atlassian.net"}
```

### List All Configuration

```bash
bd config list
bd config list --json  # JSON output
```

Example output:
```
Configuration:
  compact_tier1_days = 90
  compact_tier1_dep_levels = 2
  jira.project = PROJ
  jira.url = https://company.atlassian.net
```

JSON output:
```json
{
  "compact_tier1_days": "90",
  "compact_tier1_dep_levels": "2",
  "jira.project": "PROJ",
  "jira.url": "https://company.atlassian.net"
}
```

### Unset Configuration

```bash
bd config unset <key>
bd config unset --json <key>  # JSON output
```

Example:
```bash
bd config unset jira.url
```

## Namespace Convention

Configuration keys use dot-notation namespaces to organize settings:

### Core Namespaces

- `compact_*` - Compaction settings (see EXTENDING.md)
- `prefix` - Issue ID prefix (managed by `bd init`)
  - Note: For backward compatibility, `issue_prefix` is also supported

### Integration Namespaces

Use these namespaces for external integrations:

- `jira.*` - Jira integration settings
- `linear.*` - Linear integration settings
- `github.*` - GitHub integration settings
- `custom.*` - Custom integration settings

### Example: Jira Integration

```bash
# Configure Jira connection
bd config set jira.url "https://company.atlassian.net"
bd config set jira.project "PROJ"
bd config set jira.api_token "YOUR_TOKEN"

# Map bd statuses to Jira statuses
bd config set jira.status_map.open "To Do"
bd config set jira.status_map.in_progress "In Progress"
bd config set jira.status_map.closed "Done"

# Map bd issue types to Jira issue types
bd config set jira.type_map.bug "Bug"
bd config set jira.type_map.feature "Story"
bd config set jira.type_map.task "Task"
```

### Example: Linear Integration

```bash
# Configure Linear connection
bd config set linear.api_token "YOUR_TOKEN"
bd config set linear.team_id "team-123"

# Map statuses
bd config set linear.status_map.open "Backlog"
bd config set linear.status_map.in_progress "In Progress"
bd config set linear.status_map.closed "Done"
```

### Example: GitHub Integration

```bash
# Configure GitHub connection
bd config set github.org "myorg"
bd config set github.repo "myrepo"
bd config set github.token "YOUR_TOKEN"

# Map bd labels to GitHub labels
bd config set github.label_map.bug "bug"
bd config set github.label_map.feature "enhancement"
```

## Use in Scripts

Configuration is designed for scripting. Use `--json` for machine-readable output:

```bash
#!/bin/bash

# Get Jira URL
JIRA_URL=$(bd config get --json jira.url | jq -r '.value')

# Get all config and extract multiple values
bd config list --json | jq -r '.["jira.project"]'
```

Example Python script:
```python
import json
import subprocess

def get_config(key):
    result = subprocess.run(
        ["bd", "config", "get", "--json", key],
        capture_output=True,
        text=True
    )
    data = json.loads(result.stdout)
    return data["value"]

def list_config():
    result = subprocess.run(
        ["bd", "config", "list", "--json"],
        capture_output=True,
        text=True
    )
    return json.loads(result.stdout)

# Use in integration
jira_url = get_config("jira.url")
jira_project = get_config("jira.project")
```

## Best Practices

1. **Use namespaces**: Prefix keys with integration name (e.g., `jira.*`, `linear.*`)
2. **Hierarchical keys**: Use dots for structure (e.g., `jira.status_map.open`)
3. **Document your keys**: Add comments in integration scripts
4. **Security**: Store tokens in config, but add `.beads/*.db` to `.gitignore` (bd does this automatically)
5. **Per-project**: Configuration is project-specific, so each repo can have different settings

## Integration with bd Commands

Some bd commands automatically use configuration:

- `bd compact` uses `compact_tier1_days`, `compact_tier1_dep_levels`, etc.
- `bd init` sets `issue_prefix`

External integration scripts can read configuration to sync with Jira, Linear, GitHub, etc.

## See Also

- [README.md](README.md) - Main documentation
- [EXTENDING.md](EXTENDING.md) - Database schema and compaction config
- [examples/integrations/](examples/integrations/) - Integration examples
