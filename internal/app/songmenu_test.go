package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// The menu behind a song's dots in Now Playing copies its title, with its
// artists, and the link its source gave it.
func TestSongMenuCopies(t *testing.T) {
	var copied string
	real := copyText
	copyText = func(s string) error { copied = s; return nil }
	defer func() { copyText = real }()

	a := newTestApp()
	tr := song("1859", "宝船", "DOUDOU", "小明")
	tr.URL = "https://example.com/song/1859"
	a.pl.Cue([]music.Track{tr})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()

	for _, c := range []struct{ item, want string }{
		{"Copy Title", "宝船"},
		{"Copy Title and Artists", "宝船 - DOUDOU, 小明"},
		{"Copy Link", "https://example.com/song/1859"},
	} {
		if err := tt.Click("More"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(c.item); err != nil {
			t.Fatalf("%v; the menu shows %q", err, tt.Menu())
		}
		tt.Frame()
		if copied != c.want {
			t.Errorf("%s copied %q, want %q", c.item, copied, c.want)
		}
	}
}

// A right click on a song's row opens its menu, without playing it, and a song
// with no page at its source offers no link to copy.
func TestSongMenuOnRightClick(t *testing.T) {
	var copied string
	real := copyText
	copyText = func(s string) error { copied = s; return nil }
	defer func() { copyText = real }()

	a := newTestApp()
	linked := song("2", "宝船", "DOUDOU")
	linked.URL = "https://example.com/song/2"
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "一张相片", "林宥嘉"), linked}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.RightClick("一张相片"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Copy Link"); err == nil {
		t.Error("offered the link of a song with none")
	}
	tt.CloseMenu()

	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Copy Link"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if copied != linked.URL {
		t.Errorf("copied %q, want %q", copied, linked.URL)
	}
	if a.pl.Current() != nil {
		t.Error("the right click played the song")
	}
}

// A song's menu goes to its artist's and album's pages, as far as its source
// has them.
func TestSongMenuGoesToPages(t *testing.T) {
	a := newTestApp()
	tr := music.Track{ID: "1", Name: "宝船", Artists: []music.Artist{{ID: "12", Name: "DOUDOU"}}, Album: music.Album{ID: "8", Name: "迷跡波"}, Duration: 200000}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{tr}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Go to Artist: DOUDOU"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	if a.route != (route{page: pageArtist, src: "fake", id: "12"}) {
		t.Fatalf("went to %+v", a.route)
	}
	tt.Key(0, ui.KeyBack)
	tt.Frame()

	without(a, "album")
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Go to Album: 迷跡波"); err == nil {
		t.Error("offered the page of an album its source has no pages of")
	}
	tt.CloseMenu()
}

// Downloading a song from its menu saves what its source streams where the
// listener picks, suggesting a name of its credits that can't lead out of the
// folder: "AC/DC" made a folder of "AC" instead.
func TestSongMenuDownloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("the song"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	suggested := make(chan string, 1)
	real := saveSongAs
	saveSongAs = func(a *app, name string) (string, error) {
		suggested <- name
		return filepath.Join(dir, "picked.mp3"), nil
	}
	defer func() { saveSongAs = real }()

	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "Back in Black", "AC/DC")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.RightClick("Back in Black"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Download…"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	if name := wait(t, suggested, "where to save it"); name != "AC_DC - Back in Black.mp3" {
		t.Errorf("suggested %q", name)
	}
	until(t, ch, func() bool {
		return len(a.downloadNotices) > 0 && a.downloadNotices[len(a.downloadNotices)-1].Type == "success"
	})
	if b, _ := os.ReadFile(filepath.Join(dir, "picked.mp3")); string(b) != "the song" {
		t.Errorf("saved %q", b)
	}
}

// A song that plays elsewhere, in its source's own app, offers no download.
func TestSongMenuDownloadsOnlyWhatStreams(t *testing.T) {
	a := newTestApp()
	fake(a).info.Playback = music.PlaybackRemote
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Download…"); err == nil {
		t.Error("offered to download a song that plays elsewhere")
	}
	tt.CloseMenu()
}
