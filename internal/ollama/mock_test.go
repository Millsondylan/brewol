package ollama

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
)

// mockResponse represents a mock Ollama response configuration
type mockResponse struct {
	ThinkingChunks  []string // Thinking content chunks (if any)
	ContentChunks   []string // Response content chunks
	ToolCalls       []ToolCall
	PromptEvalCount int
	EvalCount       int
	Error           bool   // Return error response
	ErrorStatus     int    // HTTP status code for error
	ErrorMessage    string // Error message
}

// newMockServer creates a new httptest server that mocks Ollama API responses
func newMockServer(response mockResponse) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			handleTagsMock(w, r)
			return
		}

		if r.URL.Path == "/api/chat" {
			handleChatMock(w, r, response)
			return
		}

		http.NotFound(w, r)
	}))
}

// handleTagsMock handles /api/tags endpoint
func handleTagsMock(w http.ResponseWriter, r *http.Request) {
	resp := TagsResponse{
		Models: []ModelInfo{
			{Name: "test-model", Size: 1000000},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleChatMock handles /api/chat endpoint
func handleChatMock(w http.ResponseWriter, r *http.Request, response mockResponse) {
	// Use a generic map to avoid ThinkValue unmarshaling issues in tests
	var reqMap map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqMap); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Extract stream mode
	stream := false
	if s, ok := reqMap["stream"].(bool); ok {
		stream = s
	}

	// Extract model
	model := "test-model"
	if m, ok := reqMap["model"].(string); ok {
		model = m
	}

	req := ChatRequest{
		Model:  model,
		Stream: stream,
	}

	// Return error if configured
	if response.Error {
		status := response.ErrorStatus
		if status == 0 {
			status = http.StatusInternalServerError
		}
		http.Error(w, response.ErrorMessage, status)
		return
	}

	// Handle streaming response
	if req.Stream {
		handleStreamingChatMock(w, req, response)
		return
	}

	// Handle non-streaming response
	handleNonStreamingChatMock(w, req, response)
}

// handleStreamingChatMock handles streaming chat responses
func handleStreamingChatMock(w http.ResponseWriter, req ChatRequest, response mockResponse) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Send thinking chunks first (if any)
	for _, thinkingChunk := range response.ThinkingChunks {
		chunk := ChatResponse{
			Model:   req.Model,
			Message: Message{Role: "assistant", Thinking: thinkingChunk},
			Done:    false,
		}
		data, _ := json.Marshal(chunk)
		w.Write(data)
		w.Write([]byte("\n"))
		flusher.Flush()
	}

	// Send content chunks
	for i, contentChunk := range response.ContentChunks {
		isLast := i == len(response.ContentChunks)-1

		msg := Message{
			Role:    "assistant",
			Content: contentChunk,
		}

		// Add tool calls on the last content chunk if provided
		if isLast && len(response.ToolCalls) > 0 {
			msg.ToolCalls = response.ToolCalls
		}

		chunk := ChatResponse{
			Model:   req.Model,
			Message: msg,
			Done:    false,
		}

		if isLast {
			chunk.Done = true
			chunk.PromptEvalCount = response.PromptEvalCount
			chunk.EvalCount = response.EvalCount
		}

		data, _ := json.Marshal(chunk)
		w.Write(data)
		w.Write([]byte("\n"))
		flusher.Flush()
	}
}

// handleNonStreamingChatMock handles non-streaming chat responses
func handleNonStreamingChatMock(w http.ResponseWriter, req ChatRequest, response mockResponse) {
	// Combine all content chunks
	fullContent := strings.Join(response.ContentChunks, "")

	msg := Message{
		Role:    "assistant",
		Content: fullContent,
	}

	// Add tool calls if provided
	if len(response.ToolCalls) > 0 {
		msg.ToolCalls = response.ToolCalls
	}

	resp := ChatResponse{
		Model:           req.Model,
		Message:         msg,
		Done:            true,
		PromptEvalCount: response.PromptEvalCount,
		EvalCount:       response.EvalCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
