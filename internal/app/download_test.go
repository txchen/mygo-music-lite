package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"mygo-music/internal/music"
)

// Download failures, including save-dialog errors previously treated as cancellation, remain visible.
func TestDownloadFailureVisible(t *testing.T) {
	for _, dialogFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "dialog"}[dialogFailure], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
			defer server.Close()
			real := saveSongAs
			defer func() { saveSongAs = real }()
			dir := t.TempDir()
			saveSongAs = func(*app, string) (string, error) {
				if dialogFailure {
					return "", errors.New("save dialog failed")
				}
				return filepath.Join(dir, "song.m4a"), nil
			}
			a := newTestApp()
			ch := posts(a)
			fake(a).stream = func(string, string) (*music.Stream, error) { return &music.Stream{URL: server.URL, Type: "m4a"}, nil }
			tr := song("1", "Song")
			tr.Source = "fake"
			a.download(tr)
			until(t, ch, func() bool {
				return len(a.downloadNotices) > 0 && a.downloadNotices[len(a.downloadNotices)-1].Type == "error"
			})
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			if _, ok := tt.Find("Couldn't download this song"); !ok {
				t.Fatal("download failed without a visible error")
			}
		})
	}
}
