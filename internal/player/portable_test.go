//go:build darwin && arm64 && cgo

package player

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mygo-music/internal/music"
)

// The portable decoder outputs real PCM, preserves paused seeking, and restarts after EOF.
func TestPortableFormats(t *testing.T) {
	for _, file := range []string{"seek.mp3", "seek.flac", "seek.m4a", "seek-aac.m4a", "seek.ogg", "seek.wv", "seek.aac", "seek-aac-frag.m4a", "seek-flac-frag.m4a"} {
		t.Run(file, func(t *testing.T) {
			path, _ := filepath.Abs(filepath.Join("testdata", file))
			out, err := openPortable(&music.Stream{URL: music.FileURL(path)})
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			p := out.(*portablePlayer)
			ready := func() bool {
				s, e := out.Poll()
				if e != nil {
					t.Error(e)
					return true
				}
				return s == outputReady
			}
			nativeWait(t, ready)
			out.SetVolume(0)
			out.Play()
			nativeWait(t, func() bool { return out.Position() > 50*time.Millisecond })
			out.Pause()
			paused := out.Position()
			time.Sleep(80 * time.Millisecond)
			if out.Position()-paused > 30*time.Millisecond {
				t.Fatal("paused position advanced")
			}
			if p.outputEnergy() != 0 {
				t.Fatal("muted device emitted samples")
			}
			out.SetVolume(0.001)
			out.Seek(time.Second)
			nativeWait(t, func() bool { return math.Abs(out.Position().Seconds()-1) < .03 })
			if out.Playing() {
				t.Fatal("seek resumed paused output")
			}
			out.Play()
			nativeWait(t, func() bool { return out.Position() > 1100*time.Millisecond })
			if p.consumed() == 0 || p.energy() <= 0 || p.outputEnergy() <= 0 {
				t.Fatal("device did not consume decoded PCM")
			}
			nativeWait(t, func() bool {
				s, e := out.Poll()
				if e != nil {
					t.Error(e)
					return true
				}
				return s == outputEnded
			})
			out.Seek(0)
			out.Play()
			nativeWait(t, func() bool { return out.Position() < time.Second && out.Position() > 50*time.Millisecond })
		})
	}
}

// Closing an engine interrupts a remote open even when the relay sends no response headers.
func TestPortableCloseBlockedOpen(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	out, err := openPortable(&music.Stream{URL: server.URL + "/audio"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		out.Close()
		t.Fatal("no input request")
	}
	begin := time.Now()
	out.Close()
	elapsed := time.Since(begin)
	t.Logf("blocked open close: %v", elapsed)
	if elapsed > 500*time.Millisecond {
		t.Fatal("close did not interrupt blocked input")
	}
}

// Only local paths and loopback relay URLs may enter the native protocol stack.
func TestPortableRejectsUpstream(t *testing.T) {
	for _, target := range []string{"https://example.com/audio", "http://example.com/audio", "http://127.0.0.1/audio", "http://user@127.0.0.1:80/audio", "http://127.0.0.1:80/audio?secret=1"} {
		if out, err := openPortable(&music.Stream{URL: target}); err == nil {
			out.Close()
			t.Fatalf("accepted %q", target)
		}
	}
}

// Closing during a stalled media read cancels the worker instead of waiting for the 30-second stall budget.
func TestPortableCloseBlockedRead(t *testing.T) {
	data, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "100000000")
		w.Write(data)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	out, err := openPortable(&music.Stream{URL: server.URL + "/audio"})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	nativeWait(t, func() bool {
		s, e := out.Poll()
		if e != nil {
			t.Error(e)
			return true
		}
		return s == outputReady
	})
	out.SetVolume(0)
	out.Play()
	nativeWait(t, func() bool { return out.Position() > 2500*time.Millisecond })
	out.Pause()
	if out.(*portablePlayer).deviceRunning() {
		t.Fatal("paused stalled read left the output device running")
	}
	paused := out.Position()
	time.Sleep(40 * time.Millisecond)
	if out.Position() != paused {
		t.Fatal("paused stalled read advanced")
	}
	begin := time.Now()
	out.Close()
	elapsed := time.Since(begin)
	t.Logf("blocked read close: %v", elapsed)
	if elapsed > 500*time.Millisecond {
		t.Fatal("close waited for stalled media")
	}
}

// A seek from EOF must hide the old end while a new range is blocked, and closing cancels that seek.
func TestPortableSeekFromEOF(t *testing.T) {
	var block atomic.Bool
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		http.ServeFile(w, r, "testdata/seek.mp3")
	}))
	defer server.Close()
	out, err := openPortable(&music.Stream{URL: server.URL + "/audio"})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	nativeWait(t, func() bool {
		s, e := out.Poll()
		if e != nil {
			t.Error(e)
			return true
		}
		return s == outputReady
	})
	out.SetVolume(0)
	out.Seek(out.Duration() - 300*time.Millisecond)
	out.Play()
	nativeWait(t, func() bool {
		s, e := out.Poll()
		if e != nil {
			t.Error(e)
			return true
		}
		return s == outputEnded
	})
	block.Store(true)
	out.Seek(0)
	out.Play()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("seek did not request a new range")
	}
	for range 10 {
		s, e := out.Poll()
		if e != nil {
			t.Fatal(e)
		}
		if s == outputEnded {
			t.Fatal("in-flight seek exposed stale EOF")
		}
		time.Sleep(time.Millisecond)
	}
	begin := time.Now()
	out.Close()
	elapsed := time.Since(begin)
	t.Logf("blocked seek close: %v", elapsed)
	if elapsed > 500*time.Millisecond {
		t.Fatal("close waited for blocked seek")
	}
}
