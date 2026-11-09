//go:build darwin && arm64 && cgo

package player

/*
#cgo CFLAGS: -std=c11 -I${SRCDIR}/../../build/audio/darwin-arm64/include
#cgo LDFLAGS: ${SRCDIR}/../../build/audio/darwin-arm64/lib/libavformat.a ${SRCDIR}/../../build/audio/darwin-arm64/lib/libavcodec.a ${SRCDIR}/../../build/audio/darwin-arm64/lib/libswresample.a ${SRCDIR}/../../build/audio/darwin-arm64/lib/libavutil.a -framework CoreAudio -framework AudioToolbox -framework CoreFoundation -lpthread -lm
#include <stdlib.h>
#include "portable.h"
*/
import "C"

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unsafe"

	"mygo-music/internal/music"
)

type portablePlayer struct {
	handle  *C.mm_audio
	playing bool
}

func openPortable(st *music.Stream) (engine, error) {
	if st == nil {
		return nil, errors.New("missing audio stream")
	}
	target := st.URL
	network := C.int(0)
	if path, ok := music.PathOf(target); ok {
		target = path
	} else {
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") {
			return nil, errors.New("audio engine requires a loopback relay")
		}
		network = 1
	}
	if strings.IndexByte(target, 0) >= 0 {
		return nil, errors.New("invalid audio path")
	}
	text := C.CString(target)
	defer C.free(unsafe.Pointer(text))
	h := C.mm_audio_open(text, network)
	if h == nil {
		return nil, errors.New("could not create audio engine")
	}
	return &portablePlayer{handle: h}, nil
}
func (p *portablePlayer) Play()                { p.playing = true; C.mm_audio_play(p.handle, 1) }
func (p *portablePlayer) Pause()               { p.playing = false; C.mm_audio_play(p.handle, 0) }
func (p *portablePlayer) Playing() bool        { return p.playing }
func (p *portablePlayer) Seek(t time.Duration) { C.mm_audio_seek(p.handle, C.double(t.Seconds())) }
func (p *portablePlayer) Position() time.Duration {
	return time.Duration(float64(C.mm_audio_position(p.handle)) * float64(time.Second))
}
func (p *portablePlayer) Duration() time.Duration {
	return time.Duration(float64(C.mm_audio_duration(p.handle)) * float64(time.Second))
}
func (p *portablePlayer) SetVolume(v float64) { C.mm_audio_volume(p.handle, C.float(v)) }
func (p *portablePlayer) Close() {
	if p.handle != nil {
		C.mm_audio_close(p.handle)
		p.handle = nil
	}
	p.playing = false
}
func (p *portablePlayer) Poll() (outputState, error) {
	switch C.mm_audio_state(p.handle) {
	case -1:
		return outputLoading, errors.New("audio decoding or output failed")
	case 1:
		return outputReady, nil
	case 2:
		p.playing = false
		return outputEnded, nil
	default:
		return outputLoading, nil
	}
}
func (p *portablePlayer) consumed() uint64  { return uint64(C.mm_audio_consumed(p.handle)) }
func (p *portablePlayer) underruns() uint64 { return uint64(C.mm_audio_underruns(p.handle)) }
func (p *portablePlayer) energy() float64   { return float64(C.mm_audio_energy(p.handle)) }

func (p *portablePlayer) outputEnergy() float64 { return float64(C.mm_audio_output_energy(p.handle)) }

func (p *portablePlayer) deviceRunning() bool { return C.mm_audio_device_running(p.handle) != 0 }
