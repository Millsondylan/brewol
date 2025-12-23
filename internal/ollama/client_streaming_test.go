package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestClient_ChatStream_WithMetrics verifies that streaming responses capture metrics correctly
func TestClient_ChatStream_WithMetrics(t *testing.T) {
	mockResp := mockResponse{
		ContentChunks:   []string{"Hello", " ", "world"},
		PromptEvalCount: 250,
		EvalCount:       75,
	}

	server := newMockServer(mockResp)
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()
	ch, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var finalMetrics *TokenMetrics
	contentCount := 0

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}

		if chunk.Response.Message.Content != "" {
			contentCount++
		}

		if chunk.Response.Done {
			finalMetrics = chunk.Metrics
		}
	}

	if contentCount != 3 {
		t.Errorf("expected 3 content chunks, got %d", contentCount)
	}

	if finalMetrics == nil {
		t.Fatal("expected metrics on final chunk")
	}

	if finalMetrics.PromptEvalCount != 250 {
		t.Errorf("expected PromptEvalCount=250, got %d", finalMetrics.PromptEvalCount)
	}

	if finalMetrics.EvalCount != 75 {
		t.Errorf("expected EvalCount=75, got %d", finalMetrics.EvalCount)
	}

	// Verify metrics are stored in client
	storedMetrics := c.GetLastMetrics()
	if storedMetrics == nil {
		t.Fatal("expected metrics to be stored in client")
	}

	if storedMetrics.PromptEvalCount != 250 {
		t.Errorf("expected stored PromptEvalCount=250, got %d", storedMetrics.PromptEvalCount)
	}
}

// TestClient_ChatStream_WithThinkingAndContent verifies thinking content is separated from answer content
func TestClient_ChatStream_WithThinkingAndContent(t *testing.T) {
	mockResp := mockResponse{
		ThinkingChunks:  []string{"Let me think...", " about this problem."},
		ContentChunks:   []string{"Here is my answer."},
		PromptEvalCount: 150,
		EvalCount:       75,
	}

	server := newMockServer(mockResp)
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "deepseek-r1")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()
	c.SetThinkMode(ThinkModeOn)

	ch, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var thinkingChunks []string
	var contentChunks []string
	thinkingPhaseCount := 0
	contentPhaseCount := 0

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}

		if chunk.IsThinking && chunk.ThinkingContent != "" {
			thinkingChunks = append(thinkingChunks, chunk.ThinkingContent)
			thinkingPhaseCount++
		}

		if !chunk.IsThinking && chunk.Response.Message.Content != "" {
			contentChunks = append(contentChunks, chunk.Response.Message.Content)
			contentPhaseCount++
		}

		if chunk.Response.Done {
			break
		}
	}

	if len(thinkingChunks) != 2 {
		t.Errorf("expected 2 thinking chunks, got %d: %v", len(thinkingChunks), thinkingChunks)
	}

	if len(contentChunks) != 1 {
		t.Errorf("expected 1 content chunk, got %d: %v", len(contentChunks), contentChunks)
	}

	// Verify thinking trace is separate
	if thinkingChunks[0] != "Let me think..." {
		t.Errorf("expected first thinking chunk to be 'Let me think...', got %q", thinkingChunks[0])
	}
}

// TestClient_ChatStream_WithToolCalls verifies tool call parsing from streamed responses
func TestClient_ChatStream_WithToolCalls(t *testing.T) {
	mockResp := mockResponse{
		ContentChunks: []string{"I will execute the following tools:"},
		ToolCalls: []ToolCall{
			{
				Function: ToolFunction{
					Name:      "fs_read",
					Arguments: json.RawMessage(`{"path": "README.md"}`),
				},
			},
		},
		PromptEvalCount: 120,
		EvalCount:       60,
	}

	server := newMockServer(mockResp)
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()
	ch, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "Read README"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var finalMessage Message
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}

		if chunk.Response.Done {
			finalMessage = chunk.Response.Message
		}
	}

	if len(finalMessage.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(finalMessage.ToolCalls))
	}

	toolCall := finalMessage.ToolCalls[0]
	if toolCall.Function.Name != "fs_read" {
		t.Errorf("expected tool call name 'fs_read', got %q", toolCall.Function.Name)
	}

	var args map[string]string
	if err := json.Unmarshal(toolCall.Function.Arguments, &args); err != nil {
		t.Fatalf("failed to unmarshal arguments: %v", err)
	}

	if args["path"] != "README.md" {
		t.Errorf("expected path 'README.md', got %q", args["path"])
	}
}

// TestClient_Chat_WithMetrics verifies non-streaming responses capture metrics
func TestClient_Chat_WithMetrics(t *testing.T) {
	mockResp := mockResponse{
		ContentChunks:   []string{"Complete response"},
		PromptEvalCount: 300,
		EvalCount:       100,
	}

	server := newMockServer(mockResp)
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()
	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.PromptEvalCount != 300 {
		t.Errorf("expected PromptEvalCount=300, got %d", resp.PromptEvalCount)
	}

	if resp.EvalCount != 100 {
		t.Errorf("expected EvalCount=100, got %d", resp.EvalCount)
	}

	// Verify metrics stored in client
	metrics := c.GetLastMetrics()
	if metrics == nil {
		t.Fatal("expected metrics to be stored")
	}

	if metrics.PromptEvalCount != 300 {
		t.Errorf("expected stored PromptEvalCount=300, got %d", metrics.PromptEvalCount)
	}
}

// TestClient_ChatStream_ErrorResponse verifies error handling
func TestClient_ChatStream_ErrorResponse(t *testing.T) {
	mockResp := mockResponse{
		Error:        true,
		ErrorStatus:  http.StatusTooManyRequests,
		ErrorMessage: "rate limit exceeded",
	}

	server := newMockServer(mockResp)
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error for rate limit response")
	}

	// Should contain status code and message
	errStr := err.Error()
	if !contains(errStr, "429") && !contains(errStr, "rate limit") {
		t.Errorf("expected error to mention rate limit, got: %v", err)
	}
}

// TestClient_ChatStream_ContextCancellation verifies context cancellation stops streaming
func TestClient_ChatStream_ContextCancellation(t *testing.T) {
	mockResp := mockResponse{
		ContentChunks:   []string{"Hello", " ", "world", "!"},
		PromptEvalCount: 100,
		EvalCount:       50,
	}

	server := newMockServer(mockResp)
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	ch, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		// Context cancellation before request is acceptable
		return
	}

	// If request started, should get cancellation error in stream
	for chunk := range ch {
		if chunk.Error != nil {
			if chunk.Error == context.Canceled {
				return // Expected
			}
			t.Fatalf("unexpected error: %v", chunk.Error)
		}
	}
}

// TestClient_SetNumCtx verifies context window size configuration
func TestClient_SetNumCtx(t *testing.T) {
	c := NewClient()

	// Initially 0
	if c.GetNumCtx() != 0 {
		t.Errorf("expected initial numCtx=0, got %d", c.GetNumCtx())
	}

	// Set to 16384
	c.SetNumCtx(16384)
	if c.GetNumCtx() != 16384 {
		t.Errorf("expected numCtx=16384, got %d", c.GetNumCtx())
	}

	// GetModelContextSize should return the set value
	if c.GetModelContextSize() != 16384 {
		t.Errorf("expected model context size=16384, got %d", c.GetModelContextSize())
	}
}

// contains is a helper to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
