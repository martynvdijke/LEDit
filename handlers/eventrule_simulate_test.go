package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
)

// simulateRuleResult is the decoded JSON body returned by APIEventRuleSimulate.
type simulateRuleResult struct {
	Matched          bool           `json:"matched"`
	Would            bool           `json:"would"`
	Error            string         `json:"error"`
	TriggerKind      string         `json:"trigger_kind"`
	ForSeconds       int            `json:"for_seconds"`
	Sequence         string         `json:"sequence"`
	HistoryDependent bool           `json:"history_dependent"`
	Nodes            []simulateNode `json:"nodes"`
	Steps            []simulateStep `json:"steps"`
	State            map[string]any `json:"state"`
}

func simulateRule(t *testing.T, srv *Server, id int) (int, simulateRuleResult) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/eventrules/"+itoa(id)+"/simulate", nil)
	c.Params = gin.Params{{Key: "id", Value: itoa(id)}}
	srv.APIEventRuleSimulate(c)
	var out simulateRuleResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode simulate response: %v (body %s)", err, w.Body.String())
	}
	return w.Code, out
}

// useStateResolver points RuleTargetResolver at an in-memory provider serving
// the supplied state for the "systemstats" source.
func useStateResolver(t *testing.T, state map[string]any) {
	t.Helper()
	orig := RuleTargetResolver
	fake := &testStateProvider{state: state}
	RuleTargetResolver = func(sourceType string, _ int) (datasource.Datasource, bool) {
		if sourceType == "systemstats" {
			return fake, true
		}
		return nil, false
	}
	t.Cleanup(func() { RuleTargetResolver = orig })
}

func TestAPIEventRuleSimulateGroupedLeaves(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"cpu": float64(90), "temp": float64(10)})
	srv := &Server{DB: client, Ctx: context.Background()}

	rule := client.DisplayRule.Create().
		SetName("hot-and-cold").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"op":"and","conditions":[{"path":"cpu","operator":"gt","value":80},{"path":"temp","operator":"lt","value":20}]}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	code, res := simulateRule(t, srv, rule.ID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !res.Matched || !res.Would {
		t.Fatalf("expected matched/would true, got %+v", res)
	}
	if res.TriggerKind != triggerKindCondition || res.ForSeconds != 0 || res.HistoryDependent {
		t.Fatalf("unexpected trigger shape: %+v", res)
	}
	if len(res.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2 (%+v)", len(res.Nodes), res.Nodes)
	}
	if res.Nodes[0].Path != "cpu" || res.Nodes[0].Operator != "gt" || !res.Nodes[0].Matched {
		t.Fatalf("leaf 0 = %+v", res.Nodes[0])
	}
	if res.Nodes[1].Path != "temp" || res.Nodes[1].Operator != "lt" || !res.Nodes[1].Matched {
		t.Fatalf("leaf 1 = %+v", res.Nodes[1])
	}
	if len(res.Steps) != 0 {
		t.Fatalf("condition mode must not return steps: %+v", res.Steps)
	}
}

func TestAPIEventRuleSimulateGroupedReportsFalseLeaf(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"cpu": float64(90), "temp": float64(25)})
	srv := &Server{DB: client, Ctx: context.Background()}

	// Nested OR inside an AND: three leaves in document order.
	rule := client.DisplayRule.Create().
		SetName("nested").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"op":"and","conditions":[{"path":"cpu","operator":"gt","value":80},{"op":"or","conditions":[{"path":"temp","operator":"lt","value":20},{"path":"temp","operator":"ge","value":25}]}]}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	_, res := simulateRule(t, srv, rule.ID)
	if !res.Matched {
		t.Fatalf("expected overall match (cpu true, or true), got %+v", res)
	}
	if len(res.Nodes) != 3 {
		t.Fatalf("flattened leaves = %d, want 3 (%+v)", len(res.Nodes), res.Nodes)
	}
	wantPaths := []string{"cpu", "temp", "temp"}
	for i, want := range wantPaths {
		if res.Nodes[i].Path != want {
			t.Fatalf("leaf %d path = %q, want %q", i, res.Nodes[i].Path, want)
		}
	}
	if !res.Nodes[0].Matched || res.Nodes[1].Matched || !res.Nodes[2].Matched {
		t.Fatalf("leaf matches = %+v, want [true false true]", res.Nodes)
	}
}

func TestAPIEventRuleSimulateSequenceSteps(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"door": "open", "motion": true})
	srv := &Server{DB: client, Ctx: context.Background()}

	rule := client.DisplayRule.Create().
		SetName("seq").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition("{}").
		SetTriggerKind(triggerKindSequence).
		SetSequence(`{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":60}]}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	code, res := simulateRule(t, srv, rule.ID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !res.HistoryDependent {
		t.Fatal("sequence simulation must be flagged history-dependent")
	}
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.TriggerKind != triggerKindSequence {
		t.Fatalf("trigger_kind = %q", res.TriggerKind)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(res.Steps))
	}
	if res.Steps[0].Index != 0 || res.Steps[1].Index != 1 {
		t.Fatalf("step indexes = %+v", res.Steps)
	}
	if !res.Steps[0].Matched || !res.Steps[1].Matched {
		t.Fatalf("step matches = %+v", res.Steps)
	}
	if res.Steps[1].ForSeconds != 60 {
		t.Fatalf("step 1 hold = %d, want 60", res.Steps[1].ForSeconds)
	}
	// matched mirrors the completing (final) step only.
	if !res.Matched {
		t.Fatal("matched should reflect the final step immediately matching")
	}
	if len(res.Nodes) != 0 {
		t.Fatalf("sequence mode must not return nodes: %+v", res.Nodes)
	}
}

func TestAPIEventRuleSimulateSequenceReportsUnmatchedSteps(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"door": "open", "motion": false})
	srv := &Server{DB: client, Ctx: context.Background()}

	rule := client.DisplayRule.Create().
		SetName("seq-unmatched").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition("{}").
		SetTriggerKind(triggerKindSequence).
		SetSequence(`{"window_seconds":900,"steps":[{"condition":{"path":"door","operator":"eq","value":"open"},"for_seconds":0},{"condition":{"path":"motion","operator":"eq","value":true},"for_seconds":60}]}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	_, res := simulateRule(t, srv, rule.ID)
	if len(res.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(res.Steps))
	}
	if !res.Steps[0].Matched || res.Steps[1].Matched {
		t.Fatalf("step matches = %+v, want [true false]", res.Steps)
	}
	if res.Matched {
		t.Fatal("matched must not claim the sequence fires while the final step is unmatched")
	}
}

func TestAPIEventRuleSimulateSustainedIsInstantaneous(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"cpu": float64(90)})
	srv := &Server{DB: client, Ctx: context.Background()}

	rule := client.DisplayRule.Create().
		SetName("sustained").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"gt","value":80}`).
		SetForSeconds(600).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	_, res := simulateRule(t, srv, rule.ID)
	if !res.Matched {
		t.Fatal("sustained condition that matches now reports an instantaneous match")
	}
	if !res.HistoryDependent {
		t.Fatal("for_seconds>0 must be flagged history-dependent")
	}
	if res.ForSeconds != 600 {
		t.Fatalf("for_seconds = %d, want 600", res.ForSeconds)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].Path != "cpu" {
		t.Fatalf("nodes = %+v", res.Nodes)
	}
}

func TestAPIEventRuleSimulateReadOnly(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"cpu": float64(90)})
	srv := &Server{DB: client, Ctx: context.Background()}
	ResetNonCapableLogged()

	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc); unpinAll() })

	rule := client.DisplayRule.Create().
		SetName("readonly").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"gt","value":80}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SetThenActions(`{"kind":"notification","message":"rule fired"}`).
		SaveX(context.Background())

	before := client.Notification.Query().CountX(context.Background())

	_, res := simulateRule(t, srv, rule.ID)
	if !res.Matched {
		t.Fatalf("expected a match, got %+v", res)
	}

	if _, _, pinned := fc.IsPinned(); pinned {
		t.Fatal("simulation must not pin the feed")
	}
	if after := client.Notification.Query().CountX(context.Background()); after != before {
		t.Fatalf("simulation executed a then-action: notifications %d -> %d", before, after)
	}
}

func TestAPIEventRuleSimulateErrors(t *testing.T) {
	client := newEventRuleTestDB(t)
	useStateResolver(t, map[string]any{"cpu": float64(90)})
	srv := &Server{DB: client, Ctx: context.Background()}

	invalidCondition := client.DisplayRule.Create().
		SetName("bad-condition").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition(`{"path":"cpu","operator":"nope","value":80}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	_, res := simulateRule(t, srv, invalidCondition.ID)
	if res.Matched || res.Would {
		t.Fatalf("invalid condition must not match: %+v", res)
	}
	if res.Error == "" {
		t.Fatal("invalid condition must report an error")
	}

	badSeq := client.DisplayRule.Create().
		SetName("bad-sequence").
		SetEnabled(true).
		SetSourceType("systemstats").SetSourceID(0).
		SetCondition("{}").
		SetTriggerKind(triggerKindSequence).
		SetSequence(`{"window_seconds":900,"steps":[]}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	_, res = simulateRule(t, srv, badSeq.ID)
	if res.Matched || res.Would {
		t.Fatalf("invalid sequence must not match: %+v", res)
	}
	if res.Error == "" {
		t.Fatal("invalid sequence must report an error")
	}
}

func TestAPIEventRuleSimulateTargetNotResolvable(t *testing.T) {
	client := newEventRuleTestDB(t)
	orig := RuleTargetResolver
	RuleTargetResolver = func(string, int) (datasource.Datasource, bool) { return nil, false }
	t.Cleanup(func() { RuleTargetResolver = orig })
	srv := &Server{DB: client, Ctx: context.Background()}

	rule := client.DisplayRule.Create().
		SetName("missing-target").
		SetEnabled(true).
		SetSourceType("doesnotexist").SetSourceID(7).
		SetCondition(`{"path":"cpu","operator":"gt","value":80}`).
		SetCheckIntervalSeconds(30).SetCooldownSeconds(0).
		SaveX(context.Background())

	code, res := simulateRule(t, srv, rule.ID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if res.Matched || res.Would {
		t.Fatalf("unresolvable target must not match: %+v", res)
	}
	if res.Error != "target not resolvable" {
		t.Fatalf("error = %q, want target not resolvable", res.Error)
	}
	if res.State == nil || len(res.State) != 0 {
		t.Fatalf("state = %+v, want empty object", res.State)
	}
}

func TestAPIEventRuleSimulateNotFound(t *testing.T) {
	client := newEventRuleTestDB(t)
	srv := &Server{DB: client, Ctx: context.Background()}
	code, _ := simulateRule(t, srv, 4242)
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}
