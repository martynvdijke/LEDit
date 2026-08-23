package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/render"
)

func newEventRuleTestDB(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(context.Background())
	return client
}

type testStateProvider struct {
	state map[string]any
}

func (t *testStateProvider) GetPNG(width, height int) (*render.RenderedImage, error) { return nil, nil }
func (t *testStateProvider) CurrentState(ctx context.Context) (map[string]any, error) {
	return t.state, nil
}

var _ datasource.Datasource = &testStateProvider{}
var _ datasource.StateProvider = &testStateProvider{}

type nonCapableDS struct{}

func (n *nonCapableDS) GetPNG(width, height int) (*render.RenderedImage, error) { return nil, nil }

var _ datasource.Datasource = &nonCapableDS{}

func TestEvaluator_PinUnpin(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()
	ga := client.GenericAPI.Create().SetToken("t").SetURL("http://x").SetConfig(`{"title":"test"}`).SaveX(ctx)
	state := map[string]any{"temp": float64(30)}
	fake := &testStateProvider{state: state}
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, id int) (datasource.Datasource, bool) {
		if st == "genericapi" && id == ga.ID {
			return fake, true
		}
		return nil, false
	}
	t.Cleanup(func() { RuleTargetResolver = orig })
	ResetNonCapableLogged()
	fc1 := &FeedController{}
	fc2 := &FeedController{}
	joinController(fc1)
	joinController(fc2)
	t.Cleanup(func() { leaveController(fc1); leaveController(fc2) })
	rule := client.DisplayRule.Create().SetName("hot").SetEnabled(true).SetSourceType("genericapi").SetSourceID(ga.ID).SetCondition(`{"path":"temp","operator":"gt","value":25}`).SetCheckIntervalSeconds(5).SetCooldownSeconds(0).SaveX(ctx)
	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc1.IsPinned(); !ok {
		t.Fatal("expected pinned after true condition")
	}
	fake.state = map[string]any{"temp": float64(10)}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc1.IsPinned(); ok {
		t.Fatal("expected unpinned after false")
	}
	if _, _, ok := fc2.IsPinned(); ok {
		t.Fatal("fc2 should also be unpinned")
	}
}

func TestEvaluator_NonCapableLoggedOnce(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, id int) (datasource.Datasource, bool) {
		if st == "images" {
			return &nonCapableDS{}, true
		}
		return nil, false
	}
	t.Cleanup(func() { RuleTargetResolver = orig })
	ResetNonCapableLogged()
	rule := client.DisplayRule.Create().SetName("r").SetEnabled(true).SetSourceType("images").SetSourceID(1).SetCondition(`{"path":"x","operator":"exists","value":null}`).SetCheckIntervalSeconds(5).SetCooldownSeconds(0).SaveX(ctx)
	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	EvaluateRulesOnce(client, states)
	nonCapableMu.Lock()
	cnt := len(nonCapableLogged)
	nonCapableMu.Unlock()
	if cnt != 1 {
		t.Fatalf("expected 1 logged entry got %d", cnt)
	}
}

func TestEvaluator_CooldownSuppress(t *testing.T) {
	client := newEventRuleTestDB(t)
	fake := &testStateProvider{state: map[string]any{"v": float64(10)}}
	orig := RuleTargetResolver
	RuleTargetResolver = func(st string, id int) (datasource.Datasource, bool) { return fake, true }
	t.Cleanup(func() { RuleTargetResolver = orig })
	ResetNonCapableLogged()
	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })
	rule := client.DisplayRule.Create().SetName("r").SetEnabled(true).SetSourceType("genericapi").SetSourceID(1).SetCondition(`{"path":"v","operator":"gt","value":5}`).SetCheckIntervalSeconds(5).SetCooldownSeconds(10).SaveX(context.Background())
	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("should pin")
	}
	fake.state = map[string]any{"v": float64(1)}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("cooldown should suppress unpin")
	}
}

func TestJitter_Bounds(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := jitter(100)
		if v < 90 || v > 110 {
			t.Fatalf("jitter out of bounds %v", v)
		}
	}
}
