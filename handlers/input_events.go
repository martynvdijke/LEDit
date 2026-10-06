package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Closed input vocabularies (add-physical-inputs D2). An `inputs`-negotiated
// v2 connection may send exactly one additive message shape:
//
//	{"type":"input","source":"...","event":"...","value":<string|number>,"ts":<epoch-ms>}
//
// Everything outside the vocabularies (or malformed/oversized) is dropped with
// a debug log and never disturbs the feed loop.
const (
	InputSourceButtonNext  = "button:next"
	InputSourceButtonPause = "button:pause"
	InputSourceEncoder     = "encoder"
	InputSourceNFC         = "nfc"
	InputSourcePIR         = "pir"
	InputSourceMMWave      = "mmwave"
	InputSourceLux         = "lux"

	InputEventPress    = "press"
	InputEventRotate   = "rotate"
	InputEventTap      = "tap"
	InputEventPresence = "presence"
	InputEventLux      = "lux"
)

// MaxInputEventBytes bounds one device→server input frame.
const MaxInputEventBytes = 512

// inputEventTypeList is the closed vocabulary advertised by the HA input
// event entity.
func inputEventTypeList() []string {
	return []string{InputEventPress, InputEventRotate, InputEventTap, InputEventPresence, InputEventLux}
}

const (
	maxInputTagLen = 64
	maxRotateStep  = 10
	maxLuxValue    = 200000
)

// InputEvent is one validated local-hardware event.
type InputEvent struct {
	Source string          `json:"source"`
	Event  string          `json:"event"`
	Value  json.RawMessage `json:"value,omitempty"`
	TS     int64           `json:"ts,omitempty"`
}

// ValueString returns the event value as a string (NFC tag ids, presence).
func (e InputEvent) ValueString() (string, bool) {
	var s string
	if len(e.Value) == 0 || json.Unmarshal(e.Value, &s) != nil {
		return "", false
	}
	return s, true
}

// ValueInt returns the event value as an integer (rotate steps).
func (e InputEvent) ValueInt() (int, bool) {
	if len(e.Value) == 0 {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(e.Value, &n); err != nil {
		return 0, false
	}
	return n, true
}

// ValueFloat returns the event value as a finite float (lux).
func (e InputEvent) ValueFloat() (float64, bool) {
	if len(e.Value) == 0 {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(e.Value, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func hasInputValue(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

func validInputSource(source string) bool {
	switch source {
	case InputSourceButtonNext, InputSourceButtonPause, InputSourceEncoder,
		InputSourceNFC, InputSourcePIR, InputSourceMMWave, InputSourceLux:
		return true
	}
	return false
}

// ParseInputEvent validates a raw device message against the closed
// vocabulary. Unexpected errors are descriptive for logging/tests; callers
// drop the event and keep the connection.
func ParseInputEvent(raw []byte) (InputEvent, error) {
	var ev InputEvent
	if len(raw) > MaxInputEventBytes {
		return ev, fmt.Errorf("input event exceeds %d bytes", MaxInputEventBytes)
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ev, fmt.Errorf("invalid input event json: %w", err)
	}
	if !validInputSource(ev.Source) {
		return ev, fmt.Errorf("unknown input source %q", ev.Source)
	}

	switch ev.Source {
	case InputSourceButtonNext, InputSourceButtonPause:
		if ev.Event != InputEventPress {
			return ev, fmt.Errorf("source %q only supports %q", ev.Source, InputEventPress)
		}
		if hasInputValue(ev.Value) {
			return ev, fmt.Errorf("button events must not carry a value")
		}
		ev.Value = nil
	case InputSourceEncoder:
		switch ev.Event {
		case InputEventRotate:
			n, ok := ev.ValueInt()
			if !ok || n == 0 {
				return ev, fmt.Errorf("rotate requires a nonzero integer step")
			}
			if n > maxRotateStep {
				n = maxRotateStep
			}
			if n < -maxRotateStep {
				n = -maxRotateStep
			}
			ev.Value, _ = json.Marshal(n)
		case InputEventPress:
			if hasInputValue(ev.Value) {
				return ev, fmt.Errorf("encoder press must not carry a value")
			}
			ev.Value = nil
		default:
			return ev, fmt.Errorf("source %q does not support event %q", ev.Source, ev.Event)
		}
	case InputSourceNFC:
		if ev.Event != InputEventTap {
			return ev, fmt.Errorf("source %q only supports %q", ev.Source, InputEventTap)
		}
		s, ok := ev.ValueString()
		if !ok || len(s) == 0 || len(s) > maxInputTagLen {
			return ev, fmt.Errorf("tap requires a tag id of 1-%d characters", maxInputTagLen)
		}
		for _, r := range s {
			if r < 0x21 || r > 0x7e {
				return ev, fmt.Errorf("tap tag id contains non-printable characters")
			}
		}
	case InputSourcePIR, InputSourceMMWave:
		if ev.Event != InputEventPresence {
			return ev, fmt.Errorf("source %q only supports %q", ev.Source, InputEventPresence)
		}
		s, ok := ev.ValueString()
		if !ok {
			var b bool
			if err := json.Unmarshal(ev.Value, &b); err == nil {
				if b {
					s = "present"
				} else {
					s = "absent"
				}
				ok = true
			}
		}
		if !ok || (s != "present" && s != "absent") {
			return ev, fmt.Errorf("presence value must be \"present\" or \"absent\"")
		}
		ev.Value, _ = json.Marshal(s)
	case InputSourceLux:
		if ev.Event != InputEventLux {
			return ev, fmt.Errorf("source %q only supports %q", ev.Source, InputEventLux)
		}
		f, ok := ev.ValueFloat()
		if !ok || f < 0 || f > maxLuxValue {
			return ev, fmt.Errorf("lux requires a finite value between 0 and %d", maxLuxValue)
		}
		ev.Value, _ = json.Marshal(f)
	default:
		return ev, fmt.Errorf("unknown input source %q", ev.Source)
	}

	if !validInputEventForSource(ev.Source, ev.Event) {
		return ev, fmt.Errorf("source %q does not support event %q", ev.Source, ev.Event)
	}
	return ev, nil
}

func validInputEventForSource(source, event string) bool {
	switch event {
	case InputEventPress:
		return source == InputSourceButtonNext || source == InputSourceButtonPause || source == InputSourceEncoder
	case InputEventRotate:
		return source == InputSourceEncoder
	case InputEventTap:
		return source == InputSourceNFC
	case InputEventPresence:
		return source == InputSourcePIR || source == InputSourceMMWave
	case InputEventLux:
		return source == InputSourceLux
	}
	return false
}

// inputRateLimiter is a per-connection token bucket: input events are
// best-effort, so excess is dropped instead of closing the feed.
type inputRateLimiter struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

func newInputRateLimiter(perSecond, burst float64) *inputRateLimiter {
	if perSecond <= 0 {
		perSecond = 20
	}
	if burst < 1 {
		burst = 40
	}
	return &inputRateLimiter{rate: perSecond, capacity: burst, tokens: burst}
}

func (l *inputRateLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.last.IsZero() {
		l.tokens = math.Min(l.capacity, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// Per-device input sinks, mirroring the feed-controller registry: registered
// by HandleDeviceWS so serveFeed's read loop needs no *Server.
var (
	inputSinkMu      sync.RWMutex
	deviceInputSinks = map[int]func(InputEvent){}
)

func registerInputSink(deviceID int, sink func(InputEvent)) {
	inputSinkMu.Lock()
	defer inputSinkMu.Unlock()
	deviceInputSinks[deviceID] = sink
}

func unregisterInputSink(deviceID int) {
	inputSinkMu.Lock()
	defer inputSinkMu.Unlock()
	delete(deviceInputSinks, deviceID)
	clearDeviceInputStatus(deviceID)
}

func getInputSink(deviceID int) (func(InputEvent), bool) {
	inputSinkMu.RLock()
	defer inputSinkMu.RUnlock()
	sink, ok := deviceInputSinks[deviceID]
	return sink, ok
}
