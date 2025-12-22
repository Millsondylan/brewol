// Package tools provides the tool registry and implementations for the autonomous agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ai/brewol/internal/ollama"
)

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Name     string
	Output   string
	Error    error
	Duration float64 // seconds
	ExitCode int     // for exec tools
}

// Tool is the interface that all tools must implement
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, args json.RawMessage) (*ToolResult, error)
}

// Registry manages available tools
type Registry struct {
	tools         map[string]Tool
	workspaceRoot string
	mu            sync.RWMutex
}

// NewRegistry creates a new tool registry
func NewRegistry(workspaceRoot string) *Registry {
	r := &Registry{
		tools:         make(map[string]Tool),
		workspaceRoot: workspaceRoot,
	}

	// Register built-in tools
	r.Register(&FSListTool{root: workspaceRoot})
	r.Register(&FSReadTool{root: workspaceRoot})
	r.Register(&FSWriteTool{root: workspaceRoot})
	r.Register(&FSPatchTool{root: workspaceRoot})
	r.Register(&RgSearchTool{root: workspaceRoot})
	r.Register(&ExecTool{root: workspaceRoot})
	r.Register(&GitStatusTool{root: workspaceRoot})
	r.Register(&GitDiffTool{root: workspaceRoot})
	r.Register(&GitCheckoutTool{root: workspaceRoot})
	r.Register(&GitCreateBranchTool{root: workspaceRoot})
	r.Register(&GitCommitTool{root: workspaceRoot})
	r.Register(&GitResetHardTool{root: workspaceRoot})

	return r
}

// Register adds a tool to the registry
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get returns a tool by name
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns all registered tool names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Execute executes a tool by name with the given arguments
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool.Execute(ctx, args)
}

// ToOllamaTools converts registered tools to Ollama tool format
func (r *Registry) ToOllamaTools() []ollama.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]ollama.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, ollama.Tool{
			Type: "function",
			Function: ollama.ToolDef{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			},
		})
	}
	return tools
}

// WorkspaceRoot returns the workspace root path
func (r *Registry) WorkspaceRoot() string {
	return r.workspaceRoot
}
