package handlers

import (
	"encoding/json"
	"testing"
	"time"
)

func resetRateBuckets() {
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()
}

func resetNotifHistory() {
	priorityMu.Lock()
	notifHistory = nil
	notifID = 0
	priorityMu.Unlock()
}

func TestInboundPriorityMappers(t *testing.T) {
	t.Run("ntfy", func(t *testing.T) {
		cases := []struct {
			in   int
			want int
		}{
			{0, inboundPriorityLow},
			{1, inboundPriorityLow},
			{2, inboundPriorityNormal},
			{3, inboundPriorityNormal},
			{4, inboundPriorityHigh},
			{5, inboundPriorityUrgent},
			{6, inboundPriorityUrgent},
			{10, inboundPriorityUrgent},
			{-1, inboundPriorityLow},
		}
		for _, c := range cases {
			if got := inboundPriorityFromNtfy(c.in); got != c.want {
				t.Errorf("inboundPriorityFromNtfy(%d)=%d want %d", c.in, got, c.want)
			}
		}
	})
	t.Run("gotify", func(t *testing.T) {
		cases := []struct {
			in   int
			want int
		}{
			{0, inboundPriorityLow},
			{1, inboundPriorityLow},
			{2, inboundPriorityLow},
			{3, inboundPriorityNormal},
			{6, inboundPriorityNormal},
			{7, inboundPriorityHigh},
			{8, inboundPriorityHigh},
			{9, inboundPriorityUrgent},
			{10, inboundPriorityUrgent},
			{15, inboundPriorityUrgent},
		}
		for _, c := range cases {
			if got := inboundPriorityFromGotify(c.in); got != c.want {
				t.Errorf("inboundPriorityFromGotify(%d)=%d want %d", c.in, got, c.want)
			}
		}
	})
	t.Run("pushover", func(t *testing.T) {
		cases := []struct {
			in   int
			want int
		}{
			{-3, inboundPriorityLow},
			{-2, inboundPriorityLow},
			{-1, inboundPriorityNormal},
			{0, inboundPriorityNormal},
			{1, inboundPriorityHigh},
			{2, inboundPriorityUrgent},
			{3, inboundPriorityUrgent},
		}
		for _, c := range cases {
			if got := inboundPriorityFromPushover(c.in); got != c.want {
				t.Errorf("inboundPriorityFromPushover(%d)=%d want %d", c.in, got, c.want)
			}
		}
	})
}

func TestInboundAllowed(t *testing.T) {
	cases := []struct {
		name      string
		allowlist []string
		sourceID  string
		want      bool
	}{
		{"empty allowlist denies", nil, "chan1", false},
		{"empty slice denies", []string{}, "chan1", false},
		{"wildcard allows", []string{"*"}, "chan1", true},
		{"exact match allows", []string{"chan1", "chan2"}, "chan1", true},
		{"unlisted denies", []string{"chan1", "chan2"}, "chan3", false},
		{"empty sourceID denies even with wildcard", []string{"*"}, "", false},
		{"empty sourceID denies even with exact", []string{"chan1"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inboundAllowed(c.allowlist, c.sourceID); got != c.want {
				t.Errorf("inboundAllowed(%v, %q)=%v want %v", c.allowlist, c.sourceID, got, c.want)
			}
		})
	}
}

func TestAdmitInbound(t *testing.T) {
	resetRateBuckets()
	t.Cleanup(resetRateBuckets)

	t.Run("empty allowlist denies", func(t *testing.T) {
		resetRateBuckets()
		cfg := InboundAdapterConfig{Kind: "test-admit-empty-" + t.Name(), Allowlist: nil}
		msg := InboundMessage{SourceID: "chan1"}
		ok, retry := admitInbound(cfg, msg)
		if ok || retry != 0 {
			t.Fatalf("expected deny with retry 0, got ok=%v retry=%d", ok, retry)
		}
	})

	t.Run("allowed source admits", func(t *testing.T) {
		resetRateBuckets()
		kind := "test-admit-ok-" + t.Name()
		cfg := InboundAdapterConfig{Kind: kind, Allowlist: []string{"chan1"}}
		msg := InboundMessage{SourceID: "chan1"}
		ok, retry := admitInbound(cfg, msg)
		if !ok || retry != 0 {
			t.Fatalf("expected admit, got ok=%v retry=%d", ok, retry)
		}
	})

	t.Run("source throttle", func(t *testing.T) {
		resetRateBuckets()
		kind := "test-admit-src-" + t.Name()
		cfg := InboundAdapterConfig{Kind: kind, Allowlist: []string{"chan1"}}
		msg := InboundMessage{SourceID: "chan1"}
		for i := 0; i < inboundSourceRateLimit; i++ {
			ok, _ := admitInbound(cfg, msg)
			if !ok {
				t.Fatalf("call %d should admit", i+1)
			}
		}
		ok, retry := admitInbound(cfg, msg)
		if ok {
			t.Fatal("expected source throttle deny")
		}
		if retry < 1 {
			t.Fatalf("expected retry >=1, got %d", retry)
		}
	})

	t.Run("kind throttle", func(t *testing.T) {
		resetRateBuckets()
		kind := "test-admit-kind-" + t.Name()
		cfg := InboundAdapterConfig{Kind: kind, Allowlist: []string{"*"}}
		for i := 0; i < inboundKindRateLimit; i++ {
			// Distinct SourceID each time to avoid source throttle (20/min).
			suffix := ""
			n := i
			for n > 0 || suffix == "" {
				suffix = string(rune('a'+n%26)) + suffix
				n /= 26
				if n == 0 {
					break
				}
			}
			msg := InboundMessage{SourceID: "ksrc-" + t.Name() + "-" + suffix}
			ok, retry := admitInbound(cfg, msg)
			if !ok {
				t.Fatalf("kind call %d should admit, retry=%d", i+1, retry)
			}
		}
		msg := InboundMessage{SourceID: "ksrc-final-" + t.Name() + "-zzz"}
		ok, retry := admitInbound(cfg, msg)
		if ok {
			t.Fatal("expected kind throttle deny")
		}
		if retry < 1 {
			t.Fatalf("expected retry >=1, got %d", retry)
		}
	})
}

func TestDeliverInboundTTLClamp(t *testing.T) {
	srv := newTestServerWithDB(t)
	defaultTTL := srv.webhookDefaultTTL()
	if defaultTTL != 30 {
		t.Logf("webhookDefaultTTL default is %d (not hardcoded 30)", defaultTTL)
	}

	cases := []struct {
		name    string
		ttl     time.Duration
		wantSec int
	}{
		{"zero falls back to default", 0, defaultTTL},
		{"negative falls back to default", -5 * time.Second, defaultTTL},
		{"over max clamps to 3600", 7200 * time.Second, 3600},
		{"normal honored", 120 * time.Second, 120},
		{"one second", 1 * time.Second, 1},
		{"just over max", 3601 * time.Second, 3600},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetNotifHistory()
			msg := InboundMessage{
				Source:   "test",
				SourceID: "chan1",
				Title:    "ttl test",
				Body:     "body",
				TTL:      c.ttl,
			}
			srv.DeliverInbound(msg)
			q := getMemoryQueue()
			if len(q) == 0 {
				t.Fatal("no notification queued")
			}
			last := q[len(q)-1]
			got := int(last.ExpiresAt.Sub(last.CreatedAt).Seconds() + 0.5)
			if got != c.wantSec {
				t.Errorf("TTL %v: got %d want %d (expires %v created %v)", c.ttl, got, c.wantSec, last.ExpiresAt, last.CreatedAt)
			}
		})
	}
}

func TestDeliverInboundPropagatesPriorityAndMedia(t *testing.T) {
	srv := newTestServerWithDB(t)
	resetNotifHistory()
	t.Cleanup(resetNotifHistory)

	msg := InboundMessage{
		Source:   "ntfy",
		SourceID: "chan1",
		Title:    "priority test",
		Body:     "hello",
		Priority: inboundPriorityHigh,
		MediaURL: "https://example.com/img.jpg",
		TTL:      60 * time.Second,
	}
	srv.DeliverInbound(msg)
	q := getMemoryQueue()
	if len(q) == 0 {
		t.Fatal("no notification queued")
	}
	last := q[len(q)-1]
	if last.Priority != inboundPriorityHigh {
		t.Errorf("queued priority=%d want %d", last.Priority, inboundPriorityHigh)
	}
	if last.Media == nil || last.Media.URL != "https://example.com/img.jpg" {
		t.Fatalf("queued media=%+v want URL https://example.com/img.jpg", last.Media)
	}
	// Converted message should also carry priority and media.
	m := NotificationToMessage(last)
	if m.Priority != inboundPriorityHigh {
		t.Errorf("message priority=%d want %d", m.Priority, inboundPriorityHigh)
	}
	if m.Media == nil || m.Media.URL != "https://example.com/img.jpg" {
		t.Fatalf("message media=%+v want URL https://example.com/img.jpg", m.Media)
	}

	// MediaPath variant.
	resetNotifHistory()
	msg2 := InboundMessage{
		Source:    "gotify",
		SourceID:  "chan2",
		Title:     "path test",
		Body:      "body",
		MediaPath: "/media/local.jpg",
		TTL:       60 * time.Second,
	}
	srv.DeliverInbound(msg2)
	q = getMemoryQueue()
	if len(q) == 0 {
		t.Fatal("no notification queued for path test")
	}
	last = q[len(q)-1]
	if last.Media == nil || last.Media.Path != "/media/local.jpg" {
		t.Fatalf("queued media path=%+v want /media/local.jpg", last.Media)
	}
}

func TestLegacyNotifEntryJSONUnchanged(t *testing.T) {
	entry := notifEntry{
		ID:      1,
		Title:   "hello",
		Message: "world",
		Time:    "12:00:00",
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["priority"]; ok {
		t.Error("legacy JSON should not contain priority")
	}
	if _, ok := m["media"]; ok {
		t.Error("legacy JSON should not contain media")
	}
	if _, ok := m["Priority"]; ok {
		t.Error("legacy JSON should not contain Priority (capital)")
	}
	if _, ok := m["Media"]; ok {
		t.Error("legacy JSON should not contain Media (capital)")
	}
	// Ensure basic keys still present.
	for _, k := range []string{"id", "title", "message", "time"} {
		if _, ok := m[k]; !ok {
			t.Errorf("legacy JSON missing %q", k)
		}
	}
}

func TestInboundSecretOK(t *testing.T) {
	cases := []struct {
		name string
		want string
		got  string
		ok   bool
	}{
		{"match", "secret123", "secret123", true},
		{"mismatch", "secret123", "wrong", false},
		{"empty want", "", "secret123", false},
		{"empty got", "secret123", "", false},
		{"both empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inboundSecretOK(c.want, c.got); got != c.ok {
				t.Errorf("inboundSecretOK(%q,%q)=%v want %v", c.want, c.got, got, c.ok)
			}
		})
	}
}
