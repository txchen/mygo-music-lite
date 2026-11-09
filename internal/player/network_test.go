package player

import (
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"mygo-music/internal/music"
)

// Player streams direct HTTP audio, pauses and seeks through the relay, including fragmented MP4 and WavPack.
func TestNativeNetworkTransport(t *testing.T) {
	for _, file := range []string{"seek.mp3", "seek.wv", "seek-aac-frag.m4a", "seek-flac-frag.m4a"} {
		t.Run(file, func(t *testing.T) {

			var requests, denied atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer fixture" {
					denied.Add(1)
					http.Error(w, "denied", 403)
					return
				}
				http.ServeFile(w, r, filepath.Join("testdata", file))
			}))
			defer server.Close()
			asked := 0
			src := streamer{asked: &asked, st: music.Stream{URL: server.URL + "/" + file, Headers: map[string]string{"Authorization": "Bearer fixture"}}}
			var p *Player
			mygo.RunOnMain(func() {
				p = New(func(fn func()) { mygo.RunOnMain(fn) })
				p.Quiet = p.post
				p.Source = func(string) music.Source { return src }
				p.SetVolume(0)
				p.PlayQueue([]music.Track{{ID: "a", Source: "src"}}, 0)
			})
			defer mygo.RunOnMain(func() {
				if t.Failed() {
					t.Logf("engine=%T loading=%v pos=%v err=%s", p.out, p.Loading(), p.Position(), p.Err())
				}
				p.Close()
			})
			nativeWait(t, func() bool { return p.Ready() && p.Position() > 50*time.Millisecond })
			var paused time.Duration
			mygo.RunOnMain(func() {
				if p.Stream().URL != src.st.URL || p.relay == nil {
					t.Error("original stream or relay lost")
				}
				if _, ok := p.out.(*portablePlayer); !ok {
					t.Errorf("expected bundled backend, got %T", p.out)
				}

				p.TogglePlay()
				paused = p.Position()
			})
			time.Sleep(100 * time.Millisecond)
			mygo.RunOnMain(func() {
				if p.Position()-paused > 80*time.Millisecond {
					t.Error("network playback advanced while paused")
				}
				p.Seek(time.Second)
			})
			nativeWait(t, func() bool { return math.Abs(p.Position().Seconds()-1) < 0.15 })
			mygo.RunOnMain(func() {
				if p.Playing() {
					t.Error("seek resumed paused playback")
				}
				p.TogglePlay()
			})
			nativeWait(t, func() bool { return p.Position() > 1050*time.Millisecond })
			if requests.Load() < 2 || denied.Load() != 0 {
				t.Fatalf("native relay requests=%d denied=%d", requests.Load(), denied.Load())
			}
		})
	}
}
