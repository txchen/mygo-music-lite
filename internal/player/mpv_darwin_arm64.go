//go:build darwin && arm64

package player

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"mygo-music/internal/music"
)

var mpvAPI struct {
	once        sync.Once
	err         error
	create      func() uintptr
	initialize  func(uintptr) int32
	destroy     func(uintptr)
	option      func(uintptr, string, string) int32
	property    func(uintptr, string, int32, unsafe.Pointer) int32
	setProperty func(uintptr, string, string) int32
	command     func(uintptr, []*byte) int32
	wait        func(uintptr, float64) *mpvEvent
	errorString func(int32) string
}

type mpvEvent struct {
	ID       int32
	Error    int32
	Userdata uint64
	Data     unsafe.Pointer
}

type mpvEndFile struct {
	Reason int32
	Error  int32
}

func loadMPV() error {
	mpvAPI.once.Do(func() {
		var h uintptr
		for _, path := range []string{"/opt/homebrew/opt/mpv/lib/libmpv.2.dylib", "/opt/homebrew/lib/libmpv.2.dylib"} {
			h, mpvAPI.err = purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
			if mpvAPI.err == nil {
				break
			}
		}
		if mpvAPI.err != nil {
			mpvAPI.err = errors.New("optional libmpv is unavailable; install it with brew install mpv, then restart MyGo Music Lite")
			return
		}
		for name, fn := range map[string]any{
			"mpv_create": &mpvAPI.create, "mpv_initialize": &mpvAPI.initialize,
			"mpv_terminate_destroy": &mpvAPI.destroy, "mpv_set_option_string": &mpvAPI.option,
			"mpv_get_property": &mpvAPI.property, "mpv_set_property_string": &mpvAPI.setProperty,
			"mpv_command": &mpvAPI.command, "mpv_wait_event": &mpvAPI.wait, "mpv_error_string": &mpvAPI.errorString,
		} {
			address, err := purego.Dlsym(h, name)
			if err != nil {
				mpvAPI.err = err
				return
			}
			purego.RegisterFunc(fn, address)
		}
	})
	return mpvAPI.err
}

type mpvPlayer struct {
	handle                uintptr
	ready, playing, ended bool
	url                   string
	duration              time.Duration
	err                   error
}

func openMPV(st *music.Stream) (engine, error) {
	if err := loadMPV(); err != nil {
		return nil, err
	}
	p := &mpvPlayer{handle: mpvAPI.create()}
	if p.handle == 0 {
		return nil, errors.New("mpv_create failed")
	}
	options := map[string]string{
		"config": "no", "load-scripts": "no", "terminal": "no", "vid": "no",
		"audio-display": "no", "idle": "yes", "keep-open": "no", "pause": "yes",
		"input-default-bindings": "no", "input-vo-keyboard": "no", "osc": "no",
		"replaygain": "no", "volume": "0", "access-references": "no", "autoload-files": "no",
	}
	for k, v := range options {
		if err := mpvError(mpvAPI.option(p.handle, k, v)); err != nil {
			p.Close()
			return nil, err
		}
	}
	// load-scripts only covers external scripts. Built-ins start LuaJIT, whose
	// executable pages are rejected by the signed app's hardened runtime.
	for _, name := range []string{
		"ytdl", "load-stats-overlay", "load-console", "load-osd-console", "load-auto-profiles",
		"load-select", "load-positioning", "load-commands", "load-context-menu",
	} {
		code := mpvAPI.option(p.handle, name, "no")
		if code == -5 { // MPV_ERROR_OPTION_NOT_FOUND: unavailable on this mpv version.
			continue
		}
		if err := mpvError(code); err != nil {
			p.Close()
			return nil, err
		}
	}
	if err := mpvError(mpvAPI.initialize(p.handle)); err != nil {
		p.Close()
		return nil, err
	}
	url := st.URL
	if path, ok := music.PathOf(url); ok {
		url = path
	}
	p.url = url
	if err := p.cmd("loadfile", url); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func mpvError(code int32) error {
	if code < 0 {
		return fmt.Errorf("libmpv: %s", mpvAPI.errorString(code))
	}
	return nil
}

func (p *mpvPlayer) cmd(args ...string) error {
	argv := make([]*byte, len(args)+1)
	for i, arg := range args {
		if strings.IndexByte(arg, 0) >= 0 {
			return errors.New("invalid audio command")
		}
		b := append([]byte(arg), 0)
		argv[i] = &b[0]
	}
	return mpvError(mpvAPI.command(p.handle, argv))
}
func (p *mpvPlayer) Play() {
	p.playing = true
	p.err = mpvError(mpvAPI.setProperty(p.handle, "pause", "no"))
}
func (p *mpvPlayer) Pause() {
	p.playing = false
	p.err = mpvError(mpvAPI.setProperty(p.handle, "pause", "yes"))
}
func (p *mpvPlayer) Playing() bool { return p.playing }
func (p *mpvPlayer) Seek(t time.Duration) {
	if p.ended {
		p.ended, p.ready = false, false
		p.err = p.cmd("loadfile", p.url, "replace", "-1", "start="+strconv.FormatFloat(t.Seconds(), 'f', 6, 64))
		return
	}
	p.err = p.cmd("seek", strconv.FormatFloat(t.Seconds(), 'f', 6, 64), "absolute+exact")
}
func (p *mpvPlayer) number(name string) float64 {
	var v float64
	mpvAPI.property(p.handle, name, 5, unsafe.Pointer(&v)) // MPV_FORMAT_DOUBLE
	return v
}
func (p *mpvPlayer) Position() time.Duration {
	if p.ended {
		return p.duration
	}
	return time.Duration(p.number("time-pos") * float64(time.Second))
}
func (p *mpvPlayer) Duration() time.Duration {
	if d := p.number("duration"); d > 0 {
		p.duration = time.Duration(d * float64(time.Second))
	}
	return p.duration
}
func (p *mpvPlayer) SetVolume(v float64) {
	// mpv cubes its volume percentage; Transport takes linear PCM gain.
	p.err = mpvError(mpvAPI.setProperty(p.handle, "volume", strconv.FormatFloat(math.Cbrt(v)*100, 'f', 4, 64)))
}
func (p *mpvPlayer) Close() {
	if p.handle != 0 {
		mpvAPI.destroy(p.handle)
		p.handle = 0
	}
	p.playing = false
}
func (p *mpvPlayer) Poll() (outputState, error) {
	if p.err != nil {
		return outputLoading, p.err
	}
	for {
		event := mpvAPI.wait(p.handle, 0)
		if event == nil || event.ID == 0 {
			break
		}
		switch event.ID {
		case 8: // MPV_EVENT_FILE_LOADED
			p.ready = true
			p.Duration()
			// A short file can queue EOF before this batch is drained. Deliver
			// readiness first so the owner can start it before observing its end.
			return outputReady, nil
		case 7: // MPV_EVENT_END_FILE
			end := (*mpvEndFile)(event.Data)
			if end != nil && end.Reason == 4 {
				err := mpvError(end.Error)
				if err == nil {
					err = errors.New("libmpv could not play this file")
				}
				return outputLoading, err
			}
			if end != nil && end.Reason == 0 {
				p.ended, p.playing = true, false
				return outputEnded, nil
			}
		}
	}
	if !p.ready {
		return outputLoading, nil
	}
	return outputReady, nil
}
