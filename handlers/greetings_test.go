package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ledit/ent"

	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

// greetingTestDBSeq gives every test client its own in-memory SQLite database;
// a shared DSN would leak rows between clients and cross-fire rules.
var greetingTestDBSeq atomic.Int64

func newTestGreetingClient(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:memdb_greet_%d?mode=memory&_fk=1", greetingTestDBSeq.Add(1))
	drv, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("schema create: %v", err)
	}
	return client
}

// greetingNotifCount counts notifications persisted in the test's own DB.
// GetNotificationHistory merges a package-global in-memory queue and is not
// isolation-safe for assertions.
func greetingNotifCount(t *testing.T, ctx context.Context, client *ent.Client) int {
	t.Helper()
	n, err := client.Notification.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

func TestResolveTemplate(t *testing.T) {
	r := &ent.GreetingRule{Name: "Maria", TTLSeconds: 30}
	now := time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC)
	got := ResolveTemplate("Welcome home, {name}! at {time} state {entity} until {until}", r, "home", now)
	if !strings.Contains(got, "Maria") || !strings.Contains(got, "10:05") || !strings.Contains(got, "home") {
		t.Fatalf("unexpected template %q", got)
	}
}

func TestSanitize(t *testing.T) {
	r := &ent.GreetingRule{Name: "x", TTLSeconds: 30}
	now := time.Now()
	got := ResolveTemplate("{entity}", r, "<script>alert(1)</script>", now)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Fatalf("not sanitized %q", got)
	}
	// truncation
	long := strings.Repeat("a", 300)
	got = ResolveTemplate(long, r, "", now)
	if len(got) > 200 {
		t.Fatalf("not truncated %d", len(got))
	}
}

func TestInQuietHours(t *testing.T) {
	s, e := "22:00", "07:00"
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }
	if !InQuietHours(at(23, 0), &s, &e) {
		t.Fatal("expected quiet")
	}
	if InQuietHours(at(8, 0), &s, &e) {
		t.Fatal("expected not quiet")
	}
	// simple non-wrap
	s2, e2 := "13:00", "14:00"
	if !InQuietHours(at(13, 30), &s2, &e2) {
		t.Fatal("expected quiet simple")
	}
	if InQuietHours(at(15, 0), &s2, &e2) {
		t.Fatal("expected not quiet simple")
	}
}

func TestWatcherEdge(t *testing.T) {
	client := newTestGreetingClient(t)
	defer client.Close()
	ctx := context.Background()
	srv := &Server{DB: client, Ctx: ctx}
	_, err := client.GreetingRule.Create().SetName("Maria").SetEntityPath("person.maria").SetMatchValue("home").SetMessageTemplate("Hi {name}").SetTTLSeconds(30).SetCooldownMinutes(30).SetEnabled(true).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state := "not_home"
	fetcher := func(ctx context.Context, p string) (string, error) { return state, nil }
	w := &GreetingWatcher{client: client, fetcher: fetcher, server: srv, prevState: map[int]string{}, lastPush: map[int]time.Time{}, lastRepush: map[int]time.Time{}}
	now := time.Now()
	w.tick(ctx, now) // init
	if greetingNotifCount(t, ctx, client) != 0 {
		t.Fatal("should not fire on init")
	}
	state = "home"
	w.tick(ctx, now.Add(time.Second))
	if n := greetingNotifCount(t, ctx, client); n != 1 {
		t.Fatalf("expected 1 push, got %d", n)
	}
	// staying home not re-fire
	w.tick(ctx, now.Add(2*time.Second))
	if greetingNotifCount(t, ctx, client) != 1 {
		t.Fatal("should not re-fire while staying")
	}
	// cooldown suppress: leave and return quickly
	state = "not_home"
	w.tick(ctx, now.Add(3*time.Second))
	state = "home"
	w.tick(ctx, now.Add(4*time.Second))
	if greetingNotifCount(t, ctx, client) != 1 {
		t.Fatal("cooldown should suppress")
	}
}

func TestStartupAlreadyHomeNoFire(t *testing.T) {
	client := newTestGreetingClient(t)
	defer client.Close()
	ctx := context.Background()
	srv := &Server{DB: client, Ctx: ctx}
	_, _ = client.GreetingRule.Create().SetName("Maria").SetEntityPath("person.maria").SetMatchValue("home").SetMessageTemplate("Hi").SetTTLSeconds(30).SetCooldownMinutes(30).SetEnabled(true).Save(ctx)
	fetcher := func(ctx context.Context, p string) (string, error) { return "home", nil }
	w := &GreetingWatcher{client: client, fetcher: fetcher, server: srv, prevState: map[int]string{}, lastPush: map[int]time.Time{}, lastRepush: map[int]time.Time{}}
	w.tick(ctx, time.Now())
	if greetingNotifCount(t, ctx, client) != 0 {
		t.Fatal("startup already home should not fire")
	}
}

func TestHAFetchFailure(t *testing.T) {
	client := newTestGreetingClient(t)
	defer client.Close()
	ctx := context.Background()
	srv := &Server{DB: client, Ctx: ctx}
	_, _ = client.GreetingRule.Create().SetName("Maria").SetEntityPath("person.maria").SetMatchValue("home").SetMessageTemplate("Hi").SetTTLSeconds(30).SetCooldownMinutes(30).SetEnabled(true).Save(ctx)
	state := "not_home"
	fetchErr := false
	fetcher := func(ctx context.Context, p string) (string, error) {
		if fetchErr {
			return "", context.DeadlineExceeded
		}
		return state, nil
	}
	w := &GreetingWatcher{client: client, fetcher: fetcher, server: srv, prevState: map[int]string{}, lastPush: map[int]time.Time{}, lastRepush: map[int]time.Time{}}
	now := time.Now()
	w.tick(ctx, now)
	fetchErr = true
	w.tick(ctx, now.Add(time.Second)) // should skip without losing prev
	fetchErr = false
	state = "home"
	// need to ensure edge still detected after failed tick
	// prev was not_home, still should fire
	w.tick(ctx, now.Add(2*time.Second))
	// edge must still fire after failed ticks; assert on the test's own DB
	if greetingNotifCount(t, ctx, client) == 0 {
		t.Fatal("expected push after fetch recovery")
	}
}

func TestQuietHoursSuppress(t *testing.T) {
	client := newTestGreetingClient(t)
	defer client.Close()
	ctx := context.Background()
	srv := &Server{DB: client, Ctx: ctx}
	qs, qe := "22:00", "07:00"
	_, _ = client.GreetingRule.Create().SetName("Maria").SetEntityPath("person.maria").SetMatchValue("home").SetMessageTemplate("Hi").SetTTLSeconds(30).SetCooldownMinutes(30).SetQuietHoursStart(qs).SetQuietHoursEnd(qe).SetEnabled(true).Save(ctx)
	state := "not_home"
	fetcher := func(ctx context.Context, p string) (string, error) { return state, nil }
	w := &GreetingWatcher{client: client, fetcher: fetcher, server: srv, prevState: map[int]string{}, lastPush: map[int]time.Time{}, lastRepush: map[int]time.Time{}}
	quietTime := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	w.tick(ctx, quietTime)
	state = "home"
	w.tick(ctx, quietTime.Add(time.Second))
	if greetingNotifCount(t, ctx, client) != 0 {
		t.Fatal("quiet hours should suppress")
	}
}

func TestMeetingRoomRepin(t *testing.T) {
	client := newTestGreetingClient(t)
	defer client.Close()
	ctx := context.Background()
	srv := &Server{DB: client, Ctx: ctx}
	_, _ = client.GreetingRule.Create().SetName("Room").SetEntityPath("binary_sensor.room_occupied").SetMatchValue("on").SetMessageTemplate("Room busy until {until}").SetTTLSeconds(60).SetCooldownMinutes(30).SetEnabled(true).Save(ctx)
	// init with on -> dedupe
	fetcher := func(ctx context.Context, p string) (string, error) { return "on", nil }
	w := &GreetingWatcher{client: client, fetcher: fetcher, server: srv, prevState: map[int]string{}, lastPush: map[int]time.Time{}, lastRepush: map[int]time.Time{}}
	base := time.Now()
	w.tick(ctx, base) // init, no fire
	// second tick staying on should re-pin after ttl/2? Our logic re-pins only after lastRepush set. First edge never fired due to dedupe, so re-pin not yet. Simulate not_home->on transition
	client2 := newTestGreetingClient(t)
	defer client2.Close()
	srv2 := &Server{DB: client2, Ctx: ctx}
	_, _ = client2.GreetingRule.Create().SetName("Room").SetEntityPath("binary_sensor.room_occupied").SetMatchValue("on").SetMessageTemplate("Room busy until {until}").SetTTLSeconds(60).SetCooldownMinutes(30).SetEnabled(true).Save(ctx)
	state := "off"
	f2 := func(ctx context.Context, p string) (string, error) { return state, nil }
	w2 := &GreetingWatcher{client: client2, fetcher: f2, server: srv2, prevState: map[int]string{}, lastPush: map[int]time.Time{}, lastRepush: map[int]time.Time{}}
	w2.tick(ctx, base)
	state = "on"
	w2.tick(ctx, base.Add(time.Second))
	if n := greetingNotifCount(t, ctx, client2); n != 1 {
		t.Fatalf("expected first fire %d", n)
	}
	// staying on, re-push after 30s
	w2.tick(ctx, base.Add(10*time.Second))
	if greetingNotifCount(t, ctx, client2) != 1 {
		t.Fatal("should not re-pin too early")
	}
	w2.tick(ctx, base.Add(35*time.Second))
	if greetingNotifCount(t, ctx, client2) != 2 {
		t.Fatalf("expected repin %d", greetingNotifCount(t, ctx, client2))
	}
}
