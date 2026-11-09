package app

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"mygo-music/internal/music"
	"mygo-music/internal/player"
)

// The session is kept a while after playback changes and every while as
// it plays, and paused nothing more is due. It was kept by a ticker that
// woke the app, and redrew its window, every half minute even at rest.
func TestSessionRestsWhilePaused(t *testing.T) {
	var due []func()
	defer func(after func(time.Duration, func())) { sessionAfter = after }(sessionAfter)
	sessionAfter = func(_ time.Duration, fn func()) { due = append(due, fn) }

	a := newTestApp()
	a.pl.Changed = a.keepSession
	a.pl.Cue([]music.Track{song("7", "宝船")})
	out := &testTransport{}
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	defer out.Close()

	a.pl.TogglePlay()
	a.pl.TogglePlay()
	if len(due) != 1 {
		t.Fatalf("%d saves due after pausing and playing", len(due))
	}
	due[0]()
	if len(due) != 2 {
		t.Fatalf("playing, %d saves due after one, want another", len(due)-1)
	}

	a.pl.TogglePlay()
	due[1]()
	if len(due) != 2 {
		t.Errorf("paused, %d more saves due", len(due)-2)
	}
}

// keptSession keeps a session as the app would have on quitting, and takes it
// away with the test.
func keptSession(t *testing.T, s player.Session) {
	t.Helper()
	path, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
}

// Started again, the app picks up the station it played, paused where it was,
// and the station's source goes on feeding it.
func TestRestoreSessionReattachesRadio(t *testing.T) {
	asked := make(chan string, 4)
	a := newTestApp()
	ch := posts(a)
	fake(a).stationNext = func(id string) ([]music.Track, error) {
		asked <- id
		tr := song("3", "三")
		tr.Source = "fake"
		return []music.Track{tr}, nil
	}
	name := radioName("fake", &music.Station{ID: "st", Name: "Night Drive"})
	tr := song("1", "一")
	tr.Source = "fake"
	keptSession(t, player.Session{Queue: []music.Track{tr}, Order: []int{0}, At: 0, Position: 5000, Radio: name})

	a.restoreSession()
	if a.pl.Radio() != name || a.pl.Playing() {
		t.Fatalf("radio %q, playing %v", a.pl.Radio(), a.pl.Playing())
	}
	if id := wait(t, asked, "the station's next songs"); id != "st" {
		t.Fatalf("asked station %q", id)
	}
	until(t, ch, func() bool { return len(a.pl.Queue()) == 2 })
	if radioLabel(a.pl.Radio()) != "Night Drive" {
		t.Errorf("the radio is called %q", radioLabel(a.pl.Radio()))
	}
}

// A session whose radio's source went away picks up its songs without a crash,
// the radio adding nothing more.
func TestRestoreSessionOfGoneSource(t *testing.T) {
	a := newTestApp()
	tr := song("1", "一")
	tr.Source = "gone"
	keptSession(t, player.Session{Queue: []music.Track{tr}, Order: []int{0}, At: 0, Radio: radioName("gone", &music.Station{ID: "st", Name: "Night Drive"})})

	a.restoreSession()
	if cur := a.pl.Current(); cur == nil || cur.ID != "1" || len(a.pl.Queue()) != 1 {
		t.Errorf("playing %+v of %d", cur, len(a.pl.Queue()))
	}
}
