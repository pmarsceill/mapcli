# MAP CLI

[![CI](https://github.com/pmarsceill/mapcli/actions/workflows/ci.yml/badge.svg)](https://github.com/pmarsceill/mapcli/actions/workflows/ci.yml)

A tool for spawning and managing multiple AI coding agents (Claude Code and OpenAI Codex) with a Docker-like architecture: thin CLI (`map`) + local daemon (`mapd`).

https://github.com/user-attachments/assets/1e0f02fe-fdbb-4cf7-bff4-a2161662b7a2

## Overview

MAP (Multi-Agent Platform) provides infrastructure for spawning and coordinating multiple AI coding agents. It supports both **Claude Code** and **OpenAI Codex** agents. The architecture separates concerns:

- **`map`** - Lightweight CLI for spawning agents and monitoring their status
- **`mapd`** - Daemon process that manages agent lifecycles and worktrees

## Quick Start

### Build

```bash
make build
```

This creates two binaries in `bin/`:
- `map` - CLI tool
- `mapd` - Daemon

### Run

1. **Start the daemon:**
   ```bash
   map up
   # Or run in foreground for debugging:
   map up -f
   ```

2. **Spawn agents:**
   ```bash
   # Spawn a Claude agent (default)
   map agent create

   # Spawn a Codex agent
   map agent create -a codex

   # Spawn with a prompt
   map agent create -p "Fix the bug in auth.go"

   # Spawn multiple Codex agents
   map agent create -n 3 -a codex
   ```

3. **Monitor agents:**
   ```bash
   # List spawned agents
   map agent list

   # Watch agent output in real-time
   map agent watch

   # List worktrees
   map worktree ls
   ```

4. **Stop the daemon:**
   ```bash
   map down
   ```

## CLI Commands

### Daemon Control

| Command | Description |
|---------|-------------|
| `map up [-f]` | Start the daemon (foreground with -f) |
| `map down [-f]` | Stop the daemon (force immediate shutdown with -f) |
| `map watch` | Stream real-time events from the daemon |

### Agent Management

| Command | Description |
|---------|-------------|
| `map agents` | List spawned agents (alias: `map ag`) |
| `map agent create [-a type]` | Spawn agents (claude or codex) |
| `map agent list` | List spawned agents (alias: `ls`, same as `map agents`) |
| `map agent kill <id>` | Terminate a spawned agent |
| `map agent kill --all` | Terminate all spawned agents |
| `map agent watch [id]` | Attach to agent's tmux session |
| `map agent watch -a` | Watch all agents in tiled tmux view |
| `map agent respawn <id>` | Restart agent in dead tmux pane |
| `map agent merge <id>` | Merge agent's worktree changes into current branch |
| `map agent merge <id> -k` | Merge agent's changes and kill the agent |

### Worktree Management

| Command | Description |
|---------|-------------|
| `map worktree ls` | List agent worktrees (alias: `list`) |
| `map worktree cleanup` | Remove orphaned worktrees |
| `map worktree cleanup --agent <id>` | Remove worktree for a specific agent |
| `map worktree cleanup --all` | Remove all agent worktrees |

### Task Management

| Command | Description |
|---------|-------------|
| `map task submit <description>` | Submit a new task for agent processing (alias: `map tasks`, `map t`) |
| `map task ls [-n limit]` | List all tasks with status (default limit: 20) |
| `map task show <id>` | Show detailed task information |
| `map task cancel <id>` | Cancel a pending or in-progress task |

## Spawning Agents

MAP can spawn Claude Code or OpenAI Codex agents as subprocesses, with each agent optionally isolated in its own git worktree for safe concurrent work.

### Agent Types

MAP supports two agent types via the `-a` flag:

| Type | CLI | Description |
|------|-----|-------------|
| `claude` | Claude Code | Anthropic's Claude Code CLI (default) |
| `codex` | OpenAI Codex | OpenAI's Codex CLI |

### Agent Naming

Each agent receives a unique, human-friendly name based on its type:

- **Claude agents**: French-style names (e.g., `jacques-bernard`, `marie-claire`, `philippe-martin`)
- **Codex agents**: California-style names (e.g., `chad-stevenson`, `bryce-anderson`, `tyler-johnson`)

Names are automatically generated and guaranteed unique within a session.

### Basic Usage

```bash
# Spawn a Claude agent (default)
map agent create

# Spawn a Codex agent
map agent create -a codex

# Spawn 3 Claude agents in parallel
map agent create -n 3

# Spawn 3 Codex agents in parallel
map agent create -n 3 -a codex

# Spawn with a specific prompt
map agent create -p "Fix the bug in auth.go"
map agent create -a codex -p "Implement the login feature"

# Spawn without worktree isolation (agents share working directory)
map agent create --no-worktree
```

### Agent Management

```bash
# List all spawned agents
map agent list

# Kill a specific agent
map agent kill claude-abc123

# Force kill (SIGKILL instead of SIGTERM)
map agent kill claude-abc123 --force

# Kill all agents
map agent kill --all

# Force kill all agents
map agent kill --all --force
```

### Worktree Management

When agents are spawned with worktree isolation (the default), each agent gets its own git worktree in `~/.mapd/worktrees/`. This allows multiple agents to work on the same repository concurrently without conflicts.

**Permission Bypass:** By default, agents are started with permission-bypassing flags to enable autonomous operation:
- Claude: `--dangerously-skip-permissions`
- Codex: `--dangerously-bypass-approvals-and-sandbox`

This is safe when using worktrees because each worktree is an isolated copy created by MAP. Use `--require-permissions` to restore standard permission prompts if needed.

```bash
# List all worktrees
map worktree ls

# Clean up orphaned worktrees (agents that have exited)
map worktree cleanup

# Clean up a specific agent's worktree
./bin/map worktree cleanup --agent <agent-id>

# Clean up all worktrees
map worktree cleanup --all
```

### Merging Agent Changes

When an agent completes work in its worktree, use `map agent merge` to bring those changes back to your main branch:

```bash
# Merge an agent's changes into your current branch
map agent merge <agent-id>

# Merge with a custom commit message for uncommitted changes
map agent merge <agent-id> -m "Agent completed feature X"

# Squash all agent commits into one
map agent merge <agent-id> --squash

# Stage changes without committing (for manual review)
map agent merge <agent-id> --no-commit

# Merge and kill the agent afterward
map agent merge <agent-id> -k
map agent merge <agent-id> --kill
```

The merge command will:
1. Commit any uncommitted changes in the agent's worktree (if any)
2. Merge those changes into your current branch
3. Optionally kill the agent after a successful merge (with `-k` flag)

## Task Management

MAP includes a task routing system for distributing work to agents.

### Task Lifecycle

Tasks follow this lifecycle: `PENDING → OFFERED → ACCEPTED → IN_PROGRESS → COMPLETED/FAILED/CANCELLED`

### Task Commands

```bash
# Submit a new task
map task submit "Fix the authentication bug in login.go"

# Submit with scope paths (limits where agent can work)
map task submit "Update API handlers" -p ./internal/api -p ./internal/handlers

# List all tasks
map task ls

# Show task details
map task show <task-id>

# Cancel a task
map task cancel <task-id>
```

## Event Streaming

Watch real-time events from the daemon:

```bash
# Stream all daemon events
map watch
```

Events include task lifecycle changes (created, offered, accepted, started, completed, failed, cancelled) and agent status updates.

### Agent Create Options

| Flag | Default | Description |
|------|---------|-------------|
| `-a, --agent-type` | `claude` | Agent type: `claude` or `codex` |
| `-n, --count` | `1` | Number of agents to spawn |
| `--branch` | current branch | Git branch for worktrees |
| `--worktree` | `true` | Use worktree isolation |
| `--no-worktree` | `false` | Skip worktree isolation |
| `--name` | agent type | Agent name prefix |
| `-p, --prompt` | none | Initial prompt to send to the agent |
| `--require-permissions` | `false` | Require permission prompts (by default, permissions are skipped for autonomous operation) |

## Architecture

```
┌─────────┐         ┌─────────────────────────────────────┐
│   CLI   │◄───────►│             Daemon (mapd)           │
│  (map)  │  gRPC   │  ┌───────────┐  ┌────────────────┐  │
└─────────┘         │  │ Worktree  │  │    Process     │  │
                    │  │ Manager   │  │    Manager     │  │
                    │  └─────┬─────┘  └───────┬────────┘  │
                    └────────┼────────────────┼───────────┘
                             │                │
              ┌──────────────┘                └──────────────┐
              ▼                                              ▼
    ┌───────────────────┐                      ┌─────────────────────┐
    │ ~/.mapd/worktrees │                      │ claude/codex CLI    │
    │   claude-abc123/  │◄─────── cwd ─────────│   (tmux session)    │
    │   codex-def456/   │                      └─────────────────────┘
    └───────────────────┘
```

## Project Structure

```
mapcli/
├── cmd/
│   ├── map/           # CLI binary
│   └── mapd/          # Daemon binary
├── internal/
│   ├── daemon/        # Daemon core (server, store, process management)
│   ├── cli/           # CLI commands
│   └── client/        # Shared gRPC client
├── proto/map/v1/      # Protocol definitions & generated code
├── go.mod
├── Makefile
└── README.md
```

## Configuration

### Global CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-s, --socket` | `/tmp/mapd.sock` | Unix socket path for daemon communication |

### Daemon (`map up`)

| Flag | Default | Description |
|------|---------|-------------|
| `-f, --foreground` | `false` | Run daemon in foreground |
| `-d, --data-dir` | `~/.mapd` | Data directory for SQLite |

## System Requirements

MAP requires the following tools to be installed and available in your PATH:

| Dependency | Required For | Version |
|------------|--------------|---------|
| **git** | Worktree isolation | 2.15+ (worktree support) |
| **tmux** | Agent session management | Any recent version |
| **claude** | Claude Code agents | Latest (optional if only using Codex) |
| **codex** | OpenAI Codex agents | Latest (optional if only using Claude) |

At least one of `claude` or `codex` must be installed depending on which agent type you want to use.

### Installing Dependencies

**macOS (Homebrew):**
```bash
brew install git tmux
# Claude CLI: https://docs.anthropic.com/en/docs/claude-code
# Codex CLI: https://github.com/openai/codex
```

**Ubuntu/Debian:**
```bash
sudo apt update && sudo apt install git tmux
# Claude CLI: https://docs.anthropic.com/en/docs/claude-code
# Codex CLI: https://github.com/openai/codex
```

**Fedora/RHEL:**
```bash
sudo dnf install git tmux
# Claude CLI: https://docs.anthropic.com/en/docs/claude-code
# Codex CLI: https://github.com/openai/codex
```

**Arch Linux:**
```bash
sudo pacman -S git tmux
# Claude CLI: https://docs.anthropic.com/en/docs/claude-code
# Codex CLI: https://github.com/openai/codex
```

## Development

### Prerequisites

- Go 1.24+
- All system requirements above
- (Optional) protoc with go plugins for regenerating proto files

### Regenerate Proto Files

If you have protoc installed:

```bash
make install-tools  # Install protoc-gen-go plugins
make generate       # Regenerate proto files
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build all binaries |
| `make all` | Generate protos and build |
| `make generate` | Regenerate protobuf files |
| `make test` | Run all tests |
| `make clean` | Remove build artifacts |
| `make rebuild` | Clean, generate, and build |
| `make deps` | Download and tidy dependencies |
| `make install-tools` | Install protoc plugins |
| `make dev` | Hot reload development mode (requires Air) |

### Linting

Run the linter before submitting changes:

```bash
golangci-lint run --timeout=5m
```

Common issues to watch for:
- **errcheck**: Always handle error return values (use `_ = fn()` for intentionally ignored errors)
- **staticcheck**: Avoid deprecated functions
- For deferred close operations: `defer func() { _ = x.Close() }()`

### Development Helpers

Run components directly without building:

```bash
make run-daemon      # Run daemon in current shell
make run-cli ARGS="agent list"  # Run CLI with arguments
```

## Dependencies

Core runtime dependencies:

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `google.golang.org/grpc` | gRPC communication |
| `google.golang.org/protobuf` | Protocol buffer support |
| `modernc.org/sqlite` | SQLite database (pure Go) |
| `github.com/google/uuid` | UUID generation |
