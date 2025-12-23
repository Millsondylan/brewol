-e hi
hi
hi
# brewol

An autonomous terminal-based coding agent that **never stops working**. It continuously plans, executes, and verifies changes to your codebase using local LLMs via Ollama.

## Features

- **Fully Autonomous**: Never asks for permission, never pauses for input
- **Never Stops**: Continuous observe → decide → act → verify → commit loop
- **Local LLM**: Uses Ollama for privacy and speed
- **Full-Screen TUI**: Built with Bubble Tea for a beautiful terminal experience
- **Smart Project Detection**: Auto-detects Go, Node.js, Python, Rust, Java projects
- **Safe by Default**: All operations contained within workspace root
- **Checkpointing**: Automatic git commits for easy rollback

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap ai/tap
brew install brewol
```

### From Source

```bash
go install github.com/ai/brewol/cmd/brewol@latest
```

### Binary Download

Download the latest release from the [Releases page](https://github.com/ai/brewol/releases).

## Prerequisites

- [Ollama](https://ollama.ai) installed and running
- A local LLM model (e.g., `ollama pull codellama`)
- Git (for checkpointing)

## Quick Start

```bash
# Start Ollama
ollama serve

# Pull a model (if you haven't already)
ollama pull codellama

# Run brewol in your project directory
cd /path/to/your/project
brewol -m codellama
```

## Usage

```bash
brewol [flags]

Flags:
  -w, --workspace string   Workspace root directory (default: current directory)
  -g, --goal string        Initial goal for the agent
  -m, --model string       Ollama model to use (overrides OLLAMA_MODEL)
  -v, --version            Show version information
  -h, --help               Show help
```

### Examples

```bash
# Start in current directory with default model
brewol

# Start with a specific goal
brewol -g "Fix all failing tests"

# Use a specific model
brewol -m codellama

# Start in a different directory
brewol -w /path/to/project
```

## Keybindings

| Key | Action |
|-----|--------|
| `ESC` | Cancel current operation |
| `ESC ESC` | Exit (double-press within 600ms) |
| `Ctrl+K` | Command palette |
| `Ctrl+M` | Model picker |
| `Ctrl+D` | Toggle diff panel |
| `Ctrl+L` | Show logs path |
| `?` | Toggle help |
| `↑/k` | Scroll up |
| `↓/j` | Scroll down |
| `PgUp` | Page up |
| `PgDn` | Page down |

## Commands

Access via `Ctrl+K` command palette or by typing directly:

| Command | Description |
|---------|-------------|
| `/goal <text>` | Set the current goal |
| `/model <name>` | Switch to a different model |
| `/models` | Show model picker |
| `/status` | Show current status |
| `/checkpoint` | Create a manual checkpoint |
| `/rollback` | Rollback to last checkpoint |
| `/speed <n>` | Set throttle (0 = no throttle) |
| `/pause` | Pause the agent |
| `/resume` | Resume the agent |

### System Instructions Commands

Control the system prompt that guides the agent:

| Command | Description |
|---------|-------------|
| `/system show` | Display the effective system prompt (with secrets redacted) |
| `/system set <text>` | Set session-level instructions immediately |
| `/system load <path>` | Load instructions from a file (must be in workspace or config dir) |
| `/system reset` | Clear session instructions, revert to base+repo+user layers |
| `/system save` | Save session instructions to user config file |

### Memory Commands

View and manage the agent's working memory:

| Command | Description |
|---------|-------------|
| `/summary` | Show operational summary (state, goal, branch, backlog) |
| `/memory` | Show current rolling memory content |
| `/memory reset` | Clear working memory (logs preserved on disk) |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OLLAMA_HOST` | Ollama API base URL | `http://localhost:11434` |
| `OLLAMA_MODEL` | Default model to use | (none) |
| `OLLAMA_API_KEY` | API key for cloud endpoint | (none) |
| `OLLAMA_KEEP_ALIVE` | Model keep-alive duration | `-1` (forever) |

## How It Works

### The Autonomy Loop

brewol runs a continuous state machine:

1. **Observe**: Check git status, scan for TODOs, identify failing tests
2. **Decide**: Ask the LLM to pick the highest-value task from the backlog
3. **Execute**: Run tool calls immediately (no approval needed)
4. **Verify**: Run appropriate tests/build for the project type
5. **Commit**: Create a checkpoint commit
6. **Repeat**: Immediately start the next cycle

### Project Detection

brewol automatically detects your project type and configures appropriate commands:

| Project | Detection | Test Command | Build Command |
|---------|-----------|--------------|---------------|
| Go | `go.mod` | `go test ./...` | `go build ./...` |
| Node.js | `package.json` | `npm/pnpm/yarn test` | `npm/pnpm/yarn build` |
| Python | `pyproject.toml` | `pytest` | - |
| Rust | `Cargo.toml` | `cargo test` | `cargo build` |
| Java (Maven) | `pom.xml` | `mvn test` | `mvn package` |
| Java (Gradle) | `build.gradle` | `./gradlew test` | `./gradlew build` |
| Make | `Makefile` | `make test` | `make build` |

### Backlog Prioritization

Tasks are prioritized by impact:

1. **Critical** (P1): Failing tests, build errors, runtime errors
2. **High** (P2): User-set goals, FIXME/HACK comments
3. **Medium** (P3): TODO comments
4. **Low** (P4): Style improvements, documentation gaps

### Safety Features

- **Path Containment**: All file operations restricted to workspace root
- **Non-Interactive**: Git and shell commands run without prompts
- **Checkpoint Commits**: Every successful objective creates a commit
- **Rollback**: Easy recovery to previous state

## Logs & Memory

Session logs are saved to `.brewol/logs/<session-id>/`:

- `transcript.jsonl`: Full conversation history
- `tools.jsonl`: Tool execution logs
- `patches/`: Saved patches for recovery

Working memory is stored in `.brewol/memory/`:

- `working_memory.json`: Persistent memory between sessions
- `transcript_*.jsonl`: Full transcript per session
- `full_log_*.jsonl`: Detailed logs per session

## Instruction Layering

The system prompt is built from multiple layers, merged in order:

1. **Base Layer** (built-in): Core agent behavior and tool usage instructions
2. **Repo Layer**: From `.aicoder/system.md`, `AGENT.md`, or `CLAUDE.md` in workspace
3. **User Layer**: From `~/.config/brewol/system.md`
4. **Session Layer**: Set live via `/system set`, highest priority

All layers are combined for each model call. Use `/system show` to see the effective prompt.

## Architecture

```
cmd/brewol/          # Main entry point
internal/
  ├── engine/        # Autonomy state machine
  ├── ollama/        # Ollama API client
  ├── tools/         # Tool implementations (fs, git, exec, search)
  ├── repo/          # Project detection & verification
  ├── logs/          # Session logging
  ├── prompt/        # Instruction layering & prompt management
  ├── memory/        # Rolling memory & summarization
  └── tui/           # Bubble Tea TUI
```

## Development

### Building

```bash
go build ./cmd/brewol
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -race -coverprofile=coverage.out ./...

# Run tests in test mode (useful for integration tests)
go build ./cmd/brewol
./brewol --test-mode --max-cycles 3 -g "test goal" -m test-model
```

### Code Formatting

Before committing, ensure all code is properly formatted:

```bash
gofmt -w .
```

CI will fail if code is not formatted.

### Release Process

Releases are fully automated via GitHub Actions and GoReleaser:

1. **Create and push a version tag:**
   ```bash
   # Tag format: vMAJOR.MINOR.PATCH (e.g., v1.2.3)
   git tag v0.1.0
   git push origin v0.1.0
   ```

2. **Automated steps (no manual intervention):**
   - Full CI runs (tests, linting, formatting checks, multi-platform builds)
   - GoReleaser creates GitHub Release with:
     - Binary archives for all platforms (Linux, macOS, Windows)
     - Checksums
     - Changelog
   - Homebrew tap is automatically updated

3. **Installation after release:**
   ```bash
   # Homebrew (recommended)
   brew tap <owner>/tap
   brew install brewol

   # Or download binary from GitHub Releases
   # https://github.com/<owner>/brewol/releases
   ```

### Release Requirements

- All tests must pass
- Code must be formatted (gofmt)
- Linting must pass (golangci-lint)
- Tag must follow semantic versioning: `vMAJOR.MINOR.PATCH`

### GitHub Secrets Required for Releases

The following secrets must be configured in the GitHub repository:
- `HOMEBREW_TAP_GITHUB_TOKEN`: Personal access token with write access to the Homebrew tap repository

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `go test ./...`
5. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Acknowledgments

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Ollama](https://ollama.ai) - Local LLM runtime
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
hi
