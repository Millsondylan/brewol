package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayer_String(t *testing.T) {
	tests := []struct {
		layer    Layer
		expected string
	}{
		{LayerBase, "base"},
		{LayerRepo, "repo"},
		{LayerUser, "user"},
		{LayerSession, "session"},
		{Layer(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.layer.String(); got != tt.expected {
			t.Errorf("Layer(%d).String() = %q, want %q", tt.layer, got, tt.expected)
		}
	}
}

func TestNewManager(t *testing.T) {
	tempDir := t.TempDir()

	m := NewManager("testapp", tempDir, "go")

	if m.appName != "testapp" {
		t.Errorf("expected appName testapp, got %s", m.appName)
	}
	if m.workspaceRoot != tempDir {
		t.Errorf("expected workspaceRoot %s, got %s", tempDir, m.workspaceRoot)
	}
	if m.projectType != "go" {
		t.Errorf("expected projectType go, got %s", m.projectType)
	}
	if m.basePrompt == "" {
		t.Error("expected basePrompt to be set")
	}
}

func TestManager_GetEffectivePrompt_BaseOnly(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	prompt := m.GetEffectivePrompt()

	if !strings.Contains(prompt, "autonomous coding agent") {
		t.Error("expected base prompt to contain 'autonomous coding agent'")
	}
	if !strings.Contains(prompt, tempDir) {
		t.Error("expected base prompt to contain workspace root")
	}
}

func TestManager_LoadRepoInstructions_AicoderPath(t *testing.T) {
	tempDir := t.TempDir()

	// Create .aicoder/system.md
	aicoderDir := filepath.Join(tempDir, ".aicoder")
	os.MkdirAll(aicoderDir, 0755)
	os.WriteFile(filepath.Join(aicoderDir, "system.md"), []byte("Repo instructions from .aicoder"), 0644)

	m := NewManager("testapp", tempDir, "go")

	prompt := m.GetEffectivePrompt()
	if !strings.Contains(prompt, "Repo instructions from .aicoder") {
		t.Error("expected repo instructions from .aicoder/system.md")
	}
	if !strings.Contains(prompt, "REPOSITORY INSTRUCTIONS") {
		t.Error("expected REPOSITORY INSTRUCTIONS header")
	}
}

func TestManager_LoadRepoInstructions_AgentMd(t *testing.T) {
	tempDir := t.TempDir()

	// Create AGENT.md
	os.WriteFile(filepath.Join(tempDir, "AGENT.md"), []byte("Repo instructions from AGENT.md"), 0644)

	m := NewManager("testapp", tempDir, "go")

	prompt := m.GetEffectivePrompt()
	if !strings.Contains(prompt, "Repo instructions from AGENT.md") {
		t.Error("expected repo instructions from AGENT.md")
	}
}

func TestManager_LoadRepoInstructions_ClaudeMd(t *testing.T) {
	tempDir := t.TempDir()

	// Create CLAUDE.md
	os.WriteFile(filepath.Join(tempDir, "CLAUDE.md"), []byte("Repo instructions from CLAUDE.md"), 0644)

	m := NewManager("testapp", tempDir, "go")

	prompt := m.GetEffectivePrompt()
	if !strings.Contains(prompt, "Repo instructions from CLAUDE.md") {
		t.Error("expected repo instructions from CLAUDE.md")
	}
}

func TestManager_LoadRepoInstructions_Priority(t *testing.T) {
	tempDir := t.TempDir()

	// Create both .aicoder/system.md and AGENT.md - .aicoder should win
	aicoderDir := filepath.Join(tempDir, ".aicoder")
	os.MkdirAll(aicoderDir, 0755)
	os.WriteFile(filepath.Join(aicoderDir, "system.md"), []byte("Priority: .aicoder"), 0644)
	os.WriteFile(filepath.Join(tempDir, "AGENT.md"), []byte("Priority: AGENT"), 0644)

	m := NewManager("testapp", tempDir, "go")

	prompt := m.GetEffectivePrompt()
	if !strings.Contains(prompt, "Priority: .aicoder") {
		t.Error("expected .aicoder/system.md to have priority")
	}
	if strings.Contains(prompt, "Priority: AGENT") {
		t.Error("AGENT.md should not be loaded when .aicoder/system.md exists")
	}
}

func TestManager_SessionInstructions(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	// Initially empty
	if m.GetSessionInstructions() != "" {
		t.Error("expected empty session instructions initially")
	}

	// Set session instructions
	m.SetSessionInstructions("Test session instructions")

	if m.GetSessionInstructions() != "Test session instructions" {
		t.Errorf("expected 'Test session instructions', got %q", m.GetSessionInstructions())
	}

	// Check in effective prompt
	prompt := m.GetEffectivePrompt()
	if !strings.Contains(prompt, "Test session instructions") {
		t.Error("expected session instructions in effective prompt")
	}
	if !strings.Contains(prompt, "SESSION INSTRUCTIONS") {
		t.Error("expected SESSION INSTRUCTIONS header")
	}

	// Clear
	m.ClearSessionInstructions()
	if m.GetSessionInstructions() != "" {
		t.Error("expected empty after clear")
	}
}

func TestManager_GetLayers(t *testing.T) {
	tempDir := t.TempDir()

	// Create repo instructions
	os.WriteFile(filepath.Join(tempDir, "AGENT.md"), []byte("Repo content"), 0644)

	m := NewManager("testapp", tempDir, "go")
	m.SetSessionInstructions("Session content")

	layers := m.GetLayers()

	if len(layers) != 4 {
		t.Fatalf("expected 4 layers, got %d", len(layers))
	}

	// Check base layer
	if layers[0].Layer != LayerBase || !layers[0].Enabled {
		t.Error("base layer should be enabled")
	}
	if layers[0].Source != "builtin" {
		t.Errorf("expected source 'builtin', got %s", layers[0].Source)
	}

	// Check repo layer
	if layers[1].Layer != LayerRepo || !layers[1].Enabled {
		t.Error("repo layer should be enabled")
	}
	if layers[1].Content != "Repo content" {
		t.Errorf("expected repo content, got %q", layers[1].Content)
	}

	// Check user layer (should be disabled - no user config)
	if layers[2].Layer != LayerUser || layers[2].Enabled {
		t.Error("user layer should be disabled without config")
	}

	// Check session layer
	if layers[3].Layer != LayerSession || !layers[3].Enabled {
		t.Error("session layer should be enabled")
	}
	if layers[3].Content != "Session content" {
		t.Errorf("expected session content, got %q", layers[3].Content)
	}
}

func TestManager_LoadInstructionsFromFile(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	// Create a file within workspace
	instrFile := filepath.Join(tempDir, "instructions.md")
	os.WriteFile(instrFile, []byte("Custom instructions"), 0644)

	// Load should succeed
	err := m.LoadInstructionsFromFile(instrFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.GetSessionInstructions() != "Custom instructions" {
		t.Errorf("expected 'Custom instructions', got %q", m.GetSessionInstructions())
	}
}

func TestManager_LoadInstructionsFromFile_OutsideWorkspace(t *testing.T) {
	tempDir := t.TempDir()
	outsideDir := t.TempDir() // Different temp dir

	m := NewManager("testapp", tempDir, "go")

	// Create a file outside workspace
	outsideFile := filepath.Join(outsideDir, "instructions.md")
	os.WriteFile(outsideFile, []byte("Outside content"), 0644)

	// Load should fail
	err := m.LoadInstructionsFromFile(outsideFile)
	if err == nil {
		t.Error("expected error when loading from outside workspace")
	}
}

func TestManager_LoadInstructionsFromFile_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	err := m.LoadInstructionsFromFile(filepath.Join(tempDir, "nonexistent.md"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestManager_UserConfigPath(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	path := m.UserConfigPath()
	if path == "" {
		t.Skip("user config dir not available")
	}

	if !strings.Contains(path, "testapp") {
		t.Error("expected path to contain app name")
	}
	if !strings.HasSuffix(path, "system.md") {
		t.Error("expected path to end with system.md")
	}
}

func TestManager_WorkspaceRoot(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	if m.WorkspaceRoot() != tempDir {
		t.Errorf("expected %s, got %s", tempDir, m.WorkspaceRoot())
	}
}

func TestManager_GetEffectivePromptRedacted(t *testing.T) {
	tempDir := t.TempDir()
	m := NewManager("testapp", tempDir, "go")

	// Set session with a secret - OpenAI key pattern requires sk- followed by 32+ chars
	m.SetSessionInstructions("Use API key: sk-abc123def456ghi789jkl012mno345pqr")

	redacted := m.GetEffectivePromptRedacted()

	if strings.Contains(redacted, "sk-abc123def456ghi789jkl012mno345pqr") {
		t.Error("expected OpenAI key to be redacted")
	}
	if !strings.Contains(redacted, "[REDACTED]") {
		t.Error("expected [REDACTED] marker")
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // should not contain after redaction
		hasRedact bool
	}{
		{
			name:     "OpenAI API key",
			input:    "sk-abc123def456ghi789jkl012mno345pq",
			contains: "sk-abc",
			hasRedact: true,
		},
		{
			name:     "Anthropic API key",
			input:    "sk-ant-abc123def456ghi789jkl012mno345pq",
			contains: "sk-ant-",
			hasRedact: true,
		},
		{
			name:     "GitHub PAT",
			input:    "ghp_abcdefghijklmnopqrstuvwxyz123456789012",
			contains: "ghp_",
			hasRedact: true,
		},
		{
			name:     "GitHub OAuth",
			input:    "gho_abcdefghijklmnopqrstuvwxyz123456789012",
			contains: "gho_",
			hasRedact: true,
		},
		{
			name:     "GitHub fine-grained PAT",
			input:    "github_pat_abcdefghijklmnopqrstuvwxyz",
			contains: "github_pat_",
			hasRedact: true,
		},
		{
			name:     "Slack bot token",
			input:    "xoxb-123-456-abcdefghijklmnop",
			contains: "xoxb-",
			hasRedact: true,
		},
		{
			name:     "Slack user token",
			input:    "xoxp-123-456-abcdefghijklmnop",
			contains: "xoxp-",
			hasRedact: true,
		},
		{
			name:     "AWS Access Key",
			input:    "AKIAIOSFODNN7EXAMPLE",
			contains: "AKIA",
			hasRedact: true,
		},
		{
			name:     "API key assignment",
			input:    "api_key: abc123def456ghi789jkl012",
			contains: "abc123def456ghi789jkl012",
			hasRedact: true,
		},
		{
			name:     "Bearer token",
			input:    "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			contains: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			hasRedact: true,
		},
		{
			name:     "Password",
			input:    "password: mysecretpassword123",
			contains: "mysecretpassword123",
			hasRedact: true,
		},
		{
			name:     "No secrets",
			input:    "This is normal text without secrets",
			contains: "",
			hasRedact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSecrets(tt.input)

			if tt.hasRedact {
				if !strings.Contains(result, "[REDACTED]") {
					t.Errorf("expected [REDACTED] in result: %q", result)
				}
				if tt.contains != "" && strings.Contains(result, tt.contains) {
					t.Errorf("expected %q to be redacted, got %q", tt.contains, result)
				}
			} else {
				if strings.Contains(result, "[REDACTED]") {
					t.Errorf("unexpected [REDACTED] in result: %q", result)
				}
			}
		})
	}
}

func TestRedactSecrets_Preserves_KeyName(t *testing.T) {
	input := "api_key: mysecretkey12345678901234567890"
	result := RedactSecrets(input)

	// The key name should be preserved
	if !strings.HasPrefix(result, "api_key:") {
		t.Errorf("expected key name to be preserved, got %q", result)
	}
}

func TestManager_ReloadInstructions(t *testing.T) {
	tempDir := t.TempDir()

	// Create initial AGENT.md
	os.WriteFile(filepath.Join(tempDir, "AGENT.md"), []byte("Initial content"), 0644)

	m := NewManager("testapp", tempDir, "go")

	// Verify initial content
	layers := m.GetLayers()
	if layers[1].Content != "Initial content" {
		t.Errorf("expected 'Initial content', got %q", layers[1].Content)
	}

	// Update the file
	os.WriteFile(filepath.Join(tempDir, "AGENT.md"), []byte("Updated content"), 0644)

	// Reload
	m.ReloadInstructions()

	// Verify updated content
	layers = m.GetLayers()
	if layers[1].Content != "Updated content" {
		t.Errorf("expected 'Updated content' after reload, got %q", layers[1].Content)
	}
}
