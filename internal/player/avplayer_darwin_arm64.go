//go:build darwin && arm64 && cgo

package player

/*
#cgo LDFLAGS: -framework AVFoundation -framework Foundation -framework CoreMedia
#include <stdlib.h>
void *mm_av_open(const char *url);
void mm_av_close(void *p);
void mm_av_play(void *p, int play);
void mm_av_seek(void *p, double seconds);
void mm_av_volume(void *p, float volume);
double mm_av_position(void *p);
double mm_av_duration(void *p);
int mm_av_state(void *p);
char *mm_av_error(void *p);
*/
import "C"

import (
	"errors"
	"time"
	"unsafe"

	"mygo-music/internal/music"
)

type avPlayer struct {
	handle  unsafe.Pointer
	playing bool
}

func openAVPlayer(st *music.Stream) (engine, error) {
	url := C.CString(st.URL)
	defer C.free(unsafe.Pointer(url))
	h := C.mm_av_open(url)
	if h == nil {
		return nil, errors.New("could not create audio player")
	}
	return &avPlayer{handle: h}, nil
}

func (p *avPlayer) Play()                { p.playing = true; C.mm_av_play(p.handle, 1) }
func (p *avPlayer) Pause()               { p.playing = false; C.mm_av_play(p.handle, 0) }
func (p *avPlayer) Playing() bool        { return p.playing }
func (p *avPlayer) Seek(t time.Duration) { C.mm_av_seek(p.handle, C.double(t.Seconds())) }
func (p *avPlayer) Position() time.Duration {
	return time.Duration(float64(C.mm_av_position(p.handle)) * float64(time.Second))
}
func (p *avPlayer) Duration() time.Duration {
	return time.Duration(float64(C.mm_av_duration(p.handle)) * float64(time.Second))
}
func (p *avPlayer) SetVolume(v float64) { C.mm_av_volume(p.handle, C.float(v)) }
func (p *avPlayer) Close() {
	if p.handle != nil {
		C.mm_av_close(p.handle)
		p.handle = nil
	}
	p.playing = false
}
func (p *avPlayer) Poll() (outputState, error) {
	switch C.mm_av_state(p.handle) {
	case -1:
		msg := C.mm_av_error(p.handle)
		defer C.free(unsafe.Pointer(msg))
		return outputLoading, errors.New(C.GoString(msg))
	case 1:
		return outputReady, nil
	case 2:
		p.playing = false
		return outputEnded, nil
	default:
		return outputLoading, nil
	}
}
