// Package prompt provides instruction layering and system prompt management.
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Layer represents an instruction layer priority
type Layer int

const (
	LayerBase    Layer = iota // Compiled into binary / default
	LayerRepo                 // From WORKSPACE_ROOT/.aicoder/system.md or AGENT.md
	LayerUser                 // From ~/.config/<app>/system.md
	LayerSession              // Set live in TUI, not persisted unless saved
)

func (l Layer) String() string {
	switch l {
	case LayerBase:
		return "base"
	case LayerRepo:
		return "repo"
	case LayerUser:
		return "user"
	case LayerSession:
		return "session"
	default:
		return "unknown"
	}
}

// LayerInfo contains metadata about an instruction layer
type LayerInfo struct {
	Layer   Layer
	Content string
	Source  string // file path or "builtin"
	Enabled bool
}

// Manager manages instruction layers and builds effective system prompts
type Manager struct {
	appName       string
	workspaceRoot string
	projectType   string

	// Instruction layers
	basePrompt    string
	repoPrompt    string
	repoSource    string
	userPrompt    string
	userSource    string
	sessionPrompt string

	mu sync.RWMutex
}

// NewManager creates a new prompt manager
func NewManager(appName, workspaceRoot, projectType string) *Manager {
	m := &Manager{
		appName:       appName,
		workspaceRoot: workspaceRoot,
		projectType:   projectType,
	}

	// Set default base prompt
	m.basePrompt = m.buildBasePrompt()

	// Load repo-level instructions
	m.loadRepoInstructions()

	// Load user-level instructions
	m.loadUserInstructions()

	return m
}

// buildBasePrompt creates the default system prompt
func (m *Manager) buildBasePrompt() string {
	return fmt.Sprintf(`You are an autonomous coding agent working in %s (%s).

## CORE PRINCIPLES

1. **TOOL-BASED EVIDENCE**: Every claim about code state must come from tool output.
   - Never claim to have read/written/verified something without tool evidence
   - Always show the actual tool result that proves the claim

2. **OBSERVE-DECIDE-ACT-VERIFY-CHECKPOINT CYCLE**:
   - OBSERVE: Gather context using tools (fs_read, rg_search, git_status, etc.)
   - DECIDE: Plan the next action based on observations
   - ACT: Execute the planned action using appropriate tool
   - VERIFY: Confirm the action succeeded by re-reading or running tests
   - CHECKPOINT: Commit working changes with descriptive message

3. **PATCH-FIRST EDITING**:
   - Prefer fs_patch with unified diffs over fs_write for existing files
   - Always read the file first to understand current state
   - After patching, re-read changed regions to confirm correctness

4. **VERIFICATION BEFORE COMMIT**:
   - Run tests/build before committing
   - Only commit when verification passes
   - If verification fails, fix issues first

5. **ROLLBACK STRATEGY**:
   - If stuck after 3 attempts, consider git reset to last good state
   - Never leave the codebase in a broken state

## OUTPUT FORMAT

Output commands in bash code blocks to execute them:
%sshell
ls -la
cat file.txt
%s

I will run those commands and show you the output. Be concise. Do one thing at a time.

## AVAILABLE TOOLS

- fs_list: List directory contents
- fs_read: Read file contents
- fs_write: Write new file (use for new files only)
- fs_patch: Apply unified diff (preferred for editing)
- rg_search: Search code with ripgrep
- shell: Execute shell commands
- git_status, git_diff, git_commit, git_reset_hard: Git operations`,
		m.workspaceRoot,
		m.projectType,
		"```", "```")
}

// loadRepoInstructions loads instructions from the repository
func (m *Manager) loadRepoInstructions() {
	// Check for .aicoder/system.md first
	aicoderPath := filepath.Join(m.workspaceRoot, ".aicoder", "system.md")
	if content, err := os.ReadFile(aicoderPath); err == nil {
		m.repoPrompt = string(content)
		m.repoSource = aicoderPath
		return
	}

	// Fall back to AGENT.md
	agentPath := filepath.Join(m.workspaceRoot, "AGENT.md")
	if content, err := os.ReadFile(agentPath); err == nil {
		m.repoPrompt = string(content)
		m.repoSource = agentPath
		return
	}

	// Also check for CLAUDE.md (common convention)
	claudePath := filepath.Join(m.workspaceRoot, "CLAUDE.md")
	if content, err := os.ReadFile(claudePath); err == nil {
		m.repoPrompt = string(content)
		m.repoSource = claudePath
		return
	}
}

// loadUserInstructions loads instructions from user config
func (m *Manager) loadUserInstructions() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}

	userPath := filepath.Join(configDir, m.appName, "system.md")
	if content, err := os.ReadFile(userPath); err == nil {
		m.userPrompt = string(content)
		m.userSource = userPath
	}
}

// GetEffectivePrompt returns the merged system prompt from all layers
func (m *Manager) GetEffectivePrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var parts []string

	// Base layer is always included
	parts = append(parts, m.basePrompt)

	// Repo layer
	if m.repoPrompt != "" {
		parts = append(parts, fmt.Sprintf("\n## REPOSITORY INSTRUCTIONS (from %s)\n\n%s", m.repoSource, m.repoPrompt))
	}

	// User layer
	if m.userPrompt != "" {
		parts = append(parts, fmt.Sprintf("\n## USER INSTRUCTIONS\n\n%s", m.userPrompt))
	}

	// Session layer (highest priority)
	if m.sessionPrompt != "" {
		parts = append(parts, fmt.Sprintf("\n## SESSION INSTRUCTIONS (active)\n\n%s", m.sessionPrompt))
	}

	return strings.Join(parts, "\n")
}

// GetEffectivePromptRedacted returns the prompt with secrets redacted for display
func (m *Manager) GetEffectivePromptRedacted() string {
	prompt := m.GetEffectivePrompt()
	return RedactSecrets(prompt)
}

// GetLayers returns information about all instruction layers
func (m *Manager) GetLayers() []LayerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return []LayerInfo{
		{Layer: LayerBase, Content: m.basePrompt, Source: "builtin", Enabled: true},
		{Layer: LayerRepo, Content: m.repoPrompt, Source: m.repoSource, Enabled: m.repoPrompt != ""},
		{Layer: LayerUser, Content: m.userPrompt, Source: m.userSource, Enabled: m.userPrompt != ""},
		{Layer: LayerSession, Content: m.sessionPrompt, Source: "session", Enabled: m.sessionPrompt != ""},
	}
}

// SetSessionInstructions sets the session-level instructions
func (m *Manager) SetSessionInstructions(instructions string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionPrompt = instructions
}

// GetSessionInstructions returns the current session instructions
func (m *Manager) GetSessionInstructions() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionPrompt
}

// ClearSessionInstructions clears session-level instructions
func (m *Manager) ClearSessionInstructions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionPrompt = ""
}

// LoadInstructionsFromFile loads instructions from a file and sets them as session instructions
func (m *Manager) LoadInstructionsFromFile(path string) error {
	// Validate path is within workspace or user config
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	configDir, _ := os.UserConfigDir()
	workspaceAbs, _ := filepath.Abs(m.workspaceRoot)

	isWorkspace := strings.HasPrefix(absPath, workspaceAbs)
	isUserConfig := configDir != "" && strings.HasPrefix(absPath, configDir)

	if !isWorkspace && !isUserConfig {
		return fmt.Errorf("path must be within workspace (%s) or user config (%s)", m.workspaceRoot, configDir)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	m.mu.Lock()
	m.sessionPrompt = string(content)
	m.mu.Unlock()

	return nil
}

// SaveSessionToUser saves session instructions to user-level config
func (m *Manager) SaveSessionToUser() error {
	m.mu.RLock()
	session := m.sessionPrompt
	m.mu.RUnlock()

	if session == "" {
		return fmt.Errorf("no session instructions to save")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	appConfigDir := filepath.Join(configDir, m.appName)
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	userPath := filepath.Join(appConfigDir, "system.md")
	if err := os.WriteFile(userPath, []byte(session), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Update user prompt to reflect saved content
	m.mu.Lock()
	m.userPrompt = session
	m.userSource = userPath
	m.mu.Unlock()

	return nil
}

// ReloadInstructions reloads repo and user instructions from disk
func (m *Manager) ReloadInstructions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset and reload
	m.repoPrompt = ""
	m.repoSource = ""
	m.userPrompt = ""
	m.userSource = ""

	// Temporarily unlock for loading
	m.mu.Unlock()
	m.loadRepoInstructions()
	m.loadUserInstructions()
	m.mu.Lock()
}

// Secret redaction patterns
var secretPatterns = []*regexp.Regexp{
	// API keys (generic patterns)
	regexp.MustCompile(`(?i)(api[_-]?key|apikey)[=:]\s*["']?([A-Za-z0-9_\-]{20,})["']?`),
	regexp.MustCompile(`(?i)(secret[_-]?key|secretkey)[=:]\s*["']?([A-Za-z0-9_\-]{20,})["']?`),
	regexp.MustCompile(`(?i)(access[_-]?token|accesstoken)[=:]\s*["']?([A-Za-z0-9_\-]{20,})["']?`),
	regexp.MustCompile(`(?i)(auth[_-]?token|authtoken)[=:]\s*["']?([A-Za-z0-9_\-]{20,})["']?`),
	regexp.MustCompile(`(?i)(bearer)\s+([A-Za-z0-9_\-\.]{20,})`),

	// Specific service patterns
	regexp.MustCompile(`sk-[A-Za-z0-9]{32,}`),                       // OpenAI
	regexp.MustCompile(`sk-ant-[A-Za-z0-9\-]{32,}`),                 // Anthropic
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),                      // GitHub PAT
	regexp.MustCompile(`gho_[A-Za-z0-9]{36,}`),                      // GitHub OAuth
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),              // GitHub fine-grained PAT
	regexp.MustCompile(`xoxb-[A-Za-z0-9\-]+`),                       // Slack bot token
	regexp.MustCompile(`xoxp-[A-Za-z0-9\-]+`),                       // Slack user token
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),                          // AWS Access Key
	regexp.MustCompile(`(?i)aws[_-]?secret[_-]?access[_-]?key[=:]\s*["']?([A-Za-z0-9/+=]{40})["']?`),

	// Generic long secrets
	regexp.MustCompile(`(?i)(password|passwd|pwd)[=:]\s*["']?([^\s"']{8,})["']?`),
	regexp.MustCompile(`(?i)(private[_-]?key)[=:]\s*["']?([A-Za-z0-9_\-/+=]{20,})["']?`),
}

// RedactSecrets replaces potential secrets with [REDACTED]
func RedactSecrets(content string) string {
	result := content
	for _, pattern := range secretPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the key name but redact the value
			parts := pattern.FindStringSubmatch(match)
			if len(parts) > 1 {
				// Find where the secret value starts
				idx := strings.Index(match, parts[len(parts)-1])
				if idx > 0 {
					return match[:idx] + "[REDACTED]"
				}
			}
			return "[REDACTED]"
		})
	}
	return result
}

// UserConfigPath returns the path to the user config file
func (m *Manager) UserConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, m.appName, "system.md")
}

// WorkspaceRoot returns the workspace root
func (m *Manager) WorkspaceRoot() string {
	return m.workspaceRoot
}
