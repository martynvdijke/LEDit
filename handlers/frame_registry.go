package handlers

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// lastFrame is a buffered snapshot of the most recent frame a live connection
// emitted for one target+size. Embed clients and GET /api/frame read it so a
// late joiner sees the wall immediately without touching the live feed.
type lastFrame struct {
	Data     []byte
	Format   string
	Source   string
	Next     string
	Width    int
	Height   int
	Video    bool
	Captured time.Time
}

// frameRegistryMaxEntries bounds memory: one entry per distinct target+size.
// Frames are overwritten in place, so this only grows with the number of
// embed/preview geometries actually in use.
const frameRegistryMaxEntries = 128

var (
	frameRegistryMu sync.Mutex
	frameRegistry   = map[string]*lastFrame{}
)

// frameTarget names the buffered-frame namespace for a connection: "shared"
// for the anonymous wall feed, "device:<id>" for a device overlay/preview.
func frameTarget(deviceID int) string {
	if deviceID > 0 {
		return fmt.Sprintf("device:%d", deviceID)
	}
	return "shared"
}

func frameRegistryKey(target string, w, h int) string {
	return fmt.Sprintf("%s@%dx%d", target, w, h)
}

// storeFrame records the physical frame (post-slice, post-brightness) for a
// target+size. The slice is copied so later mutation by the caller is safe.
func storeFrame(target string, w, h int, fr *lastFrame) {
	if fr == nil || len(fr.Data) == 0 || w <= 0 || h <= 0 {
		return
	}
	cp := make([]byte, len(fr.Data))
	copy(cp, fr.Data)
	entry := &lastFrame{
		Data:     cp,
		Format:   strings.ToUpper(strings.TrimSpace(fr.Format)),
		Source:   fr.Source,
		Next:     fr.Next,
		Width:    w,
		Height:   h,
		Video:    fr.Video,
		Captured: time.Now(),
	}

	frameRegistryMu.Lock()
	defer frameRegistryMu.Unlock()
	key := frameRegistryKey(target, w, h)
	if _, exists := frameRegistry[key]; !exists && len(frameRegistry) >= frameRegistryMaxEntries {
		// Evict the oldest entry to keep the registry bounded.
		oldestKey := ""
		var oldest time.Time
		for k, v := range frameRegistry {
			if oldestKey == "" || v.Captured.Before(oldest) {
				oldestKey, oldest = k, v.Captured
			}
		}
		if oldestKey != "" {
			delete(frameRegistry, oldestKey)
		}
	}
	frameRegistry[key] = entry
}

// lookupFrame returns a copy of the buffered frame for a target+size.
func lookupFrame(target string, w, h int) (*lastFrame, bool) {
	frameRegistryMu.Lock()
	defer frameRegistryMu.Unlock()
	fr, ok := frameRegistry[frameRegistryKey(target, w, h)]
	if !ok {
		return nil, false
	}
	cp := make([]byte, len(fr.Data))
	copy(cp, fr.Data)
	out := *fr
	out.Data = cp
	return &out, true
}

// clearFrameRegistry drops all buffered frames; used by tests.
func clearFrameRegistry() {
	frameRegistryMu.Lock()
	frameRegistry = map[string]*lastFrame{}
	frameRegistryMu.Unlock()
}

// frameAge is the time since the buffered frame was captured.
func (fr *lastFrame) frameAge(now time.Time) time.Duration {
	if fr == nil || fr.Captured.IsZero() {
		return 0
	}
	return now.Sub(fr.Captured)
}
