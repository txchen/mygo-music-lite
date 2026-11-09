package player

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"mygo-music/internal/music"
)

// streamer answers the same stream for any track, counting how often it's asked.
type streamer struct {
	nothing
	st    music.Stream
	asked *int
}

func (s streamer) Info() music.Info { return music.Info{ID: "src", Playback: music.PlaybackStream} }
func (s streamer) Stream(id, quality string) (*music.Stream, error) {
	*s.asked++
	st := s.st
	return &st, nil
}

// Saving a song sends the headers required by its source.
func TestSaveSong(t *testing.T) {
	audio := []byte("ID3 not really a song")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		w.Write(audio)
	}))
	defer srv.Close()
	var asked int
	src := streamer{st: music.Stream{URL: srv.URL, Type: "flac", Headers: map[string]string{"Authorization": "Bearer token"}}, asked: &asked}
	p := New(func(func()) {})
	dir := t.TempDir()

	song, err := p.FindSong(src, music.Track{ID: "1", Source: "src"}, "high")
	if err != nil {
		t.Fatal(err)
	}
	if song.Type != "flac" {
		t.Errorf("type %q", song.Type)
	}
	path := filepath.Join(dir, "a.flac")
	if err := song.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, audio) {
		t.Errorf("saved %q", b)
	}

}

// A song cut short on its way leaves the file it was to replace as it was, and
// nothing beside it.
func TestSaveSongCutShort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(1000))
		w.Write([]byte("half"))
	}))
	defer srv.Close()
	var asked int
	src := streamer{st: music.Stream{URL: srv.URL, Type: "mp3"}, asked: &asked}
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	os.WriteFile(path, []byte("before"), 0o644)

	song, err := New(func(func()) {}).FindSong(src, music.Track{ID: "1", Source: "src"}, "high")
	if err != nil {
		t.Fatal(err)
	}
	if err := song.SaveTo(path); err == nil {
		t.Error("saved a song cut short")
	}
	if b, _ := os.ReadFile(path); string(b) != "before" {
		t.Errorf("left %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left %v", entries)
	}
}
