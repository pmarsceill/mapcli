# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make build          # Build both binaries (map CLI and mapd daemon) to bin/
make test           # Run tests with -v -race flags
make generate       # Regenerate protobuf files (requires protoc)
make dev            # Hot reload development mode (requires Air)
make rebuild        # Clean, generate, and build
make install-tools  # Install protoc-gen-go plugins
```

Run a single test:
```bash
go test -v -race ./internal/daemon -run TestStoreName
```

## Linting

**Important:** Before submitting changes, always run the linter and fix any issues:

```bash
golangci-lint run --timeout=5m
```

Common lint issues to watch for:
- **errcheck:** Always handle or explicitly ignore error return values (use `_ = fn()` for intentionally ignored errors)
- **staticcheck:** Avoid deprecated functions; check for modern alternatives
- For deferred close operations where errors can be ignored: `defer func() { _ = x.Close() }()`

## Architecture

MAP uses a Docker-like CLI + daemon architecture for managing AI coding agents (Claude Code and OpenAI Codex).

```
CLI (map) ──gRPC──► Daemon (mapd) ──► Agent processes (tmux sessions)
                        │
                        ├── Store (SQLite) - tasks, events, spawned_agents
                        ├── ProcessManager - spawns agents in tmux
                        ├── WorktreeManager - git worktree isolation
                        └── TaskRouter - task distribution
```

**Communication:** gRPC over Unix socket (`/tmp/mapd.sock`)

**Data storage:** SQLite at `~/.mapd/map.db`

**Agent isolation:** Each agent runs in its own git worktree at `~/.mapd/worktrees/{agentID}/`

## Code Structure

- `cmd/map/` - CLI entry point, routes to `internal/cli`
- `cmd/mapd/` - Daemon entry point, routes to `internal/daemon`
- `internal/cli/` - Cobra command implementations
- `internal/daemon/` - Core daemon logic:
  - `server.go` - gRPC server, event broadcasting
  - `store.go` - SQLite persistence layer
  - `process.go` - Agent spawning via tmux
  - `worktree.go` - Git worktree operations
  - `task.go` - Task routing logic
- `internal/client/` - gRPC client wrapper
- `proto/map/v1/` - Protobuf definitions and generated code

## Key Patterns

**Task lifecycle:** PENDING → OFFERED → ACCEPTED → IN_PROGRESS → COMPLETED/FAILED/CANCELLED

**Agent types:** "claude" (default) or "codex", managed as tmux sessions named `map-agent-{agentID}`

**Event streaming:** Real-time via `WatchEvents` RPC with broadcast to all connected watchers
