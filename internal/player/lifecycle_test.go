package player

import (
	"testing"
	"time"

	"mygo-music/internal/music"
)

// An EOF queued before Pause must not start the next song after the listener pauses.
func TestPauseWinsQueuedEnd(t *testing.T) {
	p := New(func(fn func()) { fn() })
	p.Cue([]music.Track{{ID: "a"}, {ID: "b"}})
	var poll func()
	p.after = func(_ time.Duration, fn func()) { poll = fn }
	out := &fakeEngine{state: outputReady}
	p.openEngine = func(*music.Stream, bool) (engine, error) { return out, nil }
	p.loading, p.playing = true, true
	p.startEngine(&music.Stream{URL: "file:///a"}, engineLoad{})
	p.TogglePlay()
	out.state = outputEnded
	poll()
	if p.Current().ID != "a" || p.Playing() {
		t.Fatal("EOF resumed playback after pause")
	}
	p.Close()
}

// Paused native playback drains its scheduled poll and schedules no more work.
func TestPausedEngineStopsPolling(t *testing.T) {
	p := New(func(fn func()) { fn() })
	p.Cue([]music.Track{{ID: "a"}})
	var due []func()
	p.after = func(_ time.Duration, fn func()) { due = append(due, fn) }
	out := &fakeEngine{state: outputReady}
	p.openEngine = func(*music.Stream, bool) (engine, error) { return out, nil }
	p.loading, p.playing = true, true
	p.startEngine(&music.Stream{URL: "file:///a"}, engineLoad{})
	p.TogglePlay()
	if len(due) != 1 {
		t.Fatalf("scheduled %d polls", len(due))
	}
	poll := due[0]
	due = nil
	poll()
	if p.polling || len(due) != 0 || out.Playing() {
		t.Fatal("paused player kept polling")
	}
	p.TogglePlay()
	if !out.Playing() || len(due) != 1 {
		t.Fatal("resume did not restart observation")
	}
	p.Close()
}

type delayedSource struct {
	nothing
	started, release chan struct{}
}

func (s *delayedSource) Stream(string, string) (*music.Stream, error) {
	close(s.started)
	<-s.release
	return &music.Stream{URL: "file:///a"}, nil
}

// Closing during source resolution must not create a native player when the old result arrives.
func TestCloseDuringLoad(t *testing.T) {
	posted := make(chan func(), 4)
	p := New(func(fn func()) { posted <- fn })
	src := &delayedSource{started: make(chan struct{}), release: make(chan struct{})}
	p.Source = func(string) music.Source { return src }
	opened := 0
	p.openEngine = func(*music.Stream, bool) (engine, error) { opened++; return &fakeEngine{state: outputReady}, nil }
	p.PlayQueue([]music.Track{{ID: "a"}}, 0)
	<-src.started
	p.Close()
	close(src.release)
	for range 2 {
		select {
		case fn := <-posted:
			fn()
		case <-time.After(time.Second):
			t.Fatal("source or lyrics result never arrived")
		}
	}
	if opened != 0 || p.Ready() || p.Playing() {
		t.Fatal("closed load reopened playback")
	}
}

// Network playback gives native engines only a loopback URL and releases it on Close.
func TestNetworkStreamUsesRelay(t *testing.T) {
	p := New(func(fn func()) { fn() })
	p.Cue([]music.Track{{ID: "a"}})
	p.loading, p.playing = true, true
	st := &music.Stream{URL: "https://example.com/audio", Headers: map[string]string{"Authorization": "Bearer secret"}}
	p.openEngine = func(got *music.Stream, _ bool) (engine, error) {
		if got.URL == st.URL || got.Headers != nil || got.AuthorizeURL != nil {
			t.Fatal("native engine received upstream stream")
		}
		return &fakeEngine{state: outputReady}, nil
	}
	p.startEngine(st, engineLoad{dir: stay})
	if p.Stream() != st || p.relay == nil {
		t.Fatal("original stream or relay lost")
	}
	relay := p.relay
	p.Close()
	if relay.ctx.Err() == nil || p.relay != nil {
		t.Fatal("Close left relay active")
	}
}
