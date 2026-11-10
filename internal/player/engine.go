package player

import (
	"errors"
	"fmt"
	"time"

	"mygo-music/internal/diagnostics"
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
	lastTrace        time.Time
	heard            bool
}

func (p *Player) startEngine(info *music.Stream, load engineLoad) {
	if !load.fallback {
		p.info = info
		p.engineInfo = info
		if _, local := music.PathOf(info.URL); !local {
			relay := p.relay
			var err error
			if relay == nil {
				relay, err = newAudioRelay(info)
			}
			if err != nil {
				p.loading = false
				p.loadFailed(p.track.Source, err, load.at, load.skipped, load.dir)
				return
			}
			p.relay = relay
			native := *info
			native.URL, native.Headers, native.AuthorizeURL = relay.url, nil, nil
			p.engineInfo = &native
		}
	}
	info = p.engineInfo
	if load.fallback {
		diagnostics.Record(p.traceID, "engine_mpv", 0)
	} else {
		diagnostics.Record(p.traceID, "engine_ffmpeg", 0)
	}

	var out engine
	var err error
	if p.openEngine != nil {
		out, err = p.openEngine(info, load.fallback)
	} else if load.fallback {
		out, err = openMPV(info)
	} else {
		out, err = openPortable(info)
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
	diagnostics.Record(p.traceID, "engine_error", p.Position().Milliseconds())
	if p.out != nil {
		if !p.loading {
			p.pending = p.Position()
		}
		p.out.Close()
		p.out = nil
	}
	if p.relay != nil && p.relay.Err() != nil {
		err = p.relay.Err()
		p.relay.Close()
		p.relay = nil
		p.loading = false
		p.loadFailed(p.track.Source, err, load.at, load.skipped, load.dir)
		return
	}
	p.loading = true
	if !load.fallback {
		load.fallback, load.primaryErr = true, err
		p.startEngine(p.info, load)
		return
	}
	if p.relay != nil {
		if failure := p.relay.Err(); failure != nil {
			err = failure
		}
		p.relay.Close()
		p.relay = nil
	}
	p.loading = false
	p.loadFailed(p.track.Source, fmt.Errorf("FFmpeg/miniaudio: %v; libmpv: %w", load.primaryErr, err), load.at, load.skipped, load.dir)
}

// pollEngine checks native state without invalidating the window on every tick.
// Once paused and ready it schedules nothing. A stale load cannot restart it.
func (p *Player) pollEngine() {
	out, ok := p.out.(engine)
	if !ok || p.polling {
		return
	}
	state, err := out.Poll()
	if diagnostics.Enabled() && !p.engineLoad.heard {
		if native, ok := out.(interface{ consumed() uint64 }); ok && native.consumed() > 0 {
			p.engineLoad.heard = true
			diagnostics.Record(p.traceID, "first_output_observed", int64(native.consumed()))
		}
	}
	if diagnostics.Enabled() && time.Since(p.engineLoad.lastTrace) >= time.Second {
		p.engineLoad.lastTrace = time.Now()
		diagnostics.Record(p.traceID, "position_ms", out.Position().Milliseconds())
		if native, ok := out.(interface {
			consumed() uint64
			underruns() uint64
		}); ok {
			diagnostics.Record(p.traceID, "consumed_frames", int64(native.consumed()))
			diagnostics.Record(p.traceID, "underruns", int64(native.underruns()))
		}
	}
	if p.relay != nil && p.relay.Err() != nil {
		err = p.relay.Err()
	}
	if err == nil && state == outputLoading && p.now().Sub(p.engineLoad.started) > 20*time.Second {
		err = errors.New("audio did not become ready within 20 seconds")
	}
	if err != nil {
		p.engineFailed(err, p.engineLoad)
		return
	}
	if state == outputReady && p.loading {
		diagnostics.Record(p.traceID, "engine_ready", 0)
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
