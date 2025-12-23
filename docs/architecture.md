# brewol Architecture

## Overview

brewol is an autonomous terminal-based coding agent that continuously works on your codebase using local LLMs via Ollama. It follows a never-stop design philosophy, running an infinite observe-decide-act-verify-checkpoint loop until explicitly terminated.

## Design Principles

1. **Fully Autonomous**: Never asks for permission, never pauses for input
2. **Never Stops**: Continuous execution loop with no standby states
3. **Local-First**: Uses Ollama for privacy and low latency
4. **Safe by Default**: All operations confined to workspace root
5. **Recoverable**: Automatic checkpointing and rollback capabilities

## Component Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         cmd/brewol/main.go                       │
│                    (Entry point, CLI parsing)                    │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      internal/tui/model.go                       │
│                   (Bubble Tea TUI Framework)                     │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌───────────┐  │
│  │   Header    │ │   Stream    │ │   Tool Log  │ │  Footer   │  │
│  │   (state,   │ │   View      │ │   View      │ │  (input,  │  │
│  │   model,    │ │   (LLM      │ │   (exec     │ │  status,  │  │
│  │   branch)   │ │   output)   │ │   history)  │ │  help)    │  │
│  └─────────────┘ └─────────────┘ └─────────────┘ └───────────┘  │
└─────────────────────────────┬───────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    internal/engine/state.go                      │
│                  (Autonomous State Machine)                      │
│                                                                  │
│     ┌──────────┐    ┌──────────┐    ┌───────────┐               │
│     │ OBSERVING│───▶│ DECIDING │───▶│ EXECUTING │               │
│     └──────────┘    └──────────┘    └───────────┘               │
│          ▲                                │                      │
│          │                                ▼                      │
│     ┌────┴─────┐    ┌──────────┐    ┌───────────┐               │
│     │COMMITTING│◀───│VERIFYING │◀───│           │               │
│     └──────────┘    └──────────┘    └───────────┘               │
│                                                                  │
│     ┌──────────────────────────────────────────┐                │
│     │              RECOVERING                   │                │
│     │  (Error handling, rollback, retry)        │                │
│     └──────────────────────────────────────────┘                │
└─────────────────────┬───────────────────────────────────────────┘
                      │
        ┌─────────────┼─────────────┐
        ▼             ▼             ▼
┌───────────┐ ┌───────────────┐ ┌───────────────┐
│  Ollama   │ │     Tools     │ │     Repo      │
│  Client   │ │   Registry    │ │   Detection   │
└───────────┘ └───────────────┘ └───────────────┘
```

## Package Structure

### cmd/brewol/
Entry point. Parses CLI flags, creates the engine, and starts the TUI.

### internal/engine/
The autonomous state machine that drives the agent.

**Key Types:**
- `Engine`: Main controller with state machine loop
- `State`: Enum for current phase (Observing, Deciding, Executing, etc.)
- `BacklogItem`: Task queue items with priority
- `CycleUpdate`: Updates sent to TUI during execution

**State Machine:**
1. **OBSERVING**: Gather context (git status, TODOs, test results)
2. **DECIDING**: Ask LLM to choose next action
3. **EXECUTING**: Run tool calls (file ops, commands)
4. **VERIFYING**: Run tests/build to validate changes
5. **COMMITTING**: Create checkpoint commit
6. **RECOVERING**: Handle errors, rollback if needed

### internal/ollama/
HTTP client for Ollama API with streaming support.

**Features:**
- Model listing (`/api/tags`)
- Streaming chat (`/api/chat` with SSE)
- Automatic context trimming to avoid token limits
- Support for local and cloud Ollama endpoints

### internal/tools/
Tool implementations for file system, git, and command execution.

**Registered Tools:**
| Tool | Description |
|------|-------------|
| `fs_list` | List directory contents |
| `fs_read` | Read file contents |
| `fs_write` | Write file contents |
| `fs_patch` | Apply unified diff |
| `rg_search` | Search files with ripgrep |
| `exec` | Execute shell command |
| `shell` | Simplified command execution |
| `git_status` | Get git status |
| `git_diff` | Get git diff |
| `git_checkout` | Checkout branch/commit |
| `git_create_branch` | Create new branch |
| `git_commit` | Stage and commit changes |
| `git_reset_hard` | Hard reset (recovery only) |

**Security:**
- Path containment validation prevents traversal attacks
- Non-interactive mode for git operations
- Workspace root confinement

### internal/repo/
Project detection and verification.

**Supported Projects:**
- Go (`go.mod`)
- Node.js (`package.json`)
- Python (`pyproject.toml`, `requirements.txt`)
- Rust (`Cargo.toml`)
- Java (Maven/Gradle)
- Make (Makefile)

**Verification Commands:**
- Test, Build, Lint, Format per project type
- Automatic package manager detection (npm/yarn/pnpm)

### internal/logs/
Session logging and transcript management.

**Log Files:**
- `transcript.jsonl`: Full conversation history
- `tools.jsonl`: Tool execution audit log
- `patches/`: Saved patches for recovery

## Data Flow

```
User Input (goal)
       │
       ▼
┌──────────────┐
│   Engine     │
│  SetGoal()   │
└──────┬───────┘
       │
       ▼
┌──────────────┐     ┌─────────────┐
│   observe()  │────▶│ Git Status  │
│              │     │ TODO Scan   │
└──────┬───────┘     └─────────────┘
       │
       ▼
┌──────────────┐     ┌─────────────┐
│   decide()   │────▶│   Ollama    │
│              │◀────│  Streaming  │
└──────┬───────┘     └─────────────┘
       │
       ▼
┌──────────────┐     ┌─────────────┐
│  execute()   │────▶│   Tools     │
│  (commands)  │◀────│  Registry   │
└──────┬───────┘     └─────────────┘
       │
       ▼
┌──────────────┐     ┌─────────────┐
│   verify()   │────▶│   Tests     │
│              │     │   Build     │
└──────┬───────┘     └─────────────┘
       │
       ▼
┌──────────────┐     ┌─────────────┐
│ checkpoint() │────▶│ Git Commit  │
│              │     │ Session Log │
└──────────────┘     └─────────────┘
```

## Keybindings

| Key | Action |
|-----|--------|
| ESC | Cancel current operation |
| ESC ESC | Exit (within 600ms) |
| Ctrl+K | Command palette |
| Ctrl+M | Model picker |
| Ctrl+D | Toggle diff panel |
| Ctrl+L | Show logs path |
| ? | Toggle help |

## Error Handling

1. **Rate Limiting**: Auto-pause on 403/429 errors
2. **Consecutive Errors**: Exponential backoff, pause after 3 failures
3. **Context Cancelled**: Restart with fresh context
4. **Verification Failure**: Rollback uncommitted changes

## Configuration

**Environment Variables:**
| Variable | Description | Default |
|----------|-------------|---------|
| `OLLAMA_HOST` | Ollama API URL | `http://localhost:11434` |
| `OLLAMA_MODEL` | Default model | (first available) |
| `OLLAMA_API_KEY` | API key for cloud | (none) |
| `OLLAMA_KEEP_ALIVE` | Keep-alive duration | `-1` (forever) |

## Release Process

1. Tag with semantic version: `git tag v0.1.0`
2. Push tag: `git push origin v0.1.0`
3. GitHub Actions runs GoReleaser
4. Binaries uploaded to GitHub Releases
5. Homebrew formula updated in tap

## Future Enhancements

- [ ] Config file support (`.brewol.yaml`)
- [ ] Multiple model backends (OpenAI, Anthropic)
- [ ] Plugin system for custom tools
- [ ] Web UI option
- [ ] Remote workspace support
