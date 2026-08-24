package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ledit/ent"
)

// Sink interface
type Sink interface {
	Handle(Event) error
	Enabled() bool
	Name() string
}

// Dispatcher fans out to sinks
type Dispatcher struct {
	mu    sync.RWMutex
	sinks []Sink
}

var GlobalDispatcher = &Dispatcher{}

func (d *Dispatcher) Register(s Sink) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sinks = append(d.sinks, s)
}
func (d *Dispatcher) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sinks = nil
}
func (d *Dispatcher) Dispatch(e Event) {
	d.mu.RLock()
	sinks := append([]Sink{}, d.sinks...)
	d.mu.RUnlock()
	for _, s := range sinks {
		if !s.Enabled() {
			continue
		}
		_ = s.Handle(e)
	}
}

// --- MetricsSink ---

type MetricsSink struct {
	mu          sync.Mutex
	counters    map[string]int64
	gauges      map[string]float64
	enabled     bool
	eventsTotal map[string]int64
}

func NewMetricsSink() *MetricsSink {
	return &MetricsSink{
		counters:    make(map[string]int64),
		gauges:      make(map[string]float64),
		eventsTotal: make(map[string]int64),
		enabled:     false,
	}
}
func (m *MetricsSink) Name() string { return "metrics" }
func (m *MetricsSink) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}
func (m *MetricsSink) SetEnabled(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = v
}
func (m *MetricsSink) Handle(e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsTotal[e.Type]++
	return nil
}
func (m *MetricsSink) IncCounter(key string, labels string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key+"{"+labels+"}"]++
}
func (m *MetricsSink) SetGauge(key string, val float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[key] = val
}
func (m *MetricsSink) Snapshot() (map[string]int64, map[string]float64, map[string]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := make(map[string]int64, len(m.counters))
	for k, v := range m.counters {
		c[k] = v
	}
	g := make(map[string]float64, len(m.gauges))
	for k, v := range m.gauges {
		g[k] = v
	}
	et := make(map[string]int64, len(m.eventsTotal))
	for k, v := range m.eventsTotal {
		et[k] = v
	}
	return c, g, et
}
func (m *MetricsSink) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = make(map[string]int64)
	m.gauges = make(map[string]float64)
	m.eventsTotal = make(map[string]int64)
}

var GlobalMetricsSink = NewMetricsSink()

type MqttSink struct {
	mu      sync.Mutex
	enabled bool
}

func NewMqttSink() *MqttSink     { return &MqttSink{} }
func (m *MqttSink) Name() string { return "mqtt" }
func (m *MqttSink) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}
func (m *MqttSink) SetEnabled(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = v
}
func (m *MqttSink) Handle(e Event) error {
	switch e.Type {
	case EventSourceChanged:
		if data, ok := e.Data.(map[string]string); ok {
			PublishOutbound("ledit/status/current_source", data["to"], true)
		}
	case EventFeedPaused:
		PublishOutbound("ledit/status/paused", "true", true)
	case EventFeedResumed:
		PublishOutbound("ledit/status/paused", "false", true)
	case EventDeviceLivenessChanged:
		if data, ok := e.Data.(map[string]any); ok {
			if id, ok := data["device_id"].(int); ok {
				online, _ := data["online"].(bool)
				val := "false"
				if online {
					val = "true"
				}
				PublishOutbound(fmt.Sprintf("ledit/device/%d/online", id), val, true)
			}
		}
	}
	return nil
}

var GlobalMqttSink = NewMqttSink()
var GlobalWebhookSink *WebhookSink

// --- WebhookSink ---

type Delivery struct {
	TargetID  int       `json:"target_id"`
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
}

type WebhookSink struct {
	mu         sync.Mutex
	enabled    bool
	client     *http.Client
	queue      chan Event
	deliveries []Delivery
	// db accessor
	db func() *ent.Client
	// now for testing
	sleep func(time.Duration)
}

func NewWebhookSink(db func() *ent.Client) *WebhookSink {
	ws := &WebhookSink{
		client: &http.Client{Timeout: 5 * time.Second},
		queue:  make(chan Event, 100),
		db:     db,
		sleep:  time.Sleep,
	}
	go ws.loop()
	return ws
}
func (w *WebhookSink) Name() string { return "webhook" }
func (w *WebhookSink) Enabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enabled
}
func (w *WebhookSink) SetEnabled(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.enabled = v
}
func (w *WebhookSink) Handle(e Event) error {
	select {
	case w.queue <- e:
	default:
		// drop oldest if full
		select {
		case <-w.queue:
		default:
		}
		select {
		case w.queue <- e:
		default:
		}
		slog.Warn("webhook queue overflow, dropped event", "event", e.Type)
	}
	return nil
}
func (w *WebhookSink) loop() {
	for e := range w.queue {
		w.dispatch(e)
	}
}
func (w *WebhookSink) dispatch(e Event) {
	if w.db == nil {
		return
	}
	client := w.db()
	if client == nil {
		return
	}
	ctx := context.Background()
	hooks, err := client.OutboundWebhook.Query().All(ctx)
	if err != nil {
		return
	}
	for _, h := range hooks {
		if !h.Enabled {
			continue
		}
		w.sendToTarget(h, e)
	}
}

func (w *WebhookSink) sendToTarget(hook *ent.OutboundWebhook, e Event) {
	payload, _ := json.Marshal(map[string]any{
		"event":     e.Type,
		"timestamp": e.Timestamp.Format(time.RFC3339),
		"data":      e.Data,
	})
	// compute signature
	sig := ""
	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(payload)
		sig = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	backoffs := []time.Duration{1 * time.Second, 5 * time.Second, 25 * time.Second}
	var lastErr string
	status := "success"
	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			d := backoffs[attempt-1]
			// jitter ±10%
			jitter := time.Duration(rand.Int63n(int64(d)/5)) - d/10
			d += jitter
			if w.sleep != nil {
				w.sleep(d)
			} else {
				time.Sleep(d)
			}
		}
		req, _ := http.NewRequest("POST", hook.URL, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LEDit-Event", e.Type)
		if sig != "" {
			req.Header.Set("X-LEDit-Signature", sig)
		}
		resp, err := w.client.Do(req)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			lastErr = ""
			status = "success"
			break
		}
		lastErr = fmt.Sprintf("http %d", resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			status = "failed"
			break
		}
		status = "failed"
		// retry on 5xx and 429
		if attempt == 3 {
			break
		}
	}
	w.addDelivery(Delivery{TargetID: hook.ID, Event: e.Type, Timestamp: time.Now(), Status: status, Error: lastErr})
}

func (w *WebhookSink) addDelivery(d Delivery) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deliveries = append(w.deliveries, d)
	if len(w.deliveries) > 50 {
		w.deliveries = w.deliveries[len(w.deliveries)-50:]
	}
}
func (w *WebhookSink) Deliveries() []Delivery {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Delivery, len(w.deliveries))
	copy(out, w.deliveries)
	return out
}
func (w *WebhookSink) SendTest(hook *ent.OutboundWebhook) Delivery {
	e := Event{Type: EventTest, Timestamp: time.Now(), Data: map[string]string{"msg": "test"}}
	payload, _ := json.Marshal(map[string]any{"event": e.Type, "timestamp": e.Timestamp.Format(time.RFC3339), "data": e.Data})
	sig := ""
	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(payload)
		sig = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	req, _ := http.NewRequest("POST", hook.URL, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LEDit-Event", e.Type)
	if sig != "" {
		req.Header.Set("X-LEDit-Signature", sig)
	}
	resp, err := w.client.Do(req)
	status := "success"
	errStr := ""
	if err != nil {
		status = "failed"
		errStr = err.Error()
	} else {
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			status = "failed"
			errStr = fmt.Sprintf("http %d", resp.StatusCode)
		}
	}
	d := Delivery{TargetID: hook.ID, Event: e.Type, Timestamp: time.Now(), Status: status, Error: errStr}
	w.addDelivery(d)
	return d
}

// Validation helpers
func ValidateWebhookURL(u string) error {
	if len(u) > 2048 {
		return fmt.Errorf("url too long")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url must be http(s)://")
	}
	if parsed.Host == "" {
		return fmt.Errorf("url must have host")
	}
	return nil
}
func ValidateSecret(s string) error {
	if len(s) > 1024 {
		return fmt.Errorf("secret too long")
	}
	return nil
}

// Helpers for outbound settings
func EnsureOutboundSettings(client *ent.Client) *ent.OutboundSettings {
	if client == nil {
		return nil
	}
	ctx := context.Background()
	rows, _ := client.OutboundSettings.Query().All(ctx)
	if len(rows) > 0 {
		return rows[0]
	}
	r, _ := client.OutboundSettings.Create().SetMqttPublishEnabled(false).SetMetricsEnabled(false).SetWebhooksEnabled(false).SetHaDiscoveryEnabled(false).Save(ctx)
	return r
}

func InitOutbound(s *Server) {
	GlobalWebhookSink = NewWebhookSink(func() *ent.Client {
		if s != nil {
			return s.DB
		}
		return nil
	})
	// init settings
	st := EnsureOutboundSettings(s.DB)
	if st != nil {
		GlobalMqttSink.SetEnabled(st.MqttPublishEnabled)
		GlobalMetricsSink.SetEnabled(st.MetricsEnabled)
		GlobalWebhookSink.SetEnabled(st.WebhooksEnabled)
	}
	GlobalDispatcher.Reset()
	GlobalDispatcher.Register(GlobalMqttSink)
	GlobalDispatcher.Register(GlobalMetricsSink)
	GlobalDispatcher.Register(GlobalWebhookSink)
	GlobalBus.Reset()
	GlobalBus.Subscribe(func(e Event) { GlobalDispatcher.Dispatch(e) })
}

// Utility to trim
func init() {
	_ = strings.TrimSpace
}
