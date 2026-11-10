package player

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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

// YouTube full-file requests can stall; downloads must request bounded ranges and assemble every byte.
func TestSaveSongRanges(t *testing.T) {
	audio := bytes.Repeat([]byte("audio-data"), 250000)
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		if r.Header.Get("Range") == "" {
			http.Error(w, "range required", 400)
			return
		}
		w.Header().Set("ETag", `"same-file"`)
		http.ServeContent(w, r, "audio.m4a", time.Time{}, bytes.NewReader(audio))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "song.m4a")
	song := &Song{st: &music.Stream{URL: srv.URL}}
	if err := song.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, audio) {
		t.Fatal("download incomplete or reordered")
	}
	if len(ranges) != 3 {
		t.Fatalf("requests %v", ranges)
	}
}

// Changed, ignored or truncated later ranges must not replace an existing destination.
func TestSaveSongRejectsBrokenRanges(t *testing.T) {
	for _, mode := range []string{"wrong-offset", "changed-size", "changed-etag", "full-response", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("ETag", `"one"`)
				if calls == 1 {
					w.Header().Set("Content-Range", "bytes 0-3/8")
					w.WriteHeader(206)
					w.Write([]byte("abcd"))
					return
				}
				switch mode {
				case "wrong-offset":
					w.Header().Set("Content-Range", "bytes 0-3/8")
				case "changed-size":
					w.Header().Set("Content-Range", "bytes 4-7/9")
				default:
					w.Header().Set("Content-Range", "bytes 4-7/8")
				}
				if mode == "changed-etag" {
					w.Header().Set("ETag", `"two"`)
				}
				if mode == "full-response" {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(206)
				}
				if mode == "truncated" {
					w.Write([]byte("e"))
				} else {
					w.Write([]byte("efgh"))
				}
			}))
			defer srv.Close()
			dir := t.TempDir()
			path := filepath.Join(dir, "song.m4a")
			os.WriteFile(path, []byte("original"), 0600)
			song := &Song{st: &music.Stream{URL: srv.URL}}
			if err := song.SaveTo(path); err == nil {
				t.Fatal("accepted broken ranges")
			}
			got, _ := os.ReadFile(path)
			if string(got) != "original" {
				t.Fatal("replaced original")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatal("left partial download")
			}
		})
	}
}

// Download redirects must honor the plugin's audio permission before contacting a new host.
func TestSaveSongRedirectPermission(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; w.Write([]byte("wrong")) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer origin.Close()
	song := &Song{st: &music.Stream{URL: origin.URL, AuthorizeURL: func(u *url.URL) error {
		if u.String() != origin.URL {
			return errors.New("denied")
		}
		return nil
	}}}
	if err := song.SaveTo(filepath.Join(t.TempDir(), "song.m4a")); err == nil || reached {
		t.Fatal("followed denied redirect")
	}
}
