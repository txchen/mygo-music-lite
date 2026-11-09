package player

import (
	"errors"
	"fmt"
	"time"

	"mygo-music/internal/music"
)

// Transport is the control surface shared by native engines and view fixtures.
// All calls belong to the player's main thread.
type Transport interface {
	Play()
	Pause()
	Playing() bool
	Seek(time.Duration)
	Position() time.Duration
	Duration() time.Duration
	SetVolume(float64)
	Close()
}

type outputState uint8

const (
	outputLoading outputState = iota
	outputReady
	outputEnded
)

type engine interface {
	Transport
	Poll() (outputState, error)
}

// engineLoad belongs to one queue load. It allows at most one fallback and
// keeps failures tied to the queue direction that started the load.
type engineLoad struct {
	fallback         bool
	primaryErr       error
	at, skipped, dir int
	started          time.Time
}

func (p *Player) startEngine(info *music.Stream, load engineLoad) {
	// Network audio needs separate redirect and credential handling. This
	// local-only release must not forward plugin headers through native engines.
	if _, local := music.PathOf(info.URL); !local {
		p.loading = false
		p.loadFailed(p.track.Source, errors.New("MyGo Music Lite currently supports local audio files only"), load.at, load.skipped, load.dir)
		return
	}
	p.info = info
	var out engine
	var err error
	if p.openEngine != nil {
		out, err = p.openEngine(info, load.fallback)
	} else if load.fallback {
		out, err = openMPV(info)
	} else {
		out, err = openAVPlayer(info)
	}
	if err != nil {
		p.engineFailed(err, load)
		return
	}
	p.out = out
	load.started = p.now()
	p.engineLoad = load
	p.applyGain()
	p.pollEngine()
}

func (p *Player) engineFailed(err error, load engineLoad) {
	if p.out != nil {
		if !p.loading {
			p.pending = p.Position()
		}
		p.out.Close()
		p.out = nil
	}
	p.loading = true
	if !load.fallback {
		load.fallback, load.primaryErr = true, err
		p.startEngine(p.info, load)
		return
	}
	p.loading = false
	p.loadFailed(p.track.Source, fmt.Errorf("AVPlayer: %v; libmpv: %w", load.primaryErr, err), load.at, load.skipped, load.dir)
}

// pollEngine checks native state without invalidating the window on every tick.
// Once paused and ready it schedules nothing. A stale load cannot restart it.
func (p *Player) pollEngine() {
	out, ok := p.out.(engine)
	if !ok || p.polling {
		return
	}
	state, err := out.Poll()
	if err == nil && state == outputLoading && p.now().Sub(p.engineLoad.started) > 20*time.Second {
		err = errors.New("audio did not become ready within 20 seconds")
	}
	if err != nil {
		p.engineFailed(err, p.engineLoad)
		return
	}
	if state == outputReady && p.loading {
		p.loading = false
		if p.pending > 0 {
			out.Seek(p.pending)
			p.pending = 0
		}
		if p.playing {
			out.Play()
			p.record()
		}
		p.sync()
		p.post(func() {})
	}
	if state == outputEnded && !p.loading {
		if p.playing {
			p.ended()
		}
		p.post(func() {})
		if p.out != out {
			return
		}
	}
	if !p.loading && !p.playing {
		return
	}
	p.polling = true
	gen := p.gen
	p.after(250*time.Millisecond, func() {
		post := p.Quiet
		if post == nil {
			post = p.post
		}
		post(func() {
			if gen != p.gen || p.out != out {
				return
			}
			p.polling = false
			p.pollEngine()
		})
	})
}
