//go:build darwin && arm64

package player

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Falling back to mpv must preserve linear PCM gain instead of applying its cubic volume curve again.
func TestMPVLinearGain(t *testing.T) {
	if loadMPV() != nil {
		t.Skip("optional Homebrew libmpv not installed")
	}
	render := func(gain float64) float64 {
		t.Helper()
		file := filepath.Join(t.TempDir(), "audio.f32")
		p := &mpvPlayer{handle: mpvAPI.create()}
		if p.handle == 0 {
			t.Fatal("mpv_create")
		}
		defer p.Close()
		for k, v := range map[string]string{
			"config": "no", "load-scripts": "no", "vid": "no",
			"idle": "yes", "keep-open": "no", "replaygain": "no",
			"ao": "pcm", "ao-pcm-file": file, "ao-pcm-waveheader": "no",
			"audio-format": "float", "audio-samplerate": "48000",
		} {
			if err := mpvError(mpvAPI.option(p.handle, k, v)); err != nil {
				t.Fatal(err)
			}
		}
		// Match the app's disabled built-in scripts: LuaJIT cannot allocate
		// executable pages under the release's hardened runtime.
		for _, name := range []string{
			"ytdl", "load-stats-overlay", "load-console", "load-osd-console", "load-auto-profiles",
			"load-select", "load-positioning", "load-commands", "load-context-menu",
		} {
			code := mpvAPI.option(p.handle, name, "no")
			if code != -5 {
				if err := mpvError(code); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := mpvError(mpvAPI.initialize(p.handle)); err != nil {
			t.Fatal(err)
		}
		p.SetVolume(gain)
		if p.err != nil {
			t.Fatal(p.err)
		}
		input, err := filepath.Abs("testdata/seek.flac")
		if err != nil {
			t.Fatal(err)
		}
		if err = p.cmd("loadfile", input); err != nil {
			t.Fatal(err)
		}
		var pollErr error
		nativeWait(t, func() bool {
			s, e := p.Poll()
			pollErr = e
			return e != nil || s == outputEnded
		})
		if pollErr != nil {
			t.Fatal(pollErr)
		}
		p.Close()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 || len(data)%4 != 0 {
			t.Fatal("missing float PCM")
		}
		var energy float64
		for i := 0; i < len(data); i += 4 {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[i:])))
			energy += v * v
		}
		if energy <= 0 {
			t.Fatal("silent PCM")
		}
		return energy
	}
	full, scaled := render(1), render(.64)
	if ratio := scaled / full; math.Abs(ratio-.64*.64) > .001 {
		t.Fatalf("PCM energy ratio = %.6f; want %.6f for linear gain 0.64", ratio, .64*.64)
	}
}
