package player

import (
	"path/filepath"
	"testing"
	"time"

	"mygo-music/internal/music"
)

func TestPlayEmptyQueue(t *testing.T) {
	p := &Player{at: -1}
	p.PlayQueue(nil, 0) // an empty playlist's play button
	if p.Current() != nil {
		t.Error("playing from an empty queue")
	}
}

// A radio asks for more as its queue nears its end, and adds what is not
// in it already.
func TestRadioRefills(t *testing.T) {
	p := &Player{at: -1}
	p.Cue([]music.Track{{ID: "1"}, {ID: "2"}})
	asked := 0
	p.AttachRadio("station", func(got func([]music.Track)) {
		asked++
		got([]music.Track{{ID: "2"}, {ID: "3"}})
	})
	if asked != 1 || len(p.Queue()) != 3 || p.Queue()[2].ID != "3" {
		t.Fatalf("asked %d, queue %v", asked, p.Queue())
	}
	if p.Radio() != "station" {
		t.Errorf("radio %q", p.Radio())
	}
	// Playing something else ends the radio.
	p.endRadio()
	if p.Radio() != "" || p.feed != nil {
		t.Error("radio still on")
	}
}

// Leaving a radio keeps the song playing and those before it, drops those
// it queued after, and asks it for no more.
func TestEndRadio(t *testing.T) {
	p := &Player{at: -1}
	p.Cue([]music.Track{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}})
	p.at, p.track = 1, &p.queue[1]
	asked := 0
	p.AttachRadio("station", func(got func([]music.Track)) { asked++ })
	p.EndRadio()
	if p.Radio() != "" || p.Current().ID != "2" {
		t.Fatalf("radio %q, current %v", p.Radio(), p.Current())
	}
	if q := p.Queue(); len(q) != 2 || q[0].ID != "1" || q[1].ID != "2" {
		t.Fatalf("queue %v", q)
	}
	p.refill()
	if asked != 0 {
		t.Error("asked the radio after leaving it")
	}
}

// picky plays the songs in ok, from one on disk, and says the rest aren't
// available.
type picky struct {
	nothing
	ok map[string]bool
}

func (s picky) Stream(id, _ string) (*music.Stream, error) {
	if !s.ok[id] {
		return nil, music.ErrUnavailable
	}
	path, err := filepath.Abs("testdata/seek.mp3")
	return &music.Stream{URL: "file://" + filepath.ToSlash(path), Type: "mp3"}, err
}

// pickyQueue is a, b and c, of which b can't play.
func pickyQueue() (*Player, chan func(), []music.Track) {
	ch := make(chan func(), 64)
	p := New(func(fn func()) { ch <- fn })
	p.SetVolume(0)
	src := picky{ok: map[string]bool{"a": true, "c": true}}
	p.Source = func(string) music.Source { return src }
	return p, ch, []music.Track{{ID: "a", Source: "s"}, {ID: "b", Source: "s"}, {ID: "c", Source: "s"}}
}

// settle runs what the player posts until it loaded what it went on to.
func settle(t *testing.T, ch chan func(), p *Player) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for p.Loading() {
		select {
		case fn := <-ch:
			fn()
		case <-deadline:
			t.Fatal("still loading")
		}
	}
}

// A song the listener picks that can't play stays, saying why. Clicking one
// the source refused for a moment used to play the one after it instead.
func TestPickedTrackStays(t *testing.T) {
	p, ch, q := pickyQueue()
	defer p.stop()
	p.PlayTrack(q, 1)
	settle(t, ch, p)
	if p.Current().ID != "b" || p.Playing() || p.Err() == "" {
		t.Fatalf("on %s, playing %v, err %q", p.Current().ID, p.Playing(), p.Err())
	}
	p.Jump(0)
	settle(t, ch, p)
	p.Jump(1)
	settle(t, ch, p)
	if p.Current().ID != "b" || p.Playing() {
		t.Errorf("jumped to %s, playing %v", p.Current().ID, p.Playing())
	}
}

// Previous passes a song that can't play going back, as Next does going
// forward. It used to skip forward, back onto the song playing, so Previous
// seemed to do nothing.
func TestPrevSkipsBack(t *testing.T) {
	p, ch, q := pickyQueue()
	defer p.stop()
	p.PlayTrack(q, 2)
	settle(t, ch, p)
	p.Prev()
	settle(t, ch, p)
	if p.Current().ID != "a" || !p.Playing() {
		t.Fatalf("previous went to %s, playing %v", p.Current().ID, p.Playing())
	}
	p.Next()
	settle(t, ch, p)
	if p.Current().ID != "c" || !p.Playing() {
		t.Errorf("next went to %s, playing %v", p.Current().ID, p.Playing())
	}
}

// Shuffling a whole list may start on any song, not always its first; a
// song picked to start stays first.
func TestShuffleOrderHead(t *testing.T) {
	p := &Player{order: make([]int, 10)}
	heads := map[int]bool{}
	for range 200 {
		for i := range p.order {
			p.order[i] = i
		}
		p.shuffleOrder(-1)
		heads[p.order[0]] = true
	}
	if len(heads) < 5 {
		t.Errorf("shuffled lists start on only %v", heads)
	}
	p.shuffleOrder(3)
	if p.order[0] != 3 {
		t.Errorf("picked song is at %v", p.order)
	}
}
