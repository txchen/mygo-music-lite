package player

import (
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"mygo-music/internal/music"
)

func nativeWait(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ok := false
		mygo.RunOnMain(func() { ok = check() })
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native engine did not reach the expected state")
}

// Both native engines open real fixtures, advance, pause, and seek while paused.
func TestNativeTransport(t *testing.T) {
	for _, c := range []struct {
		name, file string
		open       func(*music.Stream) (engine, error)
	}{
		{"AVPlayer MP3", "seek.mp3", openAVPlayer},
		{"AVPlayer FLAC", "seek.flac", openAVPlayer},
		{"AVPlayer ALAC", "seek.m4a", openAVPlayer},
		{"AVPlayer AAC", "seek-aac.m4a", openAVPlayer},
		{"libmpv Vorbis", "seek.ogg", openMPV},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.open == nil {
				t.Fatal("missing engine")
			}
			if c.file == "seek.ogg" && loadMPV() != nil {
				t.Skip("optional Homebrew libmpv not installed")
			}
			path, _ := filepath.Abs(filepath.Join("testdata", c.file))
			var out engine
			var err error
			mygo.RunOnMain(func() { out, err = c.open(&music.Stream{URL: music.FileURL(path)}) })
			if err != nil {
				t.Fatal(err)
			}
			defer mygo.RunOnMain(out.Close)
			nativeWait(t, func() bool {
				s, e := out.Poll()
				if e != nil {
					err = e
					return true
				}
				return s == outputReady
			})
			if err != nil {
				t.Fatal(err)
			}
			mygo.RunOnMain(func() { out.SetVolume(0); out.Play() })
			nativeWait(t, func() bool { return out.Position() > 50*time.Millisecond })
			var paused time.Duration
			mygo.RunOnMain(func() { out.Pause(); paused = out.Position() })
			time.Sleep(100 * time.Millisecond)
			mygo.RunOnMain(func() {
				if d := out.Position() - paused; d > 80*time.Millisecond {
					err = errors.New("position advanced while paused")
				}
				out.Seek(time.Second)
			})
			if err != nil {
				t.Fatal(err)
			}
			nativeWait(t, func() bool { return math.Abs(out.Position().Seconds()-1) < 0.15 })
			mygo.RunOnMain(func() {
				if out.Playing() {
					err = errors.New("seek resumed paused audio")
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			mygo.RunOnMain(out.Play)
			nativeWait(t, func() bool { return out.Position() > 1050*time.Millisecond })
		})
	}
}

// Native end notifications advance a queue and RepeatOne restarts the same item.
func TestNativeEndAndRepeat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short.wav")
	frames := 48000 / 3
	b := make([]byte, 44+frames*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 96000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(frames*2))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		repeat RepeatMode
		mpv    bool
	}{{"AVPlayer queue", RepeatOff, false}, {"AVPlayer repeat", RepeatOne, false}, {"libmpv queue", RepeatOff, true}, {"libmpv repeat", RepeatOne, true}} {
		t.Run(c.name, func(t *testing.T) {
			if c.mpv && loadMPV() != nil {
				t.Skip("optional Homebrew libmpv not installed")
			}
			repeat := c.repeat
			var p *Player
			ends := 0
			asked := 0
			src := streamer{st: music.Stream{URL: music.FileURL(path)}, asked: &asked}
			mygo.RunOnMain(func() {
				p = New(func(fn func()) { mygo.RunOnMain(fn) })
				p.Quiet = p.post
				p.Source = func(string) music.Source { return src }
				p.SetVolume(0)
				p.repeat = repeat
				p.openEngine = func(st *music.Stream, _ bool) (engine, error) {
					open := openAVPlayer
					if c.mpv {
						open = openMPV
					}
					e, err := open(st)
					if err != nil {
						return nil, err
					}
					return &observedEngine{engine: e, ends: &ends}, nil
				}
				p.PlayQueue([]music.Track{{ID: "a", Source: "src"}, {ID: "b", Source: "src"}}, 0)
			})
			defer mygo.RunOnMain(func() {
				if t.Failed() {
					t.Logf("current=%s loading=%v playing=%v position=%v engine=%T err=%s", p.Current().ID, p.Loading(), p.Playing(), p.Position(), p.out, p.Err())
				}
				p.Close()
			})
			if repeat == RepeatOff {
				nativeWait(t, func() bool { return p.Current().ID == "b" && p.Ready() })
				nativeWait(t, func() bool { return !p.Playing() && !p.Loading() })
			} else {
				nativeWait(t, func() bool { return ends >= 2 })
				mygo.RunOnMain(func() {
					if p.Current().ID != "a" || !p.Playing() {
						t.Error("repeat left current track")
					}
				})
			}
		})
	}
}

type observedEngine struct {
	engine
	ends *int
}

func (e *observedEngine) Poll() (outputState, error) {
	state, err := e.engine.Poll()
	if state == outputEnded {
		*e.ends++
	}
	return state, err
}

type fakeEngine struct {
	pos             time.Duration
	volume          float64
	playing, closed bool
	state           outputState
	err             error
}

func (e *fakeEngine) Play()                      { e.playing = true }
func (e *fakeEngine) Pause()                     { e.playing = false }
func (e *fakeEngine) Playing() bool              { return e.playing }
func (e *fakeEngine) Seek(t time.Duration)       { e.pos = t }
func (e *fakeEngine) Position() time.Duration    { return e.pos }
func (e *fakeEngine) Duration() time.Duration    { return time.Minute }
func (e *fakeEngine) SetVolume(v float64)        { e.volume = v }
func (e *fakeEngine) Close()                     { e.closed = true; e.playing = false }
func (e *fakeEngine) Poll() (outputState, error) { return e.state, e.err }

// Generated seeks and pause intents survive a failed primary engine and stale polls cannot revive it.
func TestFallbackPreservesIntent(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 7))
	for range 100 {
		p := New(func(fn func()) { fn() })
		p.Cue([]music.Track{{ID: "a", Duration: 60000}})
		var due []func()
		p.after = func(_ time.Duration, fn func()) { due = append(due, fn) }
		primary := &fakeEngine{state: outputReady}
		fallback := &fakeEngine{state: outputLoading}
		calls := 0
		p.openEngine = func(_ *music.Stream, second bool) (engine, error) {
			calls++
			if second {
				return fallback, nil
			}
			return primary, nil
		}
		p.loading, p.playing = true, true
		p.SetVolume(0.4)
		p.startEngine(&music.Stream{URL: "file:///a"}, engineLoad{})
		pos := time.Duration(rng.IntN(50000)) * time.Millisecond
		p.Seek(pos)
		paused := rng.IntN(2) == 0
		if paused {
			p.TogglePlay()
		}
		primary.err = errors.New("decode failed")
		poll := due[0]
		due = nil
		poll()
		if !primary.closed || calls != 2 {
			t.Fatal("failed engine was not replaced exactly once")
		}
		// Commands received during fallback loading replace the old seek and intent.
		pos += time.Second
		p.Seek(pos)
		p.TogglePlay()
		fallback.state = outputReady
		poll = due[0]
		due = nil
		poll()
		if fallback.pos != pos || fallback.playing != paused || math.Abs(fallback.volume-0.16) > 1e-9 {
			t.Fatalf("fallback lost state: %+v", fallback)
		}
		p.Close()
		for _, fn := range due {
			fn()
		}
		if p.out != nil || p.Playing() {
			t.Fatal("stale callback revived closed playback")
		}
	}
}

// A missing optional fallback reports failure instead of retrying the same engines forever.
func TestMissingFallbackStops(t *testing.T) {
	p := New(func(fn func()) { fn() })
	p.Cue([]music.Track{{ID: "a"}})
	p.loading, p.playing = true, true
	calls := 0
	p.openEngine = func(_ *music.Stream, _ bool) (engine, error) { calls++; return nil, errors.New("not installed") }
	p.startEngine(&music.Stream{URL: "file:///a"}, engineLoad{dir: stay})
	if calls != 2 || p.Loading() || p.Playing() || p.Err() == "" {
		t.Fatalf("calls=%d loading=%v playing=%v err=%s", calls, p.Loading(), p.Playing(), p.Err())
	}
}

// A WavPack file takes the real AVPlayer failure path into libmpv.
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
