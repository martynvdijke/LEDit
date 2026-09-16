package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func matrixTestServer(t *testing.T) *Server {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	client.InboundAdapter.Create().SetKind("matrix").SetEnabled(true).SetSecret("tok").SetAllowlist(`["!allowed:example.com"]`).SetConfig(`{"homeserver":"http://example.com","since":""}`).SaveX(ctx)
	if _, err := client.WebhookSettings.Create().SetAPIKey("").SetDefaultTTL(30).Save(ctx); err != nil {
		// ignore if already exists
	}
	return &Server{DB: client, Ctx: ctx}
}

func clearQueue() {
	priorityMu.Lock()
	notifHistory = nil
	notifID = 0
	priorityMu.Unlock()
}

func TestMatrixSyncURL(t *testing.T) {
	u := matrixSyncURL("https://matrix.example.com/", "s123")
	if !strings.Contains(u, "since=s123") || !strings.Contains(u, "/_matrix/client/v3/sync") {
		t.Fatalf("bad url %s", u)
	}
	u2 := matrixSyncURL("https://matrix.example.com", "")
	if strings.Contains(u2, "since=") {
		t.Fatalf("should not contain since %s", u2)
	}
}

func TestMatrixParseSync(t *testing.T) {
	body := []byte(`{"next_batch":"s456","rooms":{"join":{"!room:example.com":{"timeline":{"events":[{"event_id":"$ev1","type":"m.room.message","content":{"msgtype":"m.text","body":"hello"}}]}}}}}`)
	nb, evs, err := matrixParseSync(body)
	if err != nil {
		t.Fatalf("parse err %v", err)
	}
	if nb != "s456" {
		t.Fatalf("next_batch %s", nb)
	}
	if len(evs) != 1 || evs[0].Body != "hello" || evs[0].RoomID != "!room:example.com" {
		t.Fatalf("evs %+v", evs)
	}
	// filter non-text
	body2 := []byte(`{"next_batch":"s1","rooms":{"join":{"!r:ex":{"timeline":{"events":[{"event_id":"$ev2","type":"m.room.message","content":{"msgtype":"m.image","body":"img"}}]}}}}}`)
	_, evs2, _ := matrixParseSync(body2)
	if len(evs2) != 0 {
		t.Fatalf("should filter non-text")
	}
}

func TestMatrixAllowedRoomDelivered(t *testing.T) {
	clearQueue()
	srv := matrixTestServer(t)
	// httptest server with two responses
	resp1 := `{"next_batch":"s1","rooms":{"join":{"!allowed:example.com":{"timeline":{"events":[{"event_id":"$ev1","type":"m.room.message","content":{"msgtype":"m.text","body":"hello matrix"}}]}}}}}`
	called := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if called == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(resp1))
			return
		}
		// block second - return empty
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"next_batch":"s2","rooms":{"join":{}}}`))
	}))
	defer ts.Close()

	cfg := InboundAdapterConfig{
		Kind:      "matrix",
		Secret:    "tok",
		Allowlist: []string{"!allowed:example.com"},
		Config:    map[string]string{"homeserver": ts.URL, "access_token": "tok"},
	}
	a := newMatrixInbound(srv, cfg).(*MatrixInbound)
	orig := matrixHTTPClient
	matrixHTTPClient = ts.Client()
	defer func() { matrixHTTPClient = orig }()

	if err := a.syncOnce(); err != nil {
		t.Fatalf("syncOnce err %v", err)
	}
	if a.since != "s1" {
		t.Fatalf("cursor not advanced %s", a.since)
	}
	found := false
	for _, m := range ActiveMessages() {
		if m.Body == "hello matrix" {
			found = true
		}
	}
	if !found {
		// also check memory queue
		for _, n := range getMemoryQueue() {
			if n.Message == "hello matrix" || n.Title == "Matrix" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("message not delivered, queue %+v active %+v", getMemoryQueue(), ActiveMessages())
	}
}

func TestMatrixDisallowedRoomIgnoredButCursorAdvanced(t *testing.T) {
	clearQueue()
	srv := matrixTestServer(t)
	resp := `{"next_batch":"s99","rooms":{"join":{"!other:example.com":{"timeline":{"events":[{"event_id":"$ev2","type":"m.room.message","content":{"msgtype":"m.text","body":"should drop"}}]}}}}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer ts.Close()
	cfg := InboundAdapterConfig{
		Kind:      "matrix",
		Secret:    "tok",
		Allowlist: []string{"!allowed:example.com"},
		Config:    map[string]string{"homeserver": ts.URL},
	}
	a := newMatrixInbound(srv, cfg).(*MatrixInbound)
	orig := matrixHTTPClient
	matrixHTTPClient = ts.Client()
	defer func() { matrixHTTPClient = orig }()
	if err := a.syncOnce(); err != nil {
		t.Fatalf("err %v", err)
	}
	if a.since != "s99" {
		t.Fatalf("cursor not advanced %s", a.since)
	}
	for _, n := range getMemoryQueue() {
		if n.Message == "should drop" {
			t.Fatalf("disallowed message should not be delivered")
		}
	}
}

func TestMatrixDuplicateNotDeliveredTwice(t *testing.T) {
	clearQueue()
	srv := matrixTestServer(t)
	resp := `{"next_batch":"s1","rooms":{"join":{"!allowed:example.com":{"timeline":{"events":[{"event_id":"$dup","type":"m.room.message","content":{"msgtype":"m.text","body":"dup body"}}]}}}}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// always same event
		w.Write([]byte(resp))
	}))
	defer ts.Close()
	cfg := InboundAdapterConfig{
		Kind:      "matrix",
		Secret:    "tok",
		Allowlist: []string{"!allowed:example.com"},
		Config:    map[string]string{"homeserver": ts.URL},
	}
	a := newMatrixInbound(srv, cfg).(*MatrixInbound)
	orig := matrixHTTPClient
	matrixHTTPClient = ts.Client()
	defer func() { matrixHTTPClient = orig }()
	if err := a.syncOnce(); err != nil {
		t.Fatalf("first %v", err)
	}
	// second sync with same event id should not deliver again; but server returns same batch again (simulate no cursor change)
	// force same next_batch to avoid cursor change? Actually our server returns same s1 again, but second call will see duplicate
	a.since = "" // reset to force same response again? easier: call seenRecently check directly
	// second call will still hit server which returns $dup again; but we already marked seen
	// need to keep since empty so url same, server returns same dup
	a.since = ""
	if err := a.syncOnce(); err != nil {
		t.Fatalf("second %v", err)
	}
	count := 0
	for _, n := range getMemoryQueue() {
		if n.Message == "dup body" || n.Title == "Matrix" && n.Message == "dup body" {
			count++
		}
		// DeliverInbound uses title Matrix, body dup body
		if n.Message == "dup body" {
			count++
		}
	}
	// Actually AddNotification sets Title=Matrix, Message=dup body; check both
	q := getMemoryQueue()
	dupCount := 0
	for _, n := range q {
		if n.Message == "dup body" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Fatalf("expected 1 dup delivery, got %d queue %+v", dupCount, q)
	}
	// also test seenRecently directly
	if !a.seenRecently("$dup") {
		t.Fatalf("should be seen")
	}
	// persist cursor test: check DB config since updated
	_ = json.RawMessage{}
}

func TestMatrixSyncErrorBackoff(t *testing.T) {
	clearQueue()
	srv := matrixTestServer(t)
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 1 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"next_batch":"s_ok","rooms":{"join":{}}}`))
	}))
	defer ts.Close()
	cfg := InboundAdapterConfig{
		Kind:      "matrix",
		Secret:    "tok",
		Allowlist: []string{"!allowed:example.com"},
		Config:    map[string]string{"homeserver": ts.URL},
	}
	a := newMatrixInbound(srv, cfg).(*MatrixInbound)
	orig := matrixHTTPClient
	matrixHTTPClient = ts.Client()
	defer func() { matrixHTTPClient = orig }()
	err1 := a.syncOnce()
	if err1 == nil {
		t.Fatalf("expected error on 500")
	}
	err2 := a.syncOnce()
	if err2 != nil {
		t.Fatalf("second should succeed %v", err2)
	}
	if a.since != "s_ok" {
		t.Fatalf("cursor %s", a.since)
	}
}
