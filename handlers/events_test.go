package handlers

import (
	"sync"
	"testing"
	"time"
)

func TestBusEmitDelivers(t *testing.T) {
	b := &Bus{}
	var got Event
	b.Subscribe(func(e Event) { got = e })
	b.Emit(Event{Type: EventSourceChanged, Data: map[string]string{"from": "a", "to": "b"}})
	if got.Type != EventSourceChanged {
		t.Fatalf("want source_changed got %s", got.Type)
	}
}
func TestBusConcurrentEmit(t *testing.T) {
	b := &Bus{}
	var mu sync.Mutex
	count := 0
	b.Subscribe(func(e Event) { mu.Lock(); count++; mu.Unlock() })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { b.Emit(Event{Type: EventFeedPaused, Timestamp: time.Now()}); wg.Done() }()
	}
	wg.Wait()
	if count != 50 {
		t.Fatalf("want 50 got %d", count)
	}
}
