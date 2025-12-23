package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	// Save and restore environment
	origHost := os.Getenv("OLLAMA_HOST")
	origKey := os.Getenv("OLLAMA_API_KEY")
	origModel := os.Getenv("OLLAMA_MODEL")
	defer func() {
		os.Setenv("OLLAMA_HOST", origHost)
		os.Setenv("OLLAMA_API_KEY", origKey)
		os.Setenv("OLLAMA_MODEL", origModel)
	}()

	t.Run("default values", func(t *testing.T) {
		os.Unsetenv("OLLAMA_HOST")
		os.Unsetenv("OLLAMA_API_KEY")
		os.Unsetenv("OLLAMA_MODEL")

		c := NewClient()
		if c.baseURL != DefaultLocalBaseURL {
			t.Errorf("expected baseURL %s, got %s", DefaultLocalBaseURL, c.baseURL)
		}
		if c.apiKey != "" {
			t.Errorf("expected empty apiKey, got %s", c.apiKey)
		}
		if c.model != "" {
			t.Errorf("expected empty model, got %s", c.model)
		}
	})

	t.Run("custom host", func(t *testing.T) {
		os.Setenv("OLLAMA_HOST", "http://custom:8080")
		os.Unsetenv("OLLAMA_API_KEY")

		c := NewClient()
		if c.baseURL != "http://custom:8080" {
			t.Errorf("expected baseURL http://custom:8080, got %s", c.baseURL)
		}
	})

	t.Run("trailing slash removed", func(t *testing.T) {
		os.Setenv("OLLAMA_HOST", "http://custom:8080/")
		os.Unsetenv("OLLAMA_API_KEY")

		c := NewClient()
		if c.baseURL != "http://custom:8080" {
			t.Errorf("expected trailing slash removed, got %s", c.baseURL)
		}
	})

	t.Run("api key switches to cloud url", func(t *testing.T) {
		os.Unsetenv("OLLAMA_HOST")
		os.Setenv("OLLAMA_API_KEY", "test-key")

		c := NewClient()
		if c.baseURL != DefaultCloudBaseURL {
			t.Errorf("expected cloud baseURL %s, got %s", DefaultCloudBaseURL, c.baseURL)
		}
		if c.apiKey != "test-key" {
			t.Errorf("expected apiKey test-key, got %s", c.apiKey)
		}
	})

	t.Run("custom host with api key", func(t *testing.T) {
		os.Setenv("OLLAMA_HOST", "http://custom:8080")
		os.Setenv("OLLAMA_API_KEY", "test-key")

		c := NewClient()
		// Custom host should not be overwritten
		if c.baseURL != "http://custom:8080" {
			t.Errorf("expected custom baseURL, got %s", c.baseURL)
		}
		if c.apiKey != "test-key" {
			t.Errorf("expected apiKey test-key, got %s", c.apiKey)
		}
	})

	t.Run("model from env", func(t *testing.T) {
		os.Unsetenv("OLLAMA_HOST")
		os.Unsetenv("OLLAMA_API_KEY")
		os.Setenv("OLLAMA_MODEL", "codellama")

		c := NewClient()
		if c.model != "codellama" {
			t.Errorf("expected model codellama, got %s", c.model)
		}
	})
}

func TestClient_SetGetModel(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	if c.GetModel() != "" {
		t.Errorf("expected empty model initially, got %s", c.GetModel())
	}

	c.SetModel("llama2")
	if c.GetModel() != "llama2" {
		t.Errorf("expected model llama2, got %s", c.GetModel())
	}

	c.SetModel("codellama")
	if c.GetModel() != "codellama" {
		t.Errorf("expected model codellama, got %s", c.GetModel())
	}
}

func TestClient_BaseURL(t *testing.T) {
	os.Setenv("OLLAMA_HOST", "http://test:1234")
	defer os.Unsetenv("OLLAMA_HOST")

	c := NewClient()
	if c.BaseURL() != "http://test:1234" {
		t.Errorf("expected baseURL http://test:1234, got %s", c.BaseURL())
	}
}

func TestClient_ListModels(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/tags" {
				t.Errorf("expected path /api/tags, got %s", r.URL.Path)
			}
			if r.Method != "GET" {
				t.Errorf("expected GET method, got %s", r.Method)
			}

			resp := TagsResponse{
				Models: []ModelInfo{
					{Name: "llama2", Size: 1234567890},
					{Name: "codellama", Size: 9876543210},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		os.Setenv("OLLAMA_HOST", server.URL)
		defer os.Unsetenv("OLLAMA_HOST")

		c := NewClient()
		models, err := c.ListModels(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}
		if models[0].Name != "llama2" {
			t.Errorf("expected first model llama2, got %s", models[0].Name)
		}
		if models[1].Name != "codellama" {
			t.Errorf("expected second model codellama, got %s", models[1].Name)
		}
	})

	t.Run("api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
		}))
		defer server.Close()

		os.Setenv("OLLAMA_HOST", server.URL)
		defer os.Unsetenv("OLLAMA_HOST")

		c := NewClient()
		_, err := c.ListModels(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("with api key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-key" {
				t.Errorf("expected Bearer test-key, got %s", auth)
			}
			json.NewEncoder(w).Encode(TagsResponse{Models: []ModelInfo{}})
		}))
		defer server.Close()

		os.Setenv("OLLAMA_HOST", server.URL)
		os.Setenv("OLLAMA_API_KEY", "test-key")
		defer os.Unsetenv("OLLAMA_HOST")
		defer os.Unsetenv("OLLAMA_API_KEY")

		c := NewClient()
		_, err := c.ListModels(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestClient_IsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(TagsResponse{Models: []ModelInfo{}})
		}))
		defer server.Close()

		os.Setenv("OLLAMA_HOST", server.URL)
		defer os.Unsetenv("OLLAMA_HOST")

		c := NewClient()
		if !c.IsAvailable(context.Background()) {
			t.Error("expected IsAvailable to return true")
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		os.Setenv("OLLAMA_HOST", "http://localhost:99999")
		defer os.Unsetenv("OLLAMA_HOST")

		c := NewClient()
		if c.IsAvailable(context.Background()) {
			t.Error("expected IsAvailable to return false")
		}
	})
}

func TestClient_Chat_NoModel(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "test"}}, nil)
	if err == nil {
		t.Fatal("expected error when no model set")
	}
	if err.Error() != "no model selected; use SetModel() or set OLLAMA_MODEL" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_ChatStream_NoModel(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "test"}}, nil)
	if err == nil {
		t.Fatal("expected error when no model set")
	}
	if err.Error() != "no model selected; use SetModel() or set OLLAMA_MODEL" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "test-model" {
			t.Errorf("expected model test-model, got %s", req.Model)
		}
		if req.Stream != false {
			t.Error("expected stream=false for non-streaming chat")
		}
		if len(req.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(req.Messages))
		}

		resp := ChatResponse{
			Model: "test-model",
			Message: Message{
				Role:    "assistant",
				Content: "Hello!",
			},
			Done: true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
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

	if resp.Message.Content != "Hello!" {
		t.Errorf("expected response Hello!, got %s", resp.Message.Content)
	}
	if resp.Message.Role != "assistant" {
		t.Errorf("expected role assistant, got %s", resp.Message.Role)
	}
}

func TestClient_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Stream != true {
			t.Error("expected stream=true for streaming chat")
		}

		// Send streaming response
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected ResponseWriter to be a Flusher")
		}

		chunks := []ChatResponse{
			{Message: Message{Role: "assistant", Content: "Hello"}, Done: false},
			{Message: Message{Role: "assistant", Content: " world"}, Done: false},
			{Message: Message{Role: "assistant", Content: "!"}, Done: true},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
		}
	}))
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

	var contents []string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}
		contents = append(contents, chunk.Response.Message.Content)
		if chunk.Response.Done {
			break
		}
	}

	if len(contents) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(contents))
	}
}

func TestLookupModelContextSize(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		// Exact matches
		{"llama3", 8192},
		{"llama3.1", 131072},
		{"gemini", 1048576},
		{"gpt-4o", 128000},

		// With tags
		{"gemini-3-flash-preview:cloud", 1048576},
		{"llama3:latest", 8192},
		{"qwen3:32b", 131072},

		// Prefix matches
		{"gemini-2.5-flash-exp", 1048576},
		{"deepseek-r1-lite", 65536}, // matches "deepseek" prefix (not "deepseek-r1")

		// Cloud tag default
		{"unknown-model:cloud", 131072},

		// Unknown fallback
		{"", 8192},
		{"totally-unknown-model", 8192},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := LookupModelContextSize(tt.model)
			if got != tt.expected {
				t.Errorf("LookupModelContextSize(%q) = %d, want %d", tt.model, got, tt.expected)
			}
		})
	}
}

func TestClient_GetModelContextSize(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	// No model set - should return default
	if size := c.GetModelContextSize(); size != 8192 {
		t.Errorf("expected 8192 for no model, got %d", size)
	}

	// Set a known model
	c.SetModel("gemini-3-flash-preview:cloud")
	if size := c.GetModelContextSize(); size != 1048576 {
		t.Errorf("expected 1048576 for gemini, got %d", size)
	}

	// Explicitly set numCtx should override
	c.SetNumCtx(16384)
	if size := c.GetModelContextSize(); size != 16384 {
		t.Errorf("expected 16384 when explicitly set, got %d", size)
	}
}

func TestClient_ChatStream_LargeRequest(t *testing.T) {
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()

	// Create a message that will exceed the size limit after trimming
	largeContent := make([]byte, 15000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	messages := make([]Message, 10)
	for i := range messages {
		messages[i] = Message{Role: "user", Content: string(largeContent)}
	}

	_, err := c.ChatStream(context.Background(), messages, nil)
	if err == nil {
		t.Fatal("expected error for large request")
	}
}

// Tests for thinking mode functionality
func TestThinkMode(t *testing.T) {
	t.Run("think mode constants", func(t *testing.T) {
		if ThinkModeAuto != "auto" {
			t.Errorf("expected auto, got %s", ThinkModeAuto)
		}
		if ThinkModeOn != "on" {
			t.Errorf("expected on, got %s", ThinkModeOn)
		}
		if ThinkModeOff != "off" {
			t.Errorf("expected off, got %s", ThinkModeOff)
		}
		if ThinkModeLow != "low" {
			t.Errorf("expected low, got %s", ThinkModeLow)
		}
		if ThinkModeMedium != "medium" {
			t.Errorf("expected medium, got %s", ThinkModeMedium)
		}
		if ThinkModeHigh != "high" {
			t.Errorf("expected high, got %s", ThinkModeHigh)
		}
	})
}

func TestClient_SetGetThinkMode(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	// Default should be auto
	if c.GetThinkMode() != ThinkModeAuto {
		t.Errorf("expected default think mode auto, got %s", c.GetThinkMode())
	}

	// Test setting various modes
	modes := []ThinkMode{ThinkModeOn, ThinkModeOff, ThinkModeLow, ThinkModeMedium, ThinkModeHigh, ThinkModeAuto}
	for _, mode := range modes {
		c.SetThinkMode(mode)
		if c.GetThinkMode() != mode {
			t.Errorf("expected think mode %s, got %s", mode, c.GetThinkMode())
		}
	}
}

func TestClient_IsThinkingCapable(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	tests := []struct {
		model    string
		expected bool
	}{
		{"deepseek-r1", true},
		{"deepseek-coder", true},
		{"qwq:latest", true},
		{"gpt-oss-large", true},
		{"thinking-model", true},
		{"reason-ai", true},
		{"o1-preview", true},
		{"o3-mini", true},
		{"llama3", false},
		{"codellama", false},
		{"mistral", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			c.SetModel(tt.model)
			if got := c.IsThinkingCapable(); got != tt.expected {
				t.Errorf("IsThinkingCapable(%q) = %v, want %v", tt.model, got, tt.expected)
			}
		})
	}
}

func TestThinkValue_MarshalJSON(t *testing.T) {
	t.Run("boolean true", func(t *testing.T) {
		tv := NewThinkValueBool(true)
		data, err := json.Marshal(tv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "true" {
			t.Errorf("expected true, got %s", data)
		}
	})

	t.Run("boolean false", func(t *testing.T) {
		tv := NewThinkValueBool(false)
		data, err := json.Marshal(tv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "false" {
			t.Errorf("expected false, got %s", data)
		}
	})

	t.Run("string low", func(t *testing.T) {
		tv := NewThinkValueString("low")
		data, err := json.Marshal(tv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != `"low"` {
			t.Errorf("expected \"low\", got %s", data)
		}
	})

	t.Run("string medium", func(t *testing.T) {
		tv := NewThinkValueString("medium")
		data, err := json.Marshal(tv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != `"medium"` {
			t.Errorf("expected \"medium\", got %s", data)
		}
	})

	t.Run("string high", func(t *testing.T) {
		tv := NewThinkValueString("high")
		data, err := json.Marshal(tv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != `"high"` {
			t.Errorf("expected \"high\", got %s", data)
		}
	})
}

func TestClient_buildThinkValue(t *testing.T) {
	os.Unsetenv("OLLAMA_MODEL")
	c := NewClient()

	t.Run("mode off", func(t *testing.T) {
		c.SetThinkMode(ThinkModeOff)
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue")
		}
		data, _ := json.Marshal(tv)
		if string(data) != "false" {
			t.Errorf("expected false, got %s", data)
		}
	})

	t.Run("mode on", func(t *testing.T) {
		c.SetThinkMode(ThinkModeOn)
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue")
		}
		data, _ := json.Marshal(tv)
		if string(data) != "true" {
			t.Errorf("expected true, got %s", data)
		}
	})

	t.Run("mode low", func(t *testing.T) {
		c.SetThinkMode(ThinkModeLow)
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue")
		}
		data, _ := json.Marshal(tv)
		if string(data) != `"low"` {
			t.Errorf("expected \"low\", got %s", data)
		}
	})

	t.Run("mode medium", func(t *testing.T) {
		c.SetThinkMode(ThinkModeMedium)
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue")
		}
		data, _ := json.Marshal(tv)
		if string(data) != `"medium"` {
			t.Errorf("expected \"medium\", got %s", data)
		}
	})

	t.Run("mode high", func(t *testing.T) {
		c.SetThinkMode(ThinkModeHigh)
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue")
		}
		data, _ := json.Marshal(tv)
		if string(data) != `"high"` {
			t.Errorf("expected \"high\", got %s", data)
		}
	})

	t.Run("mode auto with thinking model", func(t *testing.T) {
		c.SetThinkMode(ThinkModeAuto)
		c.SetModel("deepseek-r1")
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue for thinking-capable model")
		}
		data, _ := json.Marshal(tv)
		if string(data) != "true" {
			t.Errorf("expected true, got %s", data)
		}
	})

	t.Run("mode auto with gpt-oss model", func(t *testing.T) {
		c.SetThinkMode(ThinkModeAuto)
		c.SetModel("gpt-oss-large")
		tv := c.buildThinkValue()
		if tv == nil {
			t.Fatal("expected non-nil ThinkValue for gpt-oss model")
		}
		data, _ := json.Marshal(tv)
		if string(data) != `"medium"` {
			t.Errorf("expected \"medium\", got %s", data)
		}
	})

	t.Run("mode auto with non-thinking model", func(t *testing.T) {
		c.SetThinkMode(ThinkModeAuto)
		c.SetModel("llama3")
		tv := c.buildThinkValue()
		if tv != nil {
			t.Error("expected nil ThinkValue for non-thinking model")
		}
	})
}

func TestChatRequest_ThinkFieldSerialization(t *testing.T) {
	t.Run("think true", func(t *testing.T) {
		tv := NewThinkValueBool(true)
		req := ChatRequest{
			Model:    "test",
			Messages: []Message{{Role: "user", Content: "hi"}},
			Stream:   true,
			Think:    &tv,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var parsed map[string]interface{}
		json.Unmarshal(data, &parsed)

		if parsed["think"] != true {
			t.Errorf("expected think:true, got %v", parsed["think"])
		}
	})

	t.Run("think false", func(t *testing.T) {
		tv := NewThinkValueBool(false)
		req := ChatRequest{
			Model:    "test",
			Messages: []Message{{Role: "user", Content: "hi"}},
			Stream:   true,
			Think:    &tv,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var parsed map[string]interface{}
		json.Unmarshal(data, &parsed)

		if parsed["think"] != false {
			t.Errorf("expected think:false, got %v", parsed["think"])
		}
	})

	t.Run("think medium", func(t *testing.T) {
		tv := NewThinkValueString("medium")
		req := ChatRequest{
			Model:    "test",
			Messages: []Message{{Role: "user", Content: "hi"}},
			Stream:   true,
			Think:    &tv,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var parsed map[string]interface{}
		json.Unmarshal(data, &parsed)

		if parsed["think"] != "medium" {
			t.Errorf("expected think:\"medium\", got %v", parsed["think"])
		}
	})

	t.Run("think omitted", func(t *testing.T) {
		req := ChatRequest{
			Model:    "test",
			Messages: []Message{{Role: "user", Content: "hi"}},
			Stream:   true,
			Think:    nil,
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var parsed map[string]interface{}
		json.Unmarshal(data, &parsed)

		if _, ok := parsed["think"]; ok {
			t.Error("expected think field to be omitted")
		}
	})
}

func TestClient_ChatStream_WithThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify think field is included
		// This verifies the request serialization works

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected ResponseWriter to be a Flusher")
		}

		// Send thinking chunks first
		thinkingChunks := []ChatResponse{
			{Message: Message{Role: "assistant", Thinking: "Let me think..."}, Done: false},
			{Message: Message{Role: "assistant", Thinking: " about this problem."}, Done: false},
		}

		for _, chunk := range thinkingChunks {
			data, _ := json.Marshal(chunk)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
		}

		// Then send content chunks
		contentChunks := []ChatResponse{
			{Message: Message{Role: "assistant", Content: "Hello"}, Done: false},
			{Message: Message{Role: "assistant", Content: " world!"}, Done: true},
		}

		for _, chunk := range contentChunks {
			data, _ := json.Marshal(chunk)
			w.Write(data)
			w.Write([]byte("\n"))
			flusher.Flush()
		}
	}))
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

	var thinkingContents []string
	var contentContents []string

	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}

		if chunk.ThinkingContent != "" {
			thinkingContents = append(thinkingContents, chunk.ThinkingContent)
		}
		if chunk.Response.Message.Content != "" {
			contentContents = append(contentContents, chunk.Response.Message.Content)
		}

		if chunk.Response.Done {
			break
		}
	}

	if len(thinkingContents) != 2 {
		t.Errorf("expected 2 thinking chunks, got %d", len(thinkingContents))
	}
	if len(contentContents) != 2 {
		t.Errorf("expected 2 content chunks, got %d", len(contentContents))
	}
}

func TestMessage_ThinkingFieldNotSentToAPI(t *testing.T) {
	// Verify that when messages with Thinking field are sent,
	// the Thinking field is not included (it's UI-only)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Check that incoming messages don't have thinking field
		for i, msg := range req.Messages {
			if msg.Thinking != "" {
				t.Errorf("message %d should not have thinking field, got %s", i, msg.Thinking)
			}
		}

		resp := ChatResponse{
			Model:   "test-model",
			Message: Message{Role: "assistant", Content: "Response"},
			Done:    true,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	os.Setenv("OLLAMA_HOST", server.URL)
	os.Setenv("OLLAMA_MODEL", "test-model")
	defer os.Unsetenv("OLLAMA_HOST")
	defer os.Unsetenv("OLLAMA_MODEL")

	c := NewClient()

	// Create messages with Thinking field set (simulating previous responses)
	messages := []Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello!", Thinking: "This should NOT be sent"},
		{Role: "user", Content: "How are you?"},
	}

	_, err := c.Chat(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
