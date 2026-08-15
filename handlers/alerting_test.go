package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ledit/ent"
)

// ---------------------------------------------------------------------------
// Gotify sender
// ---------------------------------------------------------------------------

func TestGotifySenderPostsMessage(t *testing.T) {
	var mu sync.Mutex
	var gotKey, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/message" {
			t.Errorf("expected /message path, got %s", r.URL.Path)
		}
		gotKey = r.Header.Get("X-Gotify-Key")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(200)
	}))
	defer srv.Close()

	g := &GotifySender{ServerURL: srv.URL + "/", Token: "sekret"}
	err := g.Send(context.Background(), Alert{Title: "T", Message: "M", Priority: 5})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "sekret" {
		t.Errorf("expected X-Gotify-Key sekret, got %q", gotKey)
	}
	if gotBody == "" || !contains(gotBody, `"title":"T"`) || !contains(gotBody, `"priority":5`) {
		t.Errorf("unexpected body %q", gotBody)
	}
}

func TestGotifySenderDisabled(t *testing.T) {
	g := &GotifySender{ServerURL: "", Token: ""}
	if g.Enabled() {
		t.Fatal("expected disabled when empty")
	}
}

func TestGotifySenderErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	g := &GotifySender{ServerURL: srv.URL, Token: "t"}
	err := g.Send(context.Background(), Alert{Title: "T", Message: "M", Priority: 5})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestBuildEmailMessage(t *testing.T) {
	msg := buildEmailMessage("ledit@example.com", "me@example.com", "Subject here", "Body line")
	s := string(msg)
	if !contains(s, "From: ledit@example.com") ||
		!contains(s, "To: me@example.com") ||
		!contains(s, "Subject: Subject here") ||
		!contains(s, "Body line") {
		t.Errorf("unexpected message: %s", s)
	}
}

func TestEmailSenderDisabled(t *testing.T) {
	e := &EmailSender{}
	if e.Enabled() {
		t.Fatal("expected disabled when empty")
	}
	e = &EmailSender{Host: "smtp.example.com", Port: 587, From: "a@b.c", Recipient: "d@e.f"}
	if !e.Enabled() {
		t.Fatal("expected enabled with full config")
	}
}

// ---------------------------------------------------------------------------
// Alert engine
// ---------------------------------------------------------------------------

// captureSender collects delivered alerts for assertions.
type captureSender struct {
	enabled bool
	mu      sync.Mutex
	alerts  []Alert
	fail    bool
}

func (c *captureSender) Name() string  { return "capture" }
func (c *captureSender) Enabled() bool { return c.enabled }
func (c *captureSender) Send(ctx context.Context, a Alert) error {
	c.mu.Lock()
	c.alerts = append(c.alerts, a)
	c.mu.Unlock()
	if c.fail {
		return errTestDelivery
	}
	return nil
}
func (c *captureSender) got() []Alert {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Alert, len(c.alerts))
	copy(out, c.alerts)
	return out
}

var errTestDelivery = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test delivery error" }

func newTestEngine(sender *captureSender) *AlertEngine {
	return NewAlertEngine(func() []AlertSender { return []AlertSender{sender} })
}

func devices(devs ...*ent.DeviceSettings) []*ent.DeviceSettings { return devs }

func TestEngineSourceFailingTransition(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)
	eng.now = func() time.Time { return time.Unix(1000, 0) }

	reg := NewHealthRegistry()
	reg.RecordFailure("rssfeed:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("rssfeed:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("rssfeed:1", errTestDelivery, 10*time.Millisecond)

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.Tick(context.Background(), reg, nil, cfg)

	got := sender.got()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 alert, got %d", len(got))
	}
	if got[0].Priority != AlertPriorityFailing {
		t.Errorf("expected failing priority %d, got %d", AlertPriorityFailing, got[0].Priority)
	}

	// Second tick: no new alert (already failing).
	eng.Tick(context.Background(), reg, nil, cfg)
	if len(sender.got()) != 1 {
		t.Fatalf("expected no duplicate alert, got %d", len(sender.got()))
	}
}

func TestEngineNoAlertBelowThreshold(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)

	reg := NewHealthRegistry()
	reg.RecordFailure("weather:2", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("weather:2", errTestDelivery, 10*time.Millisecond)

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.Tick(context.Background(), reg, nil, cfg)

	if n := len(sender.got()); n != 0 {
		t.Fatalf("expected no alert below threshold, got %d", n)
	}
}

func TestEngineRecoveryAlert(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg := NewHealthRegistry()
	reg.RecordFailure("f1:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("f1:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("f1:1", errTestDelivery, 10*time.Millisecond)

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.now = func() time.Time { return base }
	eng.Tick(context.Background(), reg, nil, cfg)

	// Advance past the cooldown, then the source recovers.
	eng.now = func() time.Time { return base.Add(16 * time.Minute) }
	reg.RecordSuccess("f1:1", 10*time.Millisecond)
	eng.Tick(context.Background(), reg, nil, cfg)

	got := sender.got()
	if len(got) != 2 {
		t.Fatalf("expected failing + recovery alerts, got %d", len(got))
	}
	if got[1].Priority != AlertPriorityRecover {
		t.Errorf("expected recovery priority %d, got %d", AlertPriorityRecover, got[1].Priority)
	}
}

func TestEngineRecoverySuppressedWhenDisabled(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg := NewHealthRegistry()
	reg.RecordFailure("x:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("x:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("x:1", errTestDelivery, 10*time.Millisecond)

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: false}
	eng.now = func() time.Time { return base }
	eng.Tick(context.Background(), reg, nil, cfg)
	eng.now = func() time.Time { return base.Add(16 * time.Minute) }

	reg.RecordSuccess("x:1", 10*time.Millisecond)
	eng.Tick(context.Background(), reg, nil, cfg)

	if n := len(sender.got()); n != 1 {
		t.Fatalf("expected only the failing alert, got %d", n)
	}
}

func TestEngineCooldownSuppressesRepeat(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	reg := NewHealthRegistry()
	cfg := AlertConfig{FailureThreshold: 1, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}

	// Failing at base fires alert #1.
	eng.now = func() time.Time { return base }
	reg.RecordFailure("a:1", errTestDelivery, 10*time.Millisecond)
	eng.Tick(context.Background(), reg, nil, cfg)
	if n := len(sender.got()); n != 1 {
		t.Fatalf("expected failing alert, got %d", n)
	}

	// Within the cooldown: recovery is suppressed, and a fresh failing episode
	// cannot re-alert either.
	eng.now = func() time.Time { return base.Add(5 * time.Minute) }
	reg.RecordSuccess("a:1", 10*time.Millisecond)
	eng.Tick(context.Background(), reg, nil, cfg)
	reg.RecordFailure("a:1", errTestDelivery, 10*time.Millisecond)
	eng.Tick(context.Background(), reg, nil, cfg)
	if n := len(sender.got()); n != 1 {
		t.Fatalf("expected cooldown suppression, got %d alerts", n)
	}

	// After the cooldown expires a new failing episode alerts again.
	eng.now = func() time.Time { return base.Add(16 * time.Minute) }
	reg.RecordFailure("a:1", errTestDelivery, 10*time.Millisecond)
	eng.Tick(context.Background(), reg, nil, cfg)
	if n := len(sender.got()); n != 2 {
		t.Fatalf("expected re-alert after cooldown, got %d", n)
	}
}

func TestEngineDeviceStaleAndBackOnline(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)

	lastSeen := time.Now().Add(-30 * time.Minute)
	dev := &ent.DeviceSettings{ID: 7, Name: "kitchen", IP: "10.0.0.7", Enabled: true, RefreshInterval: 60}
	dev.LastSeenAt = &lastSeen

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.now = func() time.Time { return time.Now() }

	reg := NewHealthRegistry()
	eng.Tick(context.Background(), reg, devices(dev), cfg)

	got := sender.got()
	if len(got) != 1 {
		t.Fatalf("expected device offline alert, got %d", len(got))
	}
	if got[0].Priority != AlertPriorityStale {
		t.Errorf("expected stale priority %d, got %d", AlertPriorityStale, got[0].Priority)
	}

	// Device reconnects: back online alert.
	nowSeen := time.Now()
	dev.LastSeenAt = &nowSeen
	eng.now = func() time.Time { return time.Now().Add(time.Hour) }
	eng.Tick(context.Background(), reg, devices(dev), cfg)

	got = sender.got()
	if len(got) != 2 {
		t.Fatalf("expected back-online alert, got %d", len(got))
	}
	if got[1].Priority != AlertPriorityRecover {
		t.Errorf("expected recover priority %d, got %d", AlertPriorityRecover, got[1].Priority)
	}
}

func TestEngineShortCircuitsWhenNoChannel(t *testing.T) {
	sender := &captureSender{enabled: false}
	eng := newTestEngine(sender)

	reg := NewHealthRegistry()
	reg.RecordFailure("x:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("x:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("x:1", errTestDelivery, 10*time.Millisecond)

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.Tick(context.Background(), reg, nil, cfg)

	if n := len(sender.got()); n != 0 {
		t.Fatalf("expected no alerts with no channel enabled, got %d", n)
	}
}

func TestEngineDisabledDeviceIgnored(t *testing.T) {
	sender := &captureSender{enabled: true}
	eng := newTestEngine(sender)

	lastSeen := time.Now().Add(-30 * time.Minute)
	dev := &ent.DeviceSettings{ID: 3, Name: "old", IP: "10.0.0.3", Enabled: false, RefreshInterval: 60}
	dev.LastSeenAt = &lastSeen

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.now = func() time.Time { return time.Now() }
	eng.Tick(context.Background(), NewHealthRegistry(), devices(dev), cfg)

	if n := len(sender.got()); n != 0 {
		t.Fatalf("expected no alert for disabled device, got %d", n)
	}
}

func TestEngineDeliveryFailureLogged(t *testing.T) {
	sender := &captureSender{enabled: true, fail: true}
	eng := newTestEngine(sender)

	reg := NewHealthRegistry()
	reg.RecordFailure("y:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("y:1", errTestDelivery, 10*time.Millisecond)
	reg.RecordFailure("y:1", errTestDelivery, 10*time.Millisecond)

	cfg := AlertConfig{FailureThreshold: 3, CooldownMinutes: 15, StaleMultiplier: 3, NotifyRecovery: true}
	eng.Tick(context.Background(), reg, nil, cfg) // must not panic

	if n := len(sender.got()); n != 1 {
		t.Fatalf("expected 1 attempted alert, got %d", n)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
