package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ledit/datasource"
)

func TestValidateIntent_CreatePlaylist_Valid(t *testing.T) {
	raw := `{"action":"create_playlist","name":"Morning","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}`
	if _, err := ValidateIntent(raw); err != nil {
		t.Fatalf("valid create_playlist rejected: %v", err)
	}
	raw2 := `{"action":"create_playlist","name":"Morning","items":"[{\"source_type\":\"weather\",\"source_id\":1}]","schedule_windows":"[]"}`
	if _, err := ValidateIntent(raw2); err != nil {
		t.Fatalf("valid with schedule_windows rejected: %v", err)
	}
}

func TestValidateIntent_CreatePlaylist_ExtraKey(t *testing.T) {
	raw := `{"action":"create_playlist","name":"x","items":"[]","extra":"field"}`
	if _, err := ValidateIntent(raw); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject extra key, got %v", err)
	}
}

func TestValidateIntent_CreatePlaylist_BadSourceType(t *testing.T) {
	raw := `{"action":"create_playlist","name":"x","items":"[{\"source_type\":\"bad\",\"source_id\":1}]"}`
	if _, err := ValidateIntent(raw); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject bad source_type, got %v", err)
	}
}

func TestValidateIntent_CreatePlaylist_Oversized(t *testing.T) {
	// 65 items is over limit (datasource caps 64)
	items := "["
	for i := 0; i < 65; i++ {
		if i > 0 {
			items += ","
		}
		items += `{"source_type":"weather","source_id":1}`
	}
	items += "]"
	// JSON-escape for outer string
	raw := `{"action":"create_playlist","name":"x","items":` + "`" + items + "`" + `}`
	// Simpler: construct via marshaling is complex; instead test oversize payload length
	_ = raw
	// Test via direct many items using ValidateIntent with properly escaped JSON string field
	// Use Go to build raw JSON with json-escaped items
	large := strings.Repeat(`{"source_type":"weather","source_id":1},`, 65)
	large = "[" + strings.TrimSuffix(large, ",") + "]"
	escaped, _ := datasource.ParsePlaylistItems(large)
	if escaped == nil {
		// Parse should fail due to cap
	}
	// Build outer intent with items as JSON string
	outer := `{"action":"create_playlist","name":"x","items":` + "\"" + strings.ReplaceAll(large, `"`, `\"`) + "\"" + `}`
	if _, err := ValidateIntent(outer); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected reject oversized items, got %v", err)
	}
}

func TestExecuteIntent_CreatePlaylist_FlagDisabled(t *testing.T) {
	ResetRateLimiterForTest()
	srv := newTelegramTestServer(t)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SetNlCreateEnabled(false).SaveX(srv.Ctx)
	srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	intent, _ := ValidateIntent(`{"action":"create_playlist","name":"Test","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}`)
	reply := ExecuteIntent(srv, intent)
	if !strings.Contains(reply, "disabled") {
		t.Fatalf("expected disabled reply, got %q", reply)
	}
	if cnt, _ := srv.DB.Playlist.Query().Count(srv.Ctx); cnt != 0 {
		t.Fatalf("expected no playlist created, got %d", cnt)
	}
}

func TestExecuteIntent_CreatePlaylist_FlagEnabled(t *testing.T) {
	ResetRateLimiterForTest()
	srv := newTelegramTestServer(t)
	// Shared in-memory DB across tests: upsert the singleton AISettings row.
	if existing, err := srv.DB.AISettings.Query().Only(srv.Ctx); err == nil {
		srv.DB.AISettings.UpdateOne(existing).SetNlCreateEnabled(true).SaveX(srv.Ctx)
	} else {
		srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SetNlCreateEnabled(true).SaveX(srv.Ctx)
	}
	// Ensure GeneralSettings exists (New already creates? ensure)
	if _, err := srv.DB.GeneralSettings.Query().Only(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	intent, _ := ValidateIntent(`{"action":"create_playlist","name":"Test","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}`)
	reply := ExecuteIntent(srv, intent)
	if !strings.Contains(reply, "Created playlist") {
		t.Fatalf("expected created reply, got %q", reply)
	}
	if cnt, _ := srv.DB.Playlist.Query().Count(srv.Ctx); cnt != 1 {
		t.Fatalf("expected 1 playlist, got %d", cnt)
	}
}

func TestExecuteIntent_CreateScene_And_Rule(t *testing.T) {
	ResetRateLimiterForTest()
	srv := newTelegramTestServer(t)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SetNlCreateEnabled(true).SaveX(srv.Ctx)
	if _, err := srv.DB.GeneralSettings.Query().Only(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	// scene
	sceneRaw := `{"action":"create_scene","name":"Night","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"==\",\"value\":\"on\"}]}]","actions":"{}"}`
	si, err := ValidateIntent(sceneRaw)
	if err != nil {
		t.Fatalf("validate scene failed: %v", err)
	}
	reply := ExecuteIntent(srv, si)
	if !strings.Contains(reply, "Created scene") {
		t.Fatalf("scene reply %q", reply)
	}
	// rule
	ruleRaw := `{"action":"create_rule","name":"Alert","source_type":"weather","source_id":1,"condition":"{\"path\":\"temp\",\"operator\":\"gt\",\"value\":30}"}`
	ri, err := ValidateIntent(ruleRaw)
	if err != nil {
		t.Fatalf("validate rule failed: %v", err)
	}
	reply = ExecuteIntent(srv, ri)
	if !strings.Contains(reply, "Created rule") {
		t.Fatalf("rule reply %q", reply)
	}
}

func TestCreationRateLimit(t *testing.T) {
	ResetRateLimiterForTest()
	chatID := int64(8888)
	for i := 0; i < 3; i++ {
		if err := checkCreateRateLimit(chatID); err != nil {
			t.Fatalf("should pass %d: %v", i, err)
		}
	}
	if err := checkCreateRateLimit(chatID); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limited on 4th")
	}
}

func TestAvailableSourcesForPrompt_Real(t *testing.T) {
	srv := newTelegramTestServer(t)
	srv.DB.Weather.Create().SetURL("http://w").SetToken("t").SaveX(srv.Ctx)
	srv.DB.TextSlide.Create().SetContent("hello").SetColor("#fff").SetBgColor("#000").SetFontSize(12).SaveX(srv.Ctx)
	// link to GeneralSettings
	if gs, err := srv.DB.GeneralSettings.Query().Only(srv.Ctx); err == nil {
		_ = gs
	}
	srcs := AvailableSourcesForPrompt(srv)
	if len(srcs) == 0 {
		t.Fatalf("expected sources, got empty")
	}
	// ensure cap 40
	if len(srcs) > 40 {
		t.Fatalf("cap exceeded %d", len(srcs))
	}
}

func TestParseIntent_CreatePlaylist_Mocked(t *testing.T) {
	orig := callLLMFunc
	defer func() { callLLMFunc = orig }()
	callLLMFunc = func(ctx context.Context, cfg datasource.AIConfig, msgs []datasource.ChatMessage, maxTokens int) (string, error) {
		return `{"action":"create_playlist","name":"Mocked","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}`, nil
	}
	cfg := datasource.AIConfig{Endpoint: "http://e", Model: "m"}
	intent, err := ParseIntent(context.Background(), "create playlist", cfg)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if intent.Action != "create_playlist" {
		t.Fatalf("want create_playlist got %s", intent.Action)
	}
}

func TestValidateIntent_InvalidJSON(t *testing.T) {
	if _, err := ValidateIntent(`{"action":"create_rule","name":"x","source_type":"weather","source_id":1,"condition":"not json"}`); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected invalid condition json")
	}
}

func TestExecuteIntent_Scene_Rule_FlagEnabled_Details(t *testing.T) {
	ResetRateLimiterForTest()
	srv := newTelegramTestServer(t)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SetNlCreateEnabled(true).SaveX(srv.Ctx)
	if _, err := srv.DB.GeneralSettings.Query().Only(srv.Ctx); err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}
	trig := `[{"op":"all-of","conditions":[{"entity_id":"sensor.x","operator":"==","value":"on"}]}]`
	act := `{"source_type":"weather","source_id":1}`
	sceneRaw := `{"action":"create_scene","name":"DetailScene","triggers":` + "`" + trig + "`" + `,"actions":` + "`" + act + "`" + `,"priority":5,"ttl_seconds":60}`
	// Build with proper JSON escaping
	sceneJSON := `{"action":"create_scene","name":"DetailScene","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"==\",\"value\":\"on\"}]}]","actions":"{\"source_type\":\"weather\",\"source_id\":1}","priority":5,"ttl_seconds":60}`
	si, err := ValidateIntent(sceneJSON)
	if err != nil {
		t.Fatalf("validate scene: %v", err)
	}
	_ = sceneRaw
	reply := ExecuteIntent(srv, si)
	if !strings.Contains(reply, "Created scene") {
		t.Fatalf("scene reply %q", reply)
	}
	if cnt, _ := srv.DB.Scene.Query().Count(srv.Ctx); cnt != 1 {
		t.Fatalf("scene count %d", cnt)
	}
	sc, _ := srv.DB.Scene.Query().Only(srv.Ctx)
	if sc.Triggers == "" || sc.Actions == "" {
		t.Fatalf("triggers/actions not stored")
	}
	// Ensure stored values are parsed JSON (contain expected keys) not raw garbage
	if !strings.Contains(sc.Triggers, "sensor.x") || !strings.Contains(sc.Actions, "weather") {
		t.Fatalf("stored triggers/actions unexpected: %q %q", sc.Triggers, sc.Actions)
	}
	if sc.Priority != 5 {
		t.Fatalf("priority %d", sc.Priority)
	}
	gs, _ := srv.DB.GeneralSettings.Query().WithScenes().Only(srv.Ctx)
	if len(gs.Edges.Scenes) != 1 {
		t.Fatalf("GeneralSettings scenes edge %d", len(gs.Edges.Scenes))
	}

	ruleJSON := `{"action":"create_rule","name":"DetailRule","source_type":"weather","source_id":1,"condition":"{\"path\":\"temp\",\"operator\":\"gt\",\"value\":30}","check_interval_seconds":10,"cooldown_seconds":5}`
	ri, err := ValidateIntent(ruleJSON)
	if err != nil {
		t.Fatalf("validate rule: %v", err)
	}
	reply = ExecuteIntent(srv, ri)
	if !strings.Contains(reply, "Created rule") {
		t.Fatalf("rule reply %q", reply)
	}
	if cnt, _ := srv.DB.DisplayRule.Query().Count(srv.Ctx); cnt != 1 {
		t.Fatalf("rule count %d", cnt)
	}
	rule, _ := srv.DB.DisplayRule.Query().Only(srv.Ctx)
	if !strings.Contains(rule.Condition, "temp") {
		t.Fatalf("condition not stored: %q", rule.Condition)
	}
	gs2, _ := srv.DB.GeneralSettings.Query().WithDisplayrules().Only(srv.Ctx)
	if len(gs2.Edges.Displayrules) != 1 {
		t.Fatalf("GeneralSettings displayrules edge %d", len(gs2.Edges.Displayrules))
	}
}

func TestExecuteIntent_Scene_Rule_FlagDisabled(t *testing.T) {
	ResetRateLimiterForTest()
	srv := newTelegramTestServer(t)
	srv.DB.AISettings.Create().SetProvider("openai").SetAPIKey("k").SetModel("m").SetEndpoint("http://e").SetNlCreateEnabled(false).SaveX(srv.Ctx)
	srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	si, _ := ValidateIntent(`{"action":"create_scene","name":"Night","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"==\",\"value\":\"on\"}]}]","actions":"{}"}`)
	if reply := ExecuteIntent(srv, si); !strings.Contains(reply, "disabled") {
		t.Fatalf("expected disabled, got %q", reply)
	}
	ri, _ := ValidateIntent(`{"action":"create_rule","name":"Alert","source_type":"weather","source_id":1,"condition":"{\"path\":\"temp\",\"operator\":\"gt\",\"value\":30}"}`)
	if reply := ExecuteIntent(srv, ri); !strings.Contains(reply, "disabled") {
		t.Fatalf("expected disabled, got %q", reply)
	}
	if cnt, _ := srv.DB.Scene.Query().Count(srv.Ctx); cnt != 0 {
		t.Fatalf("scene count %d", cnt)
	}
	if cnt, _ := srv.DB.DisplayRule.Query().Count(srv.Ctx); cnt != 0 {
		t.Fatalf("rule count %d", cnt)
	}
}

func TestValidateIntent_ClampingRejection(t *testing.T) {
	// invalid operator in scene triggers
	rawBadOp := `{"action":"create_scene","name":"x","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"invalid\",\"value\":\"on\"}]}]","actions":"{}"}`
	if _, err := ValidateIntent(rawBadOp); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected invalid operator rejected")
	}
	// check_interval <5
	rawCI := `{"action":"create_rule","name":"x","source_type":"weather","source_id":1,"condition":"{\"path\":\"a\",\"operator\":\"gt\",\"value\":1}","check_interval_seconds":2}`
	if _, err := ValidateIntent(rawCI); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected check_interval rejected")
	}
	// cooldown <0
	rawCD := `{"action":"create_rule","name":"x","source_type":"weather","source_id":1,"condition":"{\"path\":\"a\",\"operator\":\"gt\",\"value\":1}","cooldown_seconds":-1}`
	if _, err := ValidateIntent(rawCD); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected cooldown rejected")
	}
	// priority out of range
	rawPrio := `{"action":"create_scene","name":"x","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"==\",\"value\":\"on\"}]}]","actions":"{}","priority":200}`
	if _, err := ValidateIntent(rawPrio); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected priority rejected")
	}
	// ttl out of range for scene
	rawTTL := `{"action":"create_scene","name":"x","triggers":"[{\"op\":\"all-of\",\"conditions\":[{\"entity_id\":\"sensor.x\",\"operator\":\"==\",\"value\":\"on\"}]}]","actions":"{}","ttl_seconds":1}`
	if _, err := ValidateIntent(rawTTL); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected ttl rejected")
	}
	// name >64 rejected
	longName := strings.Repeat("a", 100)
	rawLong := `{"action":"create_playlist","name":"` + longName + `","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}`
	if _, err := ValidateIntent(rawLong); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected long name rejected")
	}
	// HTML in name sanitized to hello
	rawHTML := `{"action":"create_playlist","name":"<b>hello</b>","items":"[{\"source_type\":\"weather\",\"source_id\":1}]"}`
	intent, err := ValidateIntent(rawHTML)
	if err != nil {
		t.Fatalf("html name rejected: %v", err)
	}
	if intent.Name == nil || *intent.Name != "hello" {
		t.Fatalf("expected sanitized hello, got %v", intent.Name)
	}
	// oversized items already tested but also here: 65 items
	large := strings.Repeat(`{"source_type":"weather","source_id":1},`, 65)
	large = "[" + strings.TrimSuffix(large, ",") + "]"
	outer := `{"action":"create_playlist","name":"x","items":` + "\"" + strings.ReplaceAll(large, `"`, `\"`) + "\"" + `}`
	if _, err := ValidateIntent(outer); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("expected oversized items rejected")
	}
}

func TestAvailableSourcesForPrompt_IncludesConfigured(t *testing.T) {
	srv := newTelegramTestServer(t)
	cal := srv.DB.Calendar.Create().SetURL("http://cal").SetName("MyCal").SaveX(srv.Ctx)
	gs, err := srv.DB.GeneralSettings.Query().Only(srv.Ctx)
	if err != nil {
		srv.DB.GeneralSettings.Create().SetTimeout(5).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
		gs, _ = srv.DB.GeneralSettings.Query().Only(srv.Ctx)
	}
	_ = srv.DB.GeneralSettings.UpdateOne(gs).AddCalendars(cal).Exec(srv.Ctx)
	srcs := AvailableSourcesForPrompt(srv)
	found := false
	for _, s := range srcs {
		if s.Type == "calendar" && s.ID == cal.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected calendar source in prompt, got %+v", srcs)
	}
}
