//go:build playbackcheck && darwin && arm64

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"mygo-music/internal/music"
)

// This opt-in build uses an isolated catalog and the real app, local source,
// native engine and windows. The normal application does not include it.
func init() {
	dir, file := os.Getenv("MM_CHECK_DIR"), os.Getenv("MM_CHECK_FILE")
	if dir == "" || file == "" {
		return
	}
	for _, name := range []string{"data", "cache"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0700); err != nil {
			panic(err)
		}
	}
	mygo.App.SetPath(mygo.PathUserData, filepath.Join(dir, "data"))
	mygo.App.SetPath(mygo.PathCache, filepath.Join(dir, "cache"))
	shotHook = func(a *app) { go a.checkPlayback(dir, file) }
}

type playbackSample struct {
	Mode                            string
	Seconds, CPUPercent, Start, End float64
	Error                           string `json:",omitempty"`
}

func processCPU() float64 {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		panic(err)
	}
	return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
}

func (a *app) checkPlayback(dir, file string) {
	var samples []playbackSample
	defer func() {
		data, _ := json.MarshalIndent(samples, "", "  ")
		os.WriteFile(filepath.Join(dir, "results.json"), data, 0600)
		mygo.RunOnMain(mygo.App.Quit)
	}()
	fail := func(err error) { samples = append(samples, playbackSample{Mode: "setup", Error: err.Error()}) }
	file, err := filepath.Abs(file)
	if err != nil {
		fail(err)
		return
	}
	mygo.RunOnMain(func() { a.local.AddFolders(filepath.Dir(file)) })
	deadline := time.Now().Add(15 * time.Second)
	for a.local.Scanning() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	var track *music.Track
	for offset := 0; ; offset += 40 {
		res, err := a.local.Search("", music.SearchTracks, offset)
		if err != nil {
			fail(err)
			return
		}
		for _, t := range res.Tracks {
			st, err := a.local.Stream(t.ID, "lossless")
			if err == nil && st.URL == music.FileURL(file) {
				copy := t
				track = &copy
				break
			}
		}
		if track != nil || !res.More {
			break
		}
	}
	if track == nil {
		fail(fmt.Errorf("local scan did not find %s", file))
		return
	}
	mygo.RunOnMain(func() { a.pl.SetVolume(0); a.pl.PlayTrack([]music.Track{*track}, 0) })
	ready := false
	for time.Now().Before(deadline) {
		mygo.RunOnMain(func() { ready = a.pl.Ready() && a.pl.Position() > 100*time.Millisecond })
		if ready {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		mygo.RunOnMain(func() { fail(fmt.Errorf("playback not ready: %s", a.pl.Err())) })
		return
	}
	if assertions, err := exec.Command("pmset", "-g", "assertions").Output(); err == nil {
		os.WriteFile(filepath.Join(dir, "assertions.txt"), assertions, 0600)
	}
	modes := []string{"hidden", "hidden", "hidden", "hidden", "hidden", "visible", "now-playing", "paused"}
	if value := os.Getenv("MM_CHECK_MODES"); value != "" {
		modes = strings.Split(value, ",")
	}
	for _, mode := range modes {
		if mode == "visible" || mode == "now-playing" {
			session, err := exec.Command("ioreg", "-n", "Root", "-d1").Output()
			if err != nil || strings.Contains(string(session), "\"CGSSessionScreenIsLocked\"=Yes") {
				samples = append(samples, playbackSample{Mode: mode, Error: "visible rendering not measured: screen locked or session state unavailable"})
				continue
			}
		}
		mygo.RunOnMain(func() {
			a.nowPlaying = mode == "now-playing"
			if mode == "hidden" {
				a.win.Hide()
			} else {
				a.win.Show()
				a.win.Focus()
				a.win.Invalidate()
			}
			if mode == "paused" {
				a.pl.TogglePlay()
			}
		})
		time.Sleep(2 * time.Second)
		sample := playbackSample{Mode: mode}
		mygo.RunOnMain(func() { sample.Start = a.pl.Position().Seconds() })
		cpu, start := processCPU(), time.Now()
		time.Sleep(10 * time.Second)
		sample.Seconds = time.Since(start).Seconds()
		sample.CPUPercent = (processCPU() - cpu) / sample.Seconds * 100
		mygo.RunOnMain(func() { sample.End = a.pl.Position().Seconds(); sample.Error = a.pl.Err() })
		if mode != "paused" && sample.End-sample.Start < sample.Seconds-1 {
			sample.Error = "audio clock did not advance throughout measurement"
		}
		if mode == "paused" && sample.End-sample.Start > 0.1 {
			sample.Error = "audio clock advanced while paused"
		}
		samples = append(samples, sample)
		if mode == "visible" || mode == "now-playing" {
			if png, err := a.win.CapturePage(); err == nil {
				os.WriteFile(filepath.Join(dir, mode+".png"), png, 0600)
			}
		}
		if sample.Error != "" {
			return
		}
	}
}
