package handlers

import (
	"sort"
	"sync"
	"time"
)

// deviceLuxFreshness bounds how long a device-reported lux reading is trusted.
// It matches SensorFetchState.IsStale so device and HA sensor paths agree.
const deviceLuxFreshness = 60 * time.Second

type deviceLuxReading struct {
	lux float64
	at  time.Time
}

// deviceLuxCache stores the latest lux sample reported by each device over the
// input channel. Bounded by construction: one entry per device id.
var deviceLuxCache sync.Map // map[int]deviceLuxReading

func recordDeviceLux(deviceID int, lux float64) {
	deviceLuxCache.Store(deviceID, deviceLuxReading{lux: lux, at: time.Now()})
}

// freshDeviceLux returns the cached device lux when it is fresh enough to use.
func freshDeviceLux(deviceID int, now time.Time) (float64, bool) {
	v, ok := deviceLuxCache.Load(deviceID)
	if !ok {
		return 0, false
	}
	r, ok := v.(deviceLuxReading)
	if !ok || now.Sub(r.at) > deviceLuxFreshness {
		return 0, false
	}
	return r.lux, true
}

// deviceInputStatus summarizes seen input sources per device for the admin
// surface. Cleared when the device disconnects.
type deviceInputStatus struct {
	Sources   map[string]bool
	LastEvent string
	LastSeen  time.Time
}

var (
	deviceInputMu     sync.Mutex
	deviceInputStates = map[int]*deviceInputStatus{}
)

func recordDeviceInput(deviceID int, ev InputEvent) {
	deviceInputMu.Lock()
	defer deviceInputMu.Unlock()
	st, ok := deviceInputStates[deviceID]
	if !ok {
		st = &deviceInputStatus{Sources: map[string]bool{}}
		deviceInputStates[deviceID] = st
	}
	st.Sources[ev.Source] = true
	st.LastEvent = ev.Source + ":" + ev.Event
	st.LastSeen = time.Now()
}

// DeviceInputStatus returns a sorted copy of the recorded input status.
func DeviceInputStatus(deviceID int) (sources []string, lastEvent string, lastSeen time.Time, ok bool) {
	deviceInputMu.Lock()
	defer deviceInputMu.Unlock()
	st, found := deviceInputStates[deviceID]
	if !found {
		return nil, "", time.Time{}, false
	}
	sources = make([]string, 0, len(st.Sources))
	for s := range st.Sources {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	return sources, st.LastEvent, st.LastSeen, true
}

func clearDeviceInputStatus(deviceID int) {
	deviceInputMu.Lock()
	delete(deviceInputStates, deviceID)
	deviceInputMu.Unlock()
}
