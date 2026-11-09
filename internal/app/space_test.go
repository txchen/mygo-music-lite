package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// testTransport records UI commands without an audio device or wall clock.
type testTransport struct {
	pos     time.Duration
	playing bool
}

func (s *testTransport) Play()                   { s.playing = true }
func (s *testTransport) Pause()                  { s.playing = false }
func (s *testTransport) Playing() bool           { return s.playing }
func (s *testTransport) Seek(t time.Duration)    { s.pos = t }
func (s *testTransport) Position() time.Duration { return s.pos }
func (s *testTransport) Duration() time.Duration { return 200 * time.Second }
func (s *testTransport) SetVolume(float64)       {}
func (s *testTransport) Close()                  {}

// Space pauses right after a click opened Now Playing: the button clicked
// has the focus then, and would take Space as another click.
func TestSpaceAfterOpeningNowPlaying(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]music.Track{song("7", "宝船", "DOUDOU")})
	a.pl.SetLyrics(nil)
	out := &testTransport{}
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	defer out.Close()

	tt := ui.NewTester(a.view, 1200, 800)
	if err := tt.Click("Open Now Playing"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if !a.nowPlaying {
		t.Fatal("now playing did not open")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if a.pl.Playing() {
		t.Error("Space did not pause")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if !a.pl.Playing() {
		t.Error("Space did not play again")
	}
}
