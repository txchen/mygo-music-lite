package app

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// skeletonScenes are the views that load, as they show while they do.
var skeletonScenes = []struct {
	name string
	set  func(a *app)
}{
	{"home", func(a *app) {}},
	{"playlist", func(a *app) {
		a.route = route{page: pagePlaylist, src: "fake", id: "7"}
		a.loadingPl["7"] = true
	}},
	{"search", func(a *app) {
		a.route = route{page: pageSearch, src: "fake", query: "迷星叫", kind: music.SearchTracks}
		a.searches[a.route] = &searchData{page: paged{loading: true}}
	}},
	{"artists", func(a *app) {
		a.route = route{page: pageSearch, src: "fake", query: "迷星叫", kind: music.SearchArtists}
		a.searches[a.route] = &searchData{page: paged{loading: true}}
	}},
	{"comments", func(a *app) {
		a.nowPlaying, a.npPane = true, paneComments
		a.comments = commentState{track: "\x001", lib: a.library, id: "1", loading: true}
	}},
	{"more-comments", func(a *app) {
		a.nowPlaying, a.npPane = true, paneComments
		a.comments = commentState{track: "\x001", lib: a.library, id: "1", loading: true, loaded: true, total: 1, list: []music.Comment{{ID: "1", Text: "好听"}}}
	}},
	{"lyrics", func(a *app) { a.nowPlaying, a.npPane = true, paneLyrics }},
}

func skeletonApp(set func(a *app)) *app {
	a := newTestApp()
	a.homeLoading = true
	a.pl.Cue([]music.Track{{ID: "1", Name: "迷星叫"}})
	set(a)
	return a
}

// With MM_FRAMES set, saves each skeleton in the light and the dark, the
// shimmer partway across.
func TestSkeletonFrames(t *testing.T) {
	dir := os.Getenv("MM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, s := range skeletonScenes {
		for _, dark := range []bool{false, true} {
			a := skeletonApp(s.set)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			tt.Advance(450 * time.Millisecond)
			tt.Frame()
			name := s.name
			if dark {
				name += "-dark"
			}
			saveFrame(t, dir, "skeleton-"+name+".png", tt.Image())
		}
	}
}
