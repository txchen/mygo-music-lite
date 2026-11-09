package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/lyrics"
	"mygo-music/internal/music"
)

// quietApp is Now Playing a song sung from 20 s to 30 s, quiet before and
// after, with two top comments.
func quietApp(pos time.Duration) (*app, music.Track, []lyrics.Line) {
	a := newTestApp()
	a.settings.HotComments = true
	tr := music.Track{ID: "9", Name: "晴天", Duration: 60000}
	lines := []lyrics.Line{
		{At: 0, End: 2 * time.Second, Text: "作词：周杰伦"},
		{At: 20 * time.Second, End: 25 * time.Second, Text: "故事的小黄花"},
		{At: 25 * time.Second, End: 30 * time.Second, Text: "从出生那年就飘着"},
	}
	a.pl.Cue([]music.Track{tr})
	a.pl.SetLyrics(lines)
	a.pl.Hold(pos)
	a.comments = commentState{track: "\x009", lib: a.library, id: "9", loaded: true, list: []music.Comment{
		{ID: "1", Author: "听众甲", Text: "刮风这天，我试过握着你手", Likes: 52000},
		{ID: "2", Author: "听众乙", Text: "从前从前，有个人爱你很久", Likes: 31000},
	}}
	return a, tr, lines
}

// A top comment shows through the quiet parts of a song, a few one after
// another in a long one, and none turned off or with none to show.
func TestQuietComment(t *testing.T) {
	a, tr, lines := quietApp(0)
	s := time.Second
	// The intro, 1.2 s in to 1 s before the singing, shows two comments
	// for 8.9 s each; the outro, three for 9.27 s, going on from them.
	for _, c := range []struct {
		pos  time.Duration
		want string
		next time.Duration
	}{
		{0, "", 1200 * time.Millisecond},
		{5 * s, "1", 5100 * time.Millisecond},
		{12 * s, "2", 7 * s},
		{22 * s, "", 9200 * time.Millisecond},
		{35 * s, "1", 5467 * time.Millisecond},
		{45 * s, "2", 4733 * time.Millisecond},
		{59500 * time.Millisecond, "", 0},
	} {
		cm, next := a.quietComment(&tr, lines, true, c.pos)
		var id string
		if cm != nil {
			id = cm.ID
		}
		if d := next - c.next; id != c.want || d > time.Millisecond || d < -time.Millisecond {
			t.Errorf("at %v: comment %q, next in %v; want %q, %v", c.pos, id, next, c.want, c.next)
		}
	}
	// Turned off, nothing shows.
	a.settings.HotComments = false
	if cm, _ := a.quietComment(&tr, lines, true, 5*s); cm != nil {
		t.Errorf("showed %q turned off", cm.Text)
	}
	a.settings.HotComments = true
	// No top comments, nothing shows.
	a.comments.list = nil
	if cm, _ := a.quietComment(&tr, lines, true, 5*s); cm != nil {
		t.Errorf("showed %q without top comments", cm.Text)
	}
}

// In the quiet, the lyrics pane and the stage show the top comment, which
// opens the comments.
func TestQuietCard(t *testing.T) {
	for _, pane := range []int{paneLyrics} {
		a, _, _ := quietApp(5 * time.Second)
		a.nowPlaying, a.npPane = true, pane
		tt := ui.NewTester(a.view, 1000, 640)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		if !tt.HasText("刮风这天，我试过握着你手") {
			t.Fatalf("pane %d: no top comment in %q", pane, tt.Texts())
		}
		if err := tt.Click("View Comments"); err != nil {
			t.Fatal(err)
		}
		if a.npPane != paneComments {
			t.Errorf("pane %d: the comment did not open the comments", pane)
		}
	}
}
