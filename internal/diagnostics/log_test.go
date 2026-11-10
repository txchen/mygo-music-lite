package diagnostics

import (
	"encoding/json"
	"testing"
)

// Stopping preserves evidence, restarting clears it, and overflow keeps newest events.
func TestRecordingLifecycle(t *testing.T) {
	Set(false)
	Set(true)
	defer Set(false)
	for i := 0; i < capacity+3; i++ {
		Record(1, "position_ms", int64(i))
	}
	Set(false)
	Record(1, "ignored", 0)
	data, err := Snapshot("test")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Dropped uint64  `json:"dropped_events"`
		Events  []Event `json:"events"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Dropped != 3 || len(result.Events) != capacity || result.Events[0].Value != 3 || result.Events[capacity-1].Value != capacity+2 {
		t.Fatal("lost or misordered evidence")
	}
	Set(true)
	data, _ = Snapshot("test")
	json.Unmarshal(data, &result)
	if len(result.Events) != 0 {
		t.Fatal("new session retained old events")
	}
}
