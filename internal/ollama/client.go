// Package ollama provides a client for the Ollama API with streaming and tool calling support.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Default configuration
const (
	DefaultLocalBaseURL = "http://localhost:11434"
	DefaultCloudBaseURL = "https://ollama.com"
	DefaultTimeout      = 120 * time.Second
)

// Known model context sizes (in tokens)
// These are defaults when the model info API doesn't provide context size
var knownModelContextSizes = map[string]int{
	// Cloud models (typically 128k+)
	"gemini":                 1048576, // 1M tokens
	"gemini-2.0-flash":       1048576,
	"gemini-2.5-flash":       1048576,
	"gemini-3-flash":         1048576,
	"gemini-3-flash-preview": 1048576,
	"gpt-4o":                 128000,
	"gpt-4o-mini":            128000,
	"gpt-4-turbo":            128000,
	"claude-3":               200000,
	"claude-3.5":             200000,
	"claude-3-opus":          200000,
	"claude-3-sonnet":        200000,
	"claude-3.5-sonnet":      200000,
	"claude-4-sonnet":        200000,

	// Local Ollama models (common defaults)
	"llama3":         8192,
	"llama3.1":       131072, // 128k
	"llama3.2":       131072,
	"llama3.3":       131072,
	"llama2":         4096,
	"mistral":        32768,
	"mixtral":        32768,
	"codellama":      16384,
	"deepseek":       65536, // 64k
	"deepseek-coder": 65536,
	"deepseek-r1":    131072,
	"qwen":           32768,
	"qwen2":          131072,
	"qwen2.5":        131072,
	"qwen3":          131072,
	"phi3":           131072,
	"phi4":           16384,
	"command-r":      131072,
	"command-r-plus": 131072,
}

// Message represents a chat message
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"` // Thinking trace (reasoning tokens)
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool call from the model
type ToolCall struct {
	Function ToolFunction `json:"function"`
}

// ToolFunction represents the function details of a tool call
type ToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Tool represents a tool definition for the model
type Tool struct {
	Type     string  `json:"type"`
	Function ToolDef `json:"function"`
}

// ToolDef represents a tool function definition
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ThinkMode represents the thinking mode setting
type ThinkMode string

const (
	ThinkModeAuto   ThinkMode = "auto"
	ThinkModeOn     ThinkMode = "on"
	ThinkModeOff    ThinkMode = "off"
	ThinkModeLow    ThinkMode = "low"
	ThinkModeMedium ThinkMode = "medium"
	ThinkModeHigh   ThinkMode = "high"
)

// ChatOptions represents model options for the chat request
type ChatOptions struct {
	NumCtx      int     `json:"num_ctx,omitempty"`     // Context window size
	Temperature float64 `json:"temperature,omitempty"` // Sampling temperature
	NumPredict  int     `json:"num_predict,omitempty"` // Max tokens to generate
}

// ThinkValue represents the think field which can be boolean or string
type ThinkValue struct {
	value interface{} // bool or string
}

// MarshalJSON marshals ThinkValue to JSON (bool or string)
func (t ThinkValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.value)
}

// NewThinkValueBool creates a ThinkValue with boolean
func NewThinkValueBool(v bool) ThinkValue {
	return ThinkValue{value: v}
}

// NewThinkValueString creates a ThinkValue with string level
func NewThinkValueString(level string) ThinkValue {
	return ThinkValue{value: level}
}

// ChatRequest represents a chat API request
type ChatRequest struct {
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	Stream   bool         `json:"stream"`
	Options  *ChatOptions `json:"options,omitempty"`
	Think    *ThinkValue  `json:"think,omitempty"` // Thinking mode: true/false or "low"/"medium"/"high"
}

// ChatResponse represents a streaming chat response chunk
type ChatResponse struct {
	Model              string  `json:"model"`
	CreatedAt          string  `json:"created_at"`
	Message            Message `json:"message"`
	Done               bool    `json:"done"`
	TotalDuration      int64   `json:"total_duration,omitempty"`
	LoadDuration       int64   `json:"load_duration,omitempty"`
	PromptEvalCount    int     `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64   `json:"prompt_eval_duration,omitempty"`
	EvalCount          int     `json:"eval_count,omitempty"`
	EvalDuration       int64   `json:"eval_duration,omitempty"`
}

// ModelInfo represents information about a model
type ModelInfo struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
}

// TagsResponse represents the response from /api/tags
type TagsResponse struct {
	Models []ModelInfo `json:"models"`
}

// TokenMetrics contains token usage metrics from a response
type TokenMetrics struct {
	PromptEvalCount    int     // Number of tokens in the prompt
	EvalCount          int     // Number of tokens generated
	PromptEvalDuration int64   // Time spent evaluating prompt (ns)
	EvalDuration       int64   // Time spent generating (ns)
	TotalDuration      int64   // Total time (ns)
	TokensPerSec       float64 // Tokens generated per second
}

// StreamChunk represents a chunk from the streaming response
type StreamChunk struct {
	Response        ChatResponse
	Error           error
	TokensPerSec    float64
	Metrics         *TokenMetrics // Final metrics (only set on done=true)
	ThinkingContent string        // Thinking tokens from this chunk
	IsThinking      bool          // True if currently in thinking phase
}

// Client is the Ollama API client
type Client struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	model       string
	numCtx      int           // Context window size (0 = use model default)
	thinkMode   ThinkMode     // Thinking mode setting
	lastMetrics *TokenMetrics // Last token metrics from a request
	mu          sync.RWMutex
}

// NewClient creates a new Ollama client
func NewClient() *Client {
	baseURL := os.Getenv("OLLAMA_HOST")
	if baseURL == "" {
		baseURL = DefaultLocalBaseURL
	}

	// Check for cloud endpoint
	apiKey := os.Getenv("OLLAMA_API_KEY")
	if apiKey != "" && baseURL == DefaultLocalBaseURL {
		baseURL = DefaultCloudBaseURL
	}

	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		model:     os.Getenv("OLLAMA_MODEL"),
		thinkMode: ThinkModeAuto,
	}
}

// SetModel sets the current model
func (c *Client) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
}

// GetModel returns the current model
func (c *Client) GetModel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

// SetNumCtx sets the context window size
func (c *Client) SetNumCtx(numCtx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.numCtx = numCtx
}

// GetNumCtx returns the current context window size
func (c *Client) GetNumCtx() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.numCtx
}

// GetModelContextSize returns the context size for the current model
// It first checks if a custom numCtx is set, then looks up known model sizes,
// and finally returns a default value
func (c *Client) GetModelContextSize() int {
	c.mu.RLock()
	numCtx := c.numCtx
	model := c.model
	c.mu.RUnlock()

	// If explicitly set, use that
	if numCtx > 0 {
		return numCtx
	}

	// Look up model in known sizes
	return LookupModelContextSize(model)
}

// LookupModelContextSize returns the context size for a model name
// It checks exact matches first, then prefix matches
func LookupModelContextSize(model string) int {
	if model == "" {
		return 8192 // default fallback
	}

	// Normalize model name (remove :tag suffix for matching)
	baseName := model
	if idx := strings.Index(model, ":"); idx > 0 {
		baseName = model[:idx]
	}
	baseName = strings.ToLower(baseName)

	// Check exact match
	if size, ok := knownModelContextSizes[baseName]; ok {
		return size
	}

	// Check prefix matches (e.g., "gemini-3-flash-preview" matches "gemini")
	for pattern, size := range knownModelContextSizes {
		if strings.HasPrefix(baseName, pattern) {
			return size
		}
	}

	// Check if it contains "cloud" tag - likely a large context cloud model
	if strings.Contains(strings.ToLower(model), ":cloud") {
		return 131072 // 128k default for cloud models
	}

	// Default fallback
	return 8192
}

// SetThinkMode sets the thinking mode
func (c *Client) SetThinkMode(mode ThinkMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.thinkMode = mode
}

// GetThinkMode returns the current thinking mode
func (c *Client) GetThinkMode() ThinkMode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.thinkMode
}

// IsThinkingCapable returns true if the current model supports thinking
// This checks for known thinking-capable model patterns
func (c *Client) IsThinkingCapable() bool {
	c.mu.RLock()
	model := c.model
	c.mu.RUnlock()

	// Known thinking-capable model patterns
	thinkingModels := []string{
		"deepseek",
		"qwq",
		"gpt-oss",
		"thinking",
		"reason",
		"o1",
		"o3",
	}

	modelLower := strings.ToLower(model)
	for _, pattern := range thinkingModels {
		if strings.Contains(modelLower, pattern) {
			return true
		}
	}
	return false
}

// buildThinkValue builds the think field value based on mode and model
func (c *Client) buildThinkValue() *ThinkValue {
	c.mu.RLock()
	mode := c.thinkMode
	model := c.model
	c.mu.RUnlock()

	switch mode {
	case ThinkModeOff:
		v := NewThinkValueBool(false)
		return &v
	case ThinkModeOn:
		v := NewThinkValueBool(true)
		return &v
	case ThinkModeLow:
		v := NewThinkValueString("low")
		return &v
	case ThinkModeMedium:
		v := NewThinkValueString("medium")
		return &v
	case ThinkModeHigh:
		v := NewThinkValueString("high")
		return &v
	case ThinkModeAuto:
		// Auto mode: check if model is GPT-OSS or thinking-capable
		modelLower := strings.ToLower(model)
		if strings.Contains(modelLower, "gpt-oss") {
			v := NewThinkValueString("medium")
			return &v
		}
		if c.IsThinkingCapable() {
			v := NewThinkValueBool(true)
			return &v
		}
		// Not a thinking model - omit think field
		return nil
	default:
		return nil
	}
}

// GetLastMetrics returns the token metrics from the last request
func (c *Client) GetLastMetrics() *TokenMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastMetrics == nil {
		return nil
	}
	// Return a copy
	m := *c.lastMetrics
	return &m
}

// ListModels fetches available models from /api/tags
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var tagsResp TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tagsResp.Models, nil
}

// ChatStream sends a chat request and returns a channel for streaming responses
func (c *Client) ChatStream(ctx context.Context, messages []Message, tools []Tool) (<-chan StreamChunk, error) {
	c.mu.RLock()
	model := c.model
	numCtx := c.numCtx
	c.mu.RUnlock()

	if model == "" {
		return nil, fmt.Errorf("no model selected; use SetModel() or set OLLAMA_MODEL")
	}

	// Keep messages minimal - truncate content if too long
	// IMPORTANT: Do NOT include thinking field in outgoing messages
	var trimmedMessages []Message
	for _, m := range messages {
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "..."
		}
		trimmedMessages = append(trimmedMessages, Message{
			Role:    m.Role,
			Content: content,
			// Thinking is intentionally omitted - it's UI-only
		})
	}

	chatReq := ChatRequest{
		Model:    model,
		Messages: trimmedMessages,
		Stream:   true,
	}

	// Add options if context size is set
	if numCtx > 0 {
		chatReq.Options = &ChatOptions{
			NumCtx: numCtx,
		}
	}

	// Add think field based on thinkMode
	chatReq.Think = c.buildThinkValue()

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log request size for debugging
	if len(body) > 50000 {
		return nil, fmt.Errorf("request too large (%d bytes), reduce context", len(body))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk, 100)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

		startTime := time.Now()
		totalTokens := 0

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				ch <- StreamChunk{Error: ctx.Err()}
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			var chatResp ChatResponse
			if err := json.Unmarshal([]byte(line), &chatResp); err != nil {
				ch <- StreamChunk{Error: fmt.Errorf("failed to decode chunk: %w", err)}
				continue
			}

			// Calculate tokens per second
			totalTokens++
			elapsed := time.Since(startTime).Seconds()
			tokensPerSec := float64(totalTokens) / elapsed
			if elapsed < 0.1 {
				tokensPerSec = 0
			}

			chunk := StreamChunk{
				Response:     chatResp,
				TokensPerSec: tokensPerSec,
			}

			// Check if this chunk contains thinking content
			if chatResp.Message.Thinking != "" {
				chunk.ThinkingContent = chatResp.Message.Thinking
				chunk.IsThinking = true
			}

			// Capture final metrics on done=true
			if chatResp.Done {
				metrics := &TokenMetrics{
					PromptEvalCount:    chatResp.PromptEvalCount,
					EvalCount:          chatResp.EvalCount,
					PromptEvalDuration: chatResp.PromptEvalDuration,
					EvalDuration:       chatResp.EvalDuration,
					TotalDuration:      chatResp.TotalDuration,
					TokensPerSec:       tokensPerSec,
				}
				chunk.Metrics = metrics

				// Store metrics in client
				c.mu.Lock()
				c.lastMetrics = metrics
				c.mu.Unlock()
			}

			ch <- chunk

			if chatResp.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Error: fmt.Errorf("scanner error: %w", err)}
		}
	}()

	return ch, nil
}

// Chat sends a non-streaming chat request
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	c.mu.RLock()
	model := c.model
	numCtx := c.numCtx
	c.mu.RUnlock()

	if model == "" {
		return nil, fmt.Errorf("no model selected; use SetModel() or set OLLAMA_MODEL")
	}

	// Strip Thinking field from messages - it's UI-only
	var cleanMessages []Message
	for _, m := range messages {
		cleanMessages = append(cleanMessages, Message{
			Role:      m.Role,
			Content:   m.Content,
			ToolCalls: m.ToolCalls,
			// Thinking is intentionally omitted - it's UI-only
		})
	}

	chatReq := ChatRequest{
		Model:    model,
		Messages: cleanMessages,
		Stream:   false,
	}

	// Add options if context size is set
	if numCtx > 0 {
		chatReq.Options = &ChatOptions{
			NumCtx: numCtx,
		}
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Store metrics
	c.mu.Lock()
	c.lastMetrics = &TokenMetrics{
		PromptEvalCount:    chatResp.PromptEvalCount,
		EvalCount:          chatResp.EvalCount,
		PromptEvalDuration: chatResp.PromptEvalDuration,
		EvalDuration:       chatResp.EvalDuration,
		TotalDuration:      chatResp.TotalDuration,
	}
	c.mu.Unlock()

	return &chatResp, nil
}

// IsAvailable checks if Ollama is reachable
func (c *Client) IsAvailable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// BaseURL returns the current base URL
func (c *Client) BaseURL() string {
	return c.baseURL
}
