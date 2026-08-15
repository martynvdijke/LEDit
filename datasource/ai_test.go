package datasource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatCompletionsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q, want Bearer test-key", got)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		if req.Model != "test-model" || len(req.Messages) != 1 || req.MaxTokens != 200 {
			t.Errorf("unexpected request: %+v", req)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  hello world  "}}]}`))
	}))
	defer srv.Close()

	cfg := AIConfig{Endpoint: srv.URL, APIKey: "test-key", Model: "test-model"}
	got, err := ChatCompletions(context.Background(), cfg, []ChatMessage{{Role: "user", Content: "hi"}}, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("content = %q, want trimmed hello world", got)
	}
}

func TestChatCompletionsTrailingSlashEndpoint(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	cfg := AIConfig{Endpoint: srv.URL + "/v1/", Model: "m"}
	if _, err := ChatCompletions(context.Background(), cfg, nil, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %q", path)
	}
}

func TestChatCompletionsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	cfg := AIConfig{Endpoint: srv.URL, Model: "m"}
	_, err := ChatCompletions(context.Background(), cfg, nil, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want status + body snippet", err)
	}
}

func TestChatCompletionsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	cfg := AIConfig{Endpoint: srv.URL, Model: "m"}
	if _, err := ChatCompletions(context.Background(), cfg, nil, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestChatCompletionsEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	cfg := AIConfig{Endpoint: srv.URL, Model: "m"}
	if _, err := ChatCompletions(context.Background(), cfg, nil, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestChatCompletionsTimeout(t *testing.T) {
	old := ChatCompletionsTimeout
	ChatCompletionsTimeout = 50 * time.Millisecond
	defer func() { ChatCompletionsTimeout = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"late"}}]}`))
	}))
	defer srv.Close()

	cfg := AIConfig{Endpoint: srv.URL, Model: "m"}
	start := time.Now()
	if _, err := ChatCompletions(context.Background(), cfg, nil, 0); err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Errorf("timeout not enforced: took %v", time.Since(start))
	}
}

func TestChatCompletionsMissingConfig(t *testing.T) {
	if _, err := ChatCompletions(context.Background(), AIConfig{}, nil, 0); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
	if _, err := ChatCompletions(context.Background(), AIConfig{Endpoint: "http://x"}, nil, 0); err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestBuildSlideSystemPrompt(t *testing.T) {
	p := BuildSlideSystemPrompt()
	if strings.TrimSpace(p) == "" {
		t.Fatal("prompt empty")
	}
	if !strings.Contains(p, "LED") {
		t.Errorf("prompt should mention display: %q", p)
	}
}

func TestBuildDigestSystemPrompt(t *testing.T) {
	p := BuildDigestSystemPrompt()
	if strings.TrimSpace(p) == "" {
		t.Fatal("prompt empty")
	}
}
