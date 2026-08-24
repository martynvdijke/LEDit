package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ledit/datasource"
)

func TestValidateIntent_ValidAllActions(t *testing.T) {
	cases := []string{
		`{"action":"next"}`,
		`{"action":"pause"}`,
		`{"action":"resume"}`,
		`{"action":"status_query"}`,
		`{"action":"priority_display","text":"hello","ttl_seconds":30}`,
		`{"action":"source_pin_with_ttl","source_type":"weather","source_id":1,"ttl_seconds":60}`,
	}
	for _, c := range cases {
		if _, err := ValidateIntent(c); err != nil {
			t.Errorf("valid intent rejected %q: %v", c, err)
		}
	}
}

func TestValidateIntent_UnknownAction(t *testing.T) {
	if _, err := ValidateIntent(`{"action":"delete_all"}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected ErrInvalidIntent, got %v", err)
	}
}

func TestValidateIntent_ExtraField(t *testing.T) {
	if _, err := ValidateIntent(`{"action":"pause","extra":"field"}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject extra field, got %v", err)
	}
}

func TestValidateIntent_TTLRange(t *testing.T) {
	if _, err := ValidateIntent(`{"action":"priority_display","text":"hi","ttl_seconds":9999}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject ttl out of range")
	}
	if _, err := ValidateIntent(`{"action":"priority_display","text":"hi","ttl_seconds":2}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject ttl too low")
	}
}

func TestValidateIntent_UnknownSourceType(t *testing.T) {
	if _, err := ValidateIntent(`{"action":"source_pin_with_ttl","source_type":"unknown_type","source_id":1,"ttl_seconds":30}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject unknown source_type")
	}
}

func TestValidateIntent_UnknownSourceID(t *testing.T) {
	if _, err := ValidateIntent(`{"action":"source_pin_with_ttl","source_type":"weather","source_id":0,"ttl_seconds":30}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject source_id 0")
	}
}

func TestValidateIntent_TextSanitization(t *testing.T) {
	intent, err := ValidateIntent(`{"action":"priority_display","text":"<b>hello</b>","ttl_seconds":30}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent.Text == nil || *intent.Text != "hello" {
		t.Fatalf("expected sanitized hello, got %v", intent.Text)
	}
}

func TestValidateIntent_MalformedJSON(t *testing.T) {
	if _, err := ValidateIntent(`{not json`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected malformed error, got %v", err)
	}
}

func TestTruncateUserText(t *testing.T) {
	long := strings.Repeat("a", 2000)
	trunc := TruncateUserText(long)
	if len([]rune(trunc)) != 500 {
		t.Fatalf("expected 500, got %d", len([]rune(trunc)))
	}
}

func TestBuildNLPrompt_Truncate(t *testing.T) {
	long := strings.Repeat("x", 1000)
	p := BuildNLPrompt(long, nil)
	if strings.Count(p, "x") > 600 {
		t.Fatalf("prompt should truncate user text")
	}
}

func TestParseIntent_Mocked(t *testing.T) {
	orig := callLLMFunc
	defer func() { callLLMFunc = orig }()

	// pause
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `{"action":"pause"}`, nil
	}
	cfg := datasource.AIConfig{Endpoint: "http://e", Model: "m"}
	intent, err := ParseIntent(context.Background(), "pause", cfg)
	if err != nil {
		t.Fatalf("parse pause failed: %v", err)
	}
	if intent.Action != "pause" {
		t.Fatalf("want pause got %s", intent.Action)
	}

	// pin with 60s
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `{"action":"source_pin_with_ttl","source_type":"weather","source_id":1,"ttl_seconds":60}`, nil
	}
	intent, err = ParseIntent(context.Background(), "show weather for a minute", cfg)
	if err != nil {
		t.Fatalf("pin failed: %v", err)
	}
	if intent.TTLSeconds == nil || *intent.TTLSeconds != 60 {
		t.Fatalf("ttl mismatch %v", intent.TTLSeconds)
	}

	// invalid JSON
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `not json`, nil
	}
	if _, err := ParseIntent(context.Background(), "garbage", cfg); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected invalid intent, got %v", err)
	}

	// timeout
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	_, err = ParseIntent(context.Background(), "pause", cfg)
	if !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected timeout invalid, got %v", err)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatalf("timeout too long")
	}
}

func TestRateLimiter(t *testing.T) {
	ResetRateLimiterForTest()
	chatID := int64(9999)
	for i := 0; i < 10; i++ {
		if err := checkRateLimit(chatID); err != nil {
			t.Fatalf("request %d should pass: %v", i, err)
		}
	}
	if err := checkRateLimit(chatID); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("11th should be rate limited, got %v", err)
	}
	// truncation verified already
}

func TestHandleNLText_RateLimit(t *testing.T) {
	ResetRateLimiterForTest()
	// Use AI not configured path but rate limiter still checked? HandleNLText checks rate before AI.
	// We need configured AI to not return early; instead mock LLM.
	orig := callLLMFunc
	defer func() { callLLMFunc = orig }()
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `{"action":"pause"}`, nil
	}
	// create server with AI config
	srv := newTelegramTestServer(t)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SaveX(srv.Ctx)
	chatID := int64(12345)
	// 10 ok
	for i := 0; i < 10; i++ {
		reply := HandleNLText(context.Background(), srv, chatID, "pause")
		if strings.Contains(reply, "Too many") {
			t.Fatalf("should not be limited at %d: %q", i, reply)
		}
	}
	reply := HandleNLText(context.Background(), srv, chatID, "pause")
	if !strings.Contains(reply, "Too many") {
		t.Fatalf("expected rate limited reply, got %q", reply)
	}
}

func TestTelegramNL_Integration(t *testing.T) {
	ResetRateLimiterForTest()
	orig := callLLMFunc
	defer func() { callLLMFunc = orig }()
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `{"action":"pause"}`, nil
	}
	srv := newTelegramTestServer(t)
	createTelegramSettings(t, srv, true, "tok", 0)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SaveX(srv.Ctx)
	GlobalFeed = &FeedController{}
	b := &TelegramBot{s: srv, token: "tok", allowedChatID: 0, apiBase: "http://x", httpc: nil}
	// Directly test handleMessage via stub? Instead test HandleNLText + Execute
	reply := HandleNLText(context.Background(), srv, 1, "pause please")
	if !strings.Contains(reply, "Paused") {
		t.Fatalf("expected paused reply, got %q", reply)
	}
	if !GlobalFeed.IsPaused() {
		t.Fatalf("feed should be paused")
	}
	_ = b
}

func TestMQTT_NL(t *testing.T) {
	ResetRateLimiterForTest()
	orig := callLLMFunc
	defer func() { callLLMFunc = orig }()
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `{"action":"resume"}`, nil
	}
	srv := newTestServerWithDB(t)
	srv.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("tcp://b:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv.Ctx)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SaveX(srv.Ctx)
	GlobalFeed = &FeedController{}
	GlobalFeed.Pause()
	HandleNLPayload(srv, "resume")
	if GlobalFeed.IsPaused() {
		t.Fatalf("expected resumed via MQTT NL")
	}
	// when AI disabled, no action
	srv2 := newTestServerWithDB(t)
	// clear shared DB state from previous srv
	srv2.DB.AISettings.Delete().ExecX(srv2.Ctx)
	srv2.DB.MQTTSettings.Delete().ExecX(srv2.Ctx)
	srv2.DB.MQTTSettings.Create().SetEnabled(true).SetBroker("tcp://b:1883").SetControlTopic("ledit/control").SetDisplayTopic("ledit/display").SaveX(srv2.Ctx)
	GlobalFeed.Pause()
	HandleNLPayload(srv2, "resume")
	if !GlobalFeed.IsPaused() {
		t.Fatalf("should remain paused when AI disabled")
	}
	GlobalFeed.Resume()
}
