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

// Message represents a chat message
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
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
	Type     string       `json:"type"`
	Function ToolDef      `json:"function"`
}

// ToolDef represents a tool function definition
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ChatRequest represents a chat API request
type ChatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream"`
	Tools     []Tool    `json:"tools,omitempty"`
	KeepAlive int       `json:"keep_alive,omitempty"`
}

// ChatResponse represents a streaming chat response chunk
type ChatResponse struct {
	Model              string    `json:"model"`
	CreatedAt          string    `json:"created_at"`
	Message            Message   `json:"message"`
	Done               bool      `json:"done"`
	TotalDuration      int64     `json:"total_duration,omitempty"`
	LoadDuration       int64     `json:"load_duration,omitempty"`
	PromptEvalCount    int       `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64     `json:"prompt_eval_duration,omitempty"`
	EvalCount          int       `json:"eval_count,omitempty"`
	EvalDuration       int64     `json:"eval_duration,omitempty"`
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

// StreamChunk represents a chunk from the streaming response
type StreamChunk struct {
	Response    ChatResponse
	Error       error
	TokensPerSec float64
}

// Client is the Ollama API client
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	model      string
	mu         sync.RWMutex
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
		model: os.Getenv("OLLAMA_MODEL"),
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
	c.mu.RUnlock()

	if model == "" {
		return nil, fmt.Errorf("no model selected; use SetModel() or set OLLAMA_MODEL")
	}

	chatReq := ChatRequest{
		Model:     model,
		Messages:  messages,
		Stream:    true,
		Tools:     tools,
		KeepAlive: -1, // Keep model hot
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

			ch <- StreamChunk{
				Response:     chatResp,
				TokensPerSec: tokensPerSec,
			}

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
	c.mu.RUnlock()

	if model == "" {
		return nil, fmt.Errorf("no model selected; use SetModel() or set OLLAMA_MODEL")
	}

	chatReq := ChatRequest{
		Model:     model,
		Messages:  messages,
		Stream:    false,
		Tools:     tools,
		KeepAlive: -1,
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

	return &chatResp, nil
}

// Preload sends a keep_alive request to load a model
func (c *Client) Preload(ctx context.Context, model string) error {
	chatReq := ChatRequest{
		Model:     model,
		Messages:  []Message{{Role: "user", Content: ""}},
		Stream:    false,
		KeepAlive: -1,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

// Unload sends a keep_alive: 0 request to unload a model
func (c *Client) Unload(ctx context.Context, model string) error {
	chatReq := ChatRequest{
		Model:     model,
		Messages:  []Message{{Role: "user", Content: ""}},
		Stream:    false,
		KeepAlive: 0,
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	return nil
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
