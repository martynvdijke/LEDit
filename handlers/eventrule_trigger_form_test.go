package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ledit/datasource"
)

// postRuleForm submits an event-rule form as the given admin session.
func postRuleForm(t *testing.T, srv *Server, session *http.Cookie, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(session)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// getRulePage fetches an admin event-rule page as the given session.
func getRulePage(t *testing.T, srv *Server, session *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if session != nil {
		req.AddCookie(session)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// baseRuleForm is the minimal valid legacy (leaf, condition-mode) submission.
func baseRuleForm(name string) url.Values {
	return url.Values{
		"name":                   {name},
		"source_type":            {"systemstats"},
		"source_id":              {"0"},
		"condition":              {`{"path":"cpu","operator":"gt","value":50}`},
		"check_interval_seconds": {"30"},
		"cooldown_seconds":       {"0"},
		"enabled":                {"on"},
	}
}

// nestedGroupJSON builds a condition tree with exactly depth nested groups,
// each holding a leaf so every group has two children.
func nestedGroupJSON(depth int) string {
	text := `{"path":"cpu","operator":"gt","value":50}`
	for i := 0; i < depth; i++ {
		text = `{"op":"and","conditions":[` + text + `,{"path":"temp","operator":"lt","value":20}]}`
	}
	return text
}

func TestEventRuleAuth_ComposedGroupedRuleRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	grouped := `{"op":"and","conditions":[{"path":"cpu","operator":"gt","value":80},{"path":"temp","operator":"lt","value":20}]}`
	form := baseRuleForm("Hot and cold")
	form.Set("condition", grouped)

	if w := postRuleForm(t, srv, session, "/admin/eventrules/new", form); w.Code != http.StatusFound {
		t.Fatalf("grouped create: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	rule := srv.DB.DisplayRule.Query().FirstX(ctx)
	node, err := datasource.ParseConditionNode(rule.Condition)
	if err != nil || !node.IsGroup() {
		t.Fatalf("stored grouped condition did not parse as a group: %+v err=%v", node, err)
	}
	if got := datasource.FormatConditionNode(node); got != "cpu gt 80 and temp lt 20" {
		t.Fatalf("unexpected grouped summary %q", got)
	}
	if rule.ForSeconds != 0 || rule.TriggerKind != triggerKindCondition || rule.Sequence != "" {
		t.Fatalf("unexpected trigger fields %d/%q/%q", rule.ForSeconds, rule.TriggerKind, rule.Sequence)
	}

	// The list page renders the readable summary server-side.
	lw := getRulePage(t, srv, session, "/admin/eventrules")
	if lw.Code != http.StatusOK {
		t.Fatalf("list page: expected 200, got %d", lw.Code)
	}
	if body := lw.Body.String(); !strings.Contains(body, "cpu gt 80 and temp lt 20") {
		t.Fatalf("list page missing grouped summary; body head %q", body[:min(600, len(body))])
	}

	// The edit page keeps the tree so the builder can reconstruct it.
	ew := getRulePage(t, srv, session, "/admin/eventrules/"+itoa(rule.ID)+"/edit")
	if ew.Code != http.StatusOK {
		t.Fatalf("edit page: expected 200, got %d", ew.Code)
	}
	if body := ew.Body.String(); !strings.Contains(body, "&#34;op&#34;:&#34;and&#34;") {
		t.Fatalf("edit page missing serialised grouped condition")
	}
}

func TestEventRuleAuth_SustainedConditionRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	form := baseRuleForm("Sustained")
	form.Set("for_seconds", "600") // the unit select posts seconds

	if w := postRuleForm(t, srv, session, "/admin/eventrules/new", form); w.Code != http.StatusFound {
		t.Fatalf("sustained create: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	rule := srv.DB.DisplayRule.Query().FirstX(ctx)
	if rule.ForSeconds != 600 {
		t.Fatalf("expected for_seconds 600, got %d", rule.ForSeconds)
	}
	if rule.TriggerKind != triggerKindCondition {
		t.Fatalf("expected condition trigger kind, got %q", rule.TriggerKind)
	}

	// The builder receives the stored seconds so it can show 10 minutes.
	ew := getRulePage(t, srv, session, "/admin/eventrules/"+itoa(rule.ID)+"/edit")
	if body := ew.Body.String(); !strings.Contains(body, `id="for_seconds" value="600"`) {
		t.Fatalf("edit page missing for_seconds value 600")
	}

	// Editing back to 0 keeps legacy immediate firing.
	form.Set("for_seconds", "0")
	form.Set("name", "Sustained reset")
	if w := postRuleForm(t, srv, session, "/admin/eventrules/"+itoa(rule.ID)+"/edit", form); w.Code != http.StatusFound {
		t.Fatalf("sustained update: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	if got := srv.DB.DisplayRule.GetX(ctx, rule.ID).ForSeconds; got != 0 {
		t.Fatalf("expected for_seconds reset to 0, got %d", got)
	}
}

func TestEventRuleAuth_SequenceRuleRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	sequence := `{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":60}]}`
	form := baseRuleForm("Door then motion")
	form.Set("trigger_kind", "sequence")
	form.Set("for_seconds", "0")
	form.Set("sequence", sequence)

	if w := postRuleForm(t, srv, session, "/admin/eventrules/new", form); w.Code != http.StatusFound {
		t.Fatalf("sequence create: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	rule := srv.DB.DisplayRule.Query().FirstX(ctx)
	if rule.TriggerKind != triggerKindSequence {
		t.Fatalf("expected sequence trigger kind, got %q", rule.TriggerKind)
	}
	if rule.ForSeconds != 0 {
		t.Fatalf("sequence rules must not carry a rule-level hold, got %d", rule.ForSeconds)
	}
	seq, err := datasource.ParseSequence(rule.Sequence, rule.CheckIntervalSeconds)
	if err != nil || len(seq.Steps) != 2 || seq.WindowSeconds != 900 {
		t.Fatalf("stored sequence did not parse: %+v err=%v", seq, err)
	}

	lw := getRulePage(t, srv, session, "/admin/eventrules")
	if body := lw.Body.String(); !strings.Contains(body, "within 15m") {
		t.Fatalf("list page missing sequence summary; body head %q", body[:min(600, len(body))])
	}

	// The edit page carries the raw sequence JSON for the step editor.
	ew := getRulePage(t, srv, session, "/admin/eventrules/"+itoa(rule.ID)+"/edit")
	if body := ew.Body.String(); !strings.Contains(body, "&#34;window_seconds&#34;:900") {
		t.Fatalf("edit page missing serialised sequence")
	}
}

func TestEventRuleAuth_ComposedGroupDepthRejected(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	tooDeep := nestedGroupJSON(datasource.MaxConditionGroupDepth + 1)
	form := baseRuleForm("Too deep")
	form.Set("condition", tooDeep)
	w := postRuleForm(t, srv, session, "/admin/eventrules/new", form)
	if w.Code != http.StatusOK {
		t.Fatalf("too deep: expected 200 re-render, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "nest at most 4 levels") {
		t.Fatalf("expected depth error in body")
	}
	// The hidden input re-renders the submitted JSON with HTML-escaped quotes.
	if !strings.Contains(w.Body.String(), strings.ReplaceAll(tooDeep, `"`, "&#34;")) {
		t.Fatalf("expected submitted condition preserved on re-render")
	}
	if count, _ := srv.DB.DisplayRule.Query().Count(ctx); count != 0 {
		t.Fatalf("too deep condition must not persist, got %d rows", count)
	}

	// The deepest accepted tree still saves.
	form.Set("condition", nestedGroupJSON(datasource.MaxConditionGroupDepth))
	form.Set("name", "Deep but legal")
	if w := postRuleForm(t, srv, session, "/admin/eventrules/new", form); w.Code != http.StatusFound {
		t.Fatalf("depth 4 create: expected 302, got %d body %s", w.Code, w.Body.String())
	}
}

func TestEventRuleAuth_SequenceBoundsRejected(t *testing.T) {
	tests := []struct {
		name     string
		sequence string
		want     string
	}{
		{
			name:     "single step",
			sequence: `{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0}]}`,
			want:     "between 2 and 4 steps",
		},
		{
			name:     "five steps",
			sequence: `{"window_seconds":900,"steps":[{"condition":{"path":"a","operator":"exists"},"for_seconds":0},{"condition":{"path":"b","operator":"exists"},"for_seconds":0},{"condition":{"path":"c","operator":"exists"},"for_seconds":0},{"condition":{"path":"d","operator":"exists"},"for_seconds":0},{"condition":{"path":"e","operator":"exists"},"for_seconds":0}]}`,
			want:     "between 2 and 4 steps",
		},
		{
			name:     "window too small for holds",
			sequence: `{"window_seconds":30,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":600}]}`,
			want:     "to fit the step holds",
		},
		{
			name:     "step hold below check interval",
			sequence: `{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":10}]}`,
			want:     "at least the check interval",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newEventRuleAuthTestServer(t)
			session := loginAsAdmin(t, srv)
			form := baseRuleForm("Bad sequence")
			form.Set("trigger_kind", "sequence")
			form.Set("for_seconds", "0")
			form.Set("sequence", tc.sequence)
			w := postRuleForm(t, srv, session, "/admin/eventrules/new", form)
			if w.Code != http.StatusOK {
				t.Fatalf("%s: expected 200 re-render, got %d", tc.name, w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("%s: expected %q in body", tc.name, tc.want)
			}
			if !strings.Contains(w.Body.String(), "&#34;window_seconds&#34;") {
				t.Fatalf("%s: expected submitted sequence preserved on re-render", tc.name)
			}
			if count, _ := srv.DB.DisplayRule.Query().Count(context.Background()); count != 0 {
				t.Fatalf("%s: rejected sequence must not persist, got %d rows", tc.name, count)
			}
		})
	}
}

func TestEventRuleAuth_TriggerModeConflictsRejected(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(url.Values)
		want   string
	}{
		{
			name:   "unknown trigger kind",
			mutate: func(f url.Values) { f.Set("trigger_kind", "bogus") },
			want:   "invalid trigger_kind",
		},
		{
			name: "sequence text in condition mode",
			mutate: func(f url.Values) {
				f.Set("sequence", `{"window_seconds":900,"steps":[]}`)
			},
			want: "sequence must be empty",
		},
		{
			name: "rule hold in sequence mode",
			mutate: func(f url.Values) {
				f.Set("trigger_kind", "sequence")
				f.Set("for_seconds", "60")
				f.Set("sequence", `{"window_seconds":900,"steps":[{"condition":{"path":"a","operator":"exists"},"for_seconds":0},{"condition":{"path":"b","operator":"exists"},"for_seconds":0}]}`)
			},
			want: "for_seconds must be 0 for sequence triggers",
		},
		{
			name: "missing sequence steps",
			mutate: func(f url.Values) {
				f.Set("trigger_kind", "sequence")
				f.Set("sequence", "")
			},
			want: "requires an ordered list of steps",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newEventRuleAuthTestServer(t)
			session := loginAsAdmin(t, srv)
			form := baseRuleForm("Conflict")
			tc.mutate(form)
			w := postRuleForm(t, srv, session, "/admin/eventrules/new", form)
			if w.Code != http.StatusOK {
				t.Fatalf("%s: expected 200 re-render, got %d", tc.name, w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("%s: expected %q in body", tc.name, tc.want)
			}
			if count, _ := srv.DB.DisplayRule.Query().Count(context.Background()); count != 0 {
				t.Fatalf("%s: rejected rule must not persist, got %d rows", tc.name, count)
			}
		})
	}
}

func TestEventRuleAuth_LegacyRuleEditRoundTrip(t *testing.T) {
	srv := newEventRuleAuthTestServer(t)
	session := loginAsAdmin(t, srv)
	ctx := context.Background()

	// Seeded as a legacy row: no trigger_kind, no sequence, no hold.
	row := srv.DB.DisplayRule.Create().
		SetName("Legacy").
		SetEnabled(true).
		SetSourceType("systemstats").
		SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"gt","value":50}`).
		SetCheckIntervalSeconds(30).
		SetCooldownSeconds(0).
		SaveX(ctx)
	if row.TriggerKind != triggerKindCondition || row.Sequence != "" || row.ForSeconds != 0 {
		t.Fatalf("unexpected legacy defaults %q/%q/%d", row.TriggerKind, row.Sequence, row.ForSeconds)
	}

	// A legacy client submits without any of the new fields.
	form := baseRuleForm("Legacy edited")
	if w := postRuleForm(t, srv, session, "/admin/eventrules/"+itoa(row.ID)+"/edit", form); w.Code != http.StatusFound {
		t.Fatalf("legacy update: expected 302, got %d body %s", w.Code, w.Body.String())
	}
	got := srv.DB.DisplayRule.GetX(ctx, row.ID)
	if got.Name != "Legacy edited" {
		t.Fatalf("legacy edit did not persist name, got %q", got.Name)
	}
	if got.TriggerKind != triggerKindCondition || got.Sequence != "" || got.ForSeconds != 0 {
		t.Fatalf("legacy edit changed trigger fields: %q/%q/%d", got.TriggerKind, got.Sequence, got.ForSeconds)
	}
	node, err := datasource.ParseConditionNode(got.Condition)
	if err != nil || node.IsGroup() || node.Path != "cpu" {
		t.Fatalf("legacy condition changed: %+v err=%v", node, err)
	}

	// The list page still shows the historic leaf summary.
	lw := getRulePage(t, srv, session, "/admin/eventrules")
	if body := lw.Body.String(); !strings.Contains(body, "cpu gt 50") {
		t.Fatalf("list page missing legacy leaf summary; body head %q", body[:min(600, len(body))])
	}
}
