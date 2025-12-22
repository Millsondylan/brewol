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
