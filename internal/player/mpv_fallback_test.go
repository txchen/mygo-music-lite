//go:build darwin && arm64

package player

import (
	"errors"
	"github.com/egoist/mygo"
	"mygo-music/internal/music"
	"path/filepath"
	"testing"
	"time"
)

// A failed primary output falls back once to libmpv and plays the track.
func TestNativeFallback(t *testing.T) {
	if loadMPV() != nil {
		t.Skip("optional Homebrew libmpv not installed")
	}
	path, _ := filepath.Abs("testdata/seek.wv")
	asked := 0
	src := streamer{st: music.Stream{URL: music.FileURL(path)}, asked: &asked}
	var p *Player
	mygo.RunOnMain(func() {
		p = New(func(fn func()) { mygo.RunOnMain(fn) })
		p.openEngine = func(st *music.Stream, fallback bool) (engine, error) {
			if !fallback {
				return nil, errors.New("primary output unavailable")
			}
			return openMPV(st)
		}
		p.Quiet = p.post
		p.Source = func(string) music.Source { return src }
		p.SetVolume(0)
		p.PlayQueue([]music.Track{{ID: "a", Source: "src"}}, 0)
	})
	defer mygo.RunOnMain(p.Close)
	nativeWait(t, func() bool { return p.Ready() && p.Position() > 50*time.Millisecond || p.Err() != "" })
	mygo.RunOnMain(func() {
		if _, ok := p.out.(*mpvPlayer); !ok || p.Err() != "" {
			t.Errorf("engine=%T err=%s", p.out, p.Err())
		}
	})
}

// Built-in Lua clients must stay absent because their LuaJIT pages crash the hardened app.
func TestMPVDoesNotLoadBuiltinScripts(t *testing.T) {
	if loadMPV() != nil {
		t.Skip("optional Homebrew libmpv not installed")
	}
	path, err := filepath.Abs(filepath.Join("testdata", "seek.ogg"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := openMPV(&music.Stream{URL: music.FileURL(path)})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	p := out.(*mpvPlayer)
	for _, name := range []string{"osc", "ytdl_hook", "stats", "console", "auto_profiles", "select", "positioning", "commands", "context_menu"} {
		if err := p.cmd("script-message-to", name, "mygo-test-probe"); err == nil {
			t.Errorf("built-in script client %q is running", name)
		}
	}
}
