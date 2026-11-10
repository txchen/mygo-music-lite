// Package diagnostics keeps opt-in, bounded playback evidence in memory. Only
// fixed event labels and numeric measurements belong here, never URLs or errors.
package diagnostics

import (
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const capacity = 16384

var enabled atomic.Bool
var sequence atomic.Uint64
var mu sync.Mutex
var events = make([]Event, capacity)
var count, next uint64

type Event struct {
	Time  time.Time `json:"time"`
	ID    uint64    `json:"id"`
	Stage string    `json:"stage"`
	Value int64     `json:"value"`
}

func Enabled() bool { return enabled.Load() }
func ID() uint64    { return sequence.Add(1) }

// Set preserves existing evidence when stopped; starting again clears it.
func Set(on bool) {
	mu.Lock()
	defer mu.Unlock()
	if on && !enabled.Load() {
		count, next = 0, 0
	}
	enabled.Store(on)
}
func Record(id uint64, stage string, value int64) {
	if !enabled.Load() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if !enabled.Load() {
		return
	}
	events[next%capacity] = Event{time.Now().UTC(), id, stage, value}
	next++
	if count < capacity {
		count++
	}
}
func Snapshot(version string) ([]byte, error) {
	mu.Lock()
	snapshot := make([]Event, count)
	for i := range snapshot {
		snapshot[i] = events[(next-count+uint64(i))%capacity]
	}
	dropped := next - count
	mu.Unlock()
	return json.MarshalIndent(struct {
		Version string  `json:"app_version"`
		OS      string  `json:"os"`
		Arch    string  `json:"arch"`
		Format  int     `json:"format"`
		Dropped uint64  `json:"dropped_events"`
		Events  []Event `json:"events"`
	}{version, runtime.GOOS, runtime.GOARCH, 1, dropped, snapshot}, "", "  ")
}
