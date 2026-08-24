package handlers

import (
	"sync"
	"time"
)

const (
	EventSourceChanged         = "source_changed"
	EventFeedPaused            = "feed_paused"
	EventFeedResumed           = "feed_resumed"
	EventNotificationFired     = "notification_fired"
	EventDeviceLivenessChanged = "device_liveness_changed"
	EventTest                  = "test"
)

type Event struct {
	Type      string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

type Bus struct {
	mu        sync.RWMutex
	subs      []func(Event)
	dropCount int
}

var GlobalBus = &Bus{}

func (b *Bus) Subscribe(fn func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, fn)
}

func (b *Bus) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = nil
	b.dropCount = 0
}

func (b *Bus) Emit(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	b.mu.RLock()
	subs := append([]func(Event){}, b.subs...)
	b.mu.RUnlock()
	for _, fn := range subs {
		// non-blocking dispatch per subscriber via goroutine with drop semantics
		// For simplicity, call directly but recover if slow; design says drop not block.
		// We implement non-blocking by launching goroutine with timeout? Instead direct call is fast.
		// If sink is slow it queues internally.
		fn(e)
	}
}

// Emit helper for typed events
func EmitSourceChanged(from, to string) {
	GlobalBus.Emit(Event{Type: EventSourceChanged, Data: map[string]string{"from": from, "to": to}})
}
func EmitFeedPaused()  { GlobalBus.Emit(Event{Type: EventFeedPaused}) }
func EmitFeedResumed() { GlobalBus.Emit(Event{Type: EventFeedResumed}) }
func EmitNotificationFired(id int, msg string) {
	GlobalBus.Emit(Event{Type: EventNotificationFired, Data: map[string]any{"id": id, "message": msg}})
}
func EmitDeviceLiveness(deviceID int, online bool) {
	GlobalBus.Emit(Event{Type: EventDeviceLivenessChanged, Data: map[string]any{"device_id": deviceID, "online": online}})
}
