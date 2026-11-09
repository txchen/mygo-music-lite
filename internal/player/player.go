package player

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/egoist/mygo"

	"mygo-music/internal/i18n"
	"mygo-music/internal/lyrics"
	"mygo-music/internal/music"
)

type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// Player plays a queue of tracks, from whichever source each is of. Its
// methods run on the main thread; loading happens on goroutines, whose results
// post brings back there.
type Player struct {
	post func(func())
	// Quiet is post without a redraw, for what mostly changes nothing, if set.
	Quiet func(func())
	// Source finds the source of an ID, or nil if it's gone.
	Source func(id string) music.Source
	// Failed, if set, hears that a source said the listener has something to
	// do first: ErrSignIn or ErrSetup.
	Failed func(source string, err error)
	// Quality returns the quality to ask sources for, as the settings say.
	Quality func() string
	// Changed, if set, hears that what plays, where it is or whether it
	// plays changed.
	Changed func()

	queue   []music.Track
	order   []int // the queue's indices in play order: shuffled or not
	at      int   // where in order the current track is, -1 for none
	shuffle bool
	repeat  RepeatMode
	volume  float64

	// A radio goes on with the tracks its feed gives, as the queue nears
	// its end, for as long as nothing else is played.
	radio    string
	feed     func(got func([]music.Track))
	radioGen int  // counts radios, to drop what an old one gives
	feeding  bool // the feed is asked
	awaiting bool // at the queue's end, for what the feed gives

	track      *music.Track
	out        Transport
	info       *music.Stream // what the current track streams
	relay      *audioRelay
	loadCancel context.CancelFunc
	engineInfo *music.Stream
	openEngine func(*music.Stream, bool) (engine, error)
	engineLoad engineLoad
	polling    bool
	loading    bool
	playing    bool // what the listener wants: false while paused
	err        string
	gen        int // counts loads, to drop the results of stale ones

	lyrics       []lyrics.Line
	lyricsLoaded bool

	// pending holds a seek asked for while the track loads.
	pending time.Duration
	// remote is set while the current track is a PlaybackRemote source's,
	// which plays it elsewhere: see remote.go.
	remote *remote
	// after runs fn after d, on another goroutine; tests replace it.
	after func(d time.Duration, fn func())
	now   func() time.Time
	cmds  commands
	// The volume last asked of a remote source, and whether that's on its
	// way: guarded by cmds.mu.
	volumeAsked  int
	volumeQueued bool

	recorded string // the track last told its source it played

}

func New(post func(func())) *Player {
	p := &Player{post: post, at: -1, volume: 0.8}
	p.after = func(d time.Duration, fn func()) { time.AfterFunc(d, fn) }
	p.now = time.Now
	// The media keys, and the controls of Control Center.
	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) {
		switch cmd {
		case mygo.MediaPlay, mygo.MediaPause:
			if (cmd == mygo.MediaPlay) != p.playing {
				p.TogglePlay()
			}
		case mygo.MediaNext:
			p.Next()
		case mygo.MediaPrevious:
			p.Prev()
		case mygo.MediaSeek:
			p.Seek(pos)
		}
		p.post(func() {})
	})
	return p
}

// sync shows the system, and Changed, where playback is.
func (p *Player) sync() {
	if p.Changed != nil {
		defer p.Changed()
	}
	if p.track == nil {
		mygo.NowPlaying.Clear()
		return
	}
	state := mygo.PlaybackPaused
	if p.playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, p.Position())
}

// announce shows the system the track, and its cover once loaded.
func (p *Player) announce(t *music.Track, artwork []byte) {
	state := mygo.PlaybackPaused
	if p.playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
		Title:    t.Name,
		Artist:   t.ArtistNames(),
		Album:    t.Album.Name,
		Artwork:  artwork,
		Duration: t.Length(),
		Position: p.Position(),
		State:    state,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext | mygo.MediaPrevious | mygo.MediaSeek,
	})
}

func (p *Player) ended() {
	if p.repeat == RepeatOne {
		p.out.Seek(0)
		p.out.Play()
		return
	}
	if p.at+1 >= len(p.order) && p.feed != nil {
		p.awaitFeed()
		return
	}
	if p.at+1 >= len(p.order) && p.repeat == RepeatOff {
		p.playing = false
		p.sync()
		return
	}
	p.Next()
}

// PlayQueue replaces the queue with tracks and plays the one at start, or the
// first after it that can play.
func (p *Player) PlayQueue(tracks []music.Track, start int) {
	p.playQueue(tracks, start, forward)
}

// PlayTrack replaces the queue with tracks and plays the one at i, the
// listener's pick: one that can't play stays, saying why, rather than another
// playing in its place.
func (p *Player) PlayTrack(tracks []music.Track, i int) {
	p.playQueue(tracks, i, stay)
}

// PlayShuffled replaces the queue with tracks in a random order, any of them
// first, and plays it with shuffle on.
func (p *Player) PlayShuffled(tracks []music.Track) {
	p.shuffle = true
	p.playQueue(tracks, -1, forward)
}

// playQueue plays tracks from start; a start below 0, when shuffled, lets
// any track come first.
func (p *Player) playQueue(tracks []music.Track, start, dir int) {
	if len(tracks) == 0 {
		return
	}
	p.endRadio()
	start = min(start, len(tracks)-1)
	p.queue = append([]music.Track(nil), tracks...)
	p.order = make([]int, len(p.queue))
	for i := range p.order {
		p.order[i] = i
	}
	if p.shuffle {
		p.shuffleOrder(start)
		start = 0
	}
	p.loadSkipping(max(start, 0), 0, dir, true, 0)
}

// PlayRadio plays tracks in their order, then goes on with those feed
// gives as the queue nears its end, until something else is played. The
// radio's name says which it is. feed calls got on the main thread.
func (p *Player) PlayRadio(name string, tracks []music.Track, feed func(got func([]music.Track))) {
	if len(tracks) == 0 {
		return
	}
	shuffle := p.shuffle
	p.shuffle = false
	p.PlayQueue(tracks, 0)
	p.shuffle = shuffle
	p.AttachRadio(name, feed)
}

// AttachRadio makes the queue the radio name's, which feed goes on with,
// as after restoring a session that played it.
func (p *Player) AttachRadio(name string, feed func(got func([]music.Track))) {
	p.endRadio()
	p.radio, p.feed = name, feed
	p.refill()
}

// EndRadio stops the radio playing: the song playing plays on, but the
// songs the radio queued after it go, and it adds no more.
func (p *Player) EndRadio() {
	if p.radio == "" && p.feed == nil {
		return
	}
	awaiting := p.awaiting
	p.endRadio()
	if p.at >= 0 && p.at+1 < len(p.order) {
		kept := make([]music.Track, p.at+1)
		for i, k := range p.order[:p.at+1] {
			kept[i] = p.queue[k]
		}
		p.queue, p.order = kept, make([]int, len(kept))
		for i := range p.order {
			p.order[i] = i
		}
	}
	if awaiting {
		// At its end, waiting for more: nothing plays on.
		p.playing = false
		p.sync()
	}
}

func (p *Player) endRadio() {
	p.radioGen++
	p.radio, p.feed, p.feeding, p.awaiting = "", nil, false, false
}

// Radio returns the name of the radio playing, or "".
func (p *Player) Radio() string { return p.radio }

// refill asks the radio for more once the queue is near its end.
func (p *Player) refill() {
	if p.feed == nil || p.feeding || p.at < len(p.order)-2 {
		return
	}
	p.feeding = true
	gen := p.radioGen
	p.feed(func(more []music.Track) {
		if gen != p.radioGen {
			return
		}
		p.feeding = false
		p.appendTracks(more)
		switch {
		case !p.awaiting:
		case p.at+1 < len(p.order):
			p.awaiting = false
			p.load(p.at + 1)
		default:
			// The radio has nothing more: it stops at its end.
			p.awaiting, p.playing = false, false
			p.err = i18n.T("The station has nothing more right now")
			p.sync()
		}
	})
}

// awaitFeed waits at the end of a radio's queue for what its feed gives.
func (p *Player) awaitFeed() {
	p.awaiting = true
	p.refill()
}

// appendTracks adds tracks to the end of the queue, but those in it.
func (p *Player) appendTracks(tracks []music.Track) {
	type key struct{ source, id string }
	in := make(map[key]bool, len(p.queue))
	for _, t := range p.queue {
		in[key{t.Source, t.ID}] = true
	}
	for _, t := range tracks {
		if k := (key{t.Source, t.ID}); !in[k] {
			in[k] = true
			p.order = append(p.order, len(p.queue))
			p.queue = append(p.queue, t)
		}
	}
}

// shuffleOrder puts the order in a random one with first at its head, or
// with any at its head when first is below 0.
func (p *Player) shuffleOrder(first int) {
	rand.Shuffle(len(p.order), func(i, j int) { p.order[i], p.order[j] = p.order[j], p.order[i] })
	for i, v := range p.order {
		if v == first {
			p.order[0], p.order[i] = p.order[i], p.order[0]
		}
	}
}

func (p *Player) ToggleShuffle() {
	p.shuffle = !p.shuffle
	if len(p.order) == 0 {
		return
	}
	cur := p.order[max(p.at, 0)]
	if p.shuffle {
		p.shuffleOrder(cur)
		p.at = 0
	} else {
		for i := range p.order {
			p.order[i] = i
		}
		p.at = cur
	}
}

func (p *Player) CycleRepeat() { p.repeat = (p.repeat + 1) % 3 }

// Current returns the track playing or loading, or nil.
func (p *Player) Current() *music.Track { return p.track }

// Queue returns the tracks queued, in the order they were given.
func (p *Player) Queue() []music.Track { return p.queue }

// Playing reports what the listener wants: false while paused.
func (p *Player) Playing() bool { return p.playing }

// Loading reports whether the current track is still loading.
// Loading reports whether the current track is on its way: loading, or
// waiting for the next of its bytes, after a seek past what has come in.
func (p *Player) Loading() bool {
	return p.loading || p.awaiting
}

// Ready reports whether the current track has loaded, to play.
func (p *Player) Ready() bool {
	return p.out != nil && !p.loading || p.remote != nil && p.remote.started
}

// Err returns why the current track did not play, or "".
func (p *Player) Err() string { return p.err }

// Stream returns what the current track streams, once loaded, or nil.
func (p *Player) Stream() *music.Stream { return p.info }

func (p *Player) Shuffle() bool      { return p.shuffle }
func (p *Player) Repeat() RepeatMode { return p.repeat }

// Lyrics returns the current track's lyrics, and whether they have loaded.
func (p *Player) Lyrics() ([]lyrics.Line, bool) { return p.lyrics, p.lyricsLoaded }

// PlayOrder returns the queue's indices in play order, and where the
// current track is among them, -1 for none.
func (p *Player) PlayOrder() ([]int, int) { return p.order, p.at }

func (p *Player) Next() {
	if len(p.order) == 0 {
		return
	}
	at := p.at + 1
	if at >= len(p.order) {
		if p.feed != nil {
			p.stopForFeed()
			p.awaitFeed()
			return
		}
		at = 0
	}
	p.load(at)
}

// Prev plays the track before this one, however far this one has played:
// once a few seconds in, going back to its start only kept the listener
// pressing twice.
func (p *Player) Prev() {
	if len(p.order) == 0 {
		return
	}
	at := p.at - 1
	if at < 0 {
		at = len(p.order) - 1
	}
	// One that can't play is passed going back, as going forward.
	p.loadSkipping(at, 0, back, true, 0)
}

// Jump plays the queue's track i, the listener's pick: one that can't play
// stays, saying why.
func (p *Player) Jump(i int) {
	for k, v := range p.order {
		if v == i {
			p.loadSkipping(k, 0, stay, true, 0)
			return
		}
	}
}

// TogglePlay pauses or resumes the current engine without processing PCM.
func (p *Player) TogglePlay() {
	if p.remote != nil && p.remote.started {
		p.remoteToggle()
		return
	}
	if p.loading {
		p.playing = !p.playing
		p.sync()
		return
	}
	if p.out == nil {
		if len(p.order) > 0 {
			p.loadSkipping(max(p.at, 0), 0, stay, true, p.pending)
		}
		return
	}
	p.playing = !p.playing
	if p.playing {
		if d := p.out.Duration(); d > 0 && p.out.Position() >= d-time.Millisecond {
			p.out.Seek(0)
		}
		p.out.Play()
		p.record()
		p.pollEngine()
	} else {
		p.out.Pause()
	}
	p.sync()
}

// Seek preserves the listener's pause/play intent while the engine seeks.
func (p *Player) Seek(t time.Duration) {
	t = max(0, t)
	if d := p.Duration(); d > 0 {
		t = min(t, d)
	}
	if p.remote != nil && p.remote.started {
		p.remoteSeek(t)
		return
	}
	if p.out == nil || p.loading {
		p.pending = t
		return
	}
	p.out.Seek(t)
	p.sync()
}

func (p *Player) SetVolume(v float64) {
	p.volume = min(1, max(0, v))
	p.applyGain()
	if p.remote != nil {
		p.remoteVolume(v)
	}
}

// applyGain keeps the existing perceptual volume curve, without normalization.
func (p *Player) applyGain() {
	if p.out != nil {
		p.out.SetVolume(p.volume * p.volume)
	}
}

func (p *Player) Position() time.Duration {
	switch {
	case p.remote != nil && p.remote.started:
		return p.remotePosition()
	case p.out == nil || p.loading:
		return p.pending
	}
	return p.out.Position()
}

func (p *Player) Duration() time.Duration {
	switch {
	case p.track == nil:
		return 0
	case p.remote != nil && p.remote.duration > 0:
		return p.remote.duration
	case p.track.Duration > 0 || p.out == nil:
		return p.track.Length()
	}
	return p.out.Duration() // a source that didn't say
}

func (p *Player) stop() {
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	if p.relay != nil {
		p.relay.Close()
		p.relay = nil
	}
	p.engineInfo = nil
	if p.out != nil {
		p.out.Close()
	}
	p.out, p.info, p.remote = nil, nil, nil
	p.polling = false
}

// Close releases native audio and invalidates pending loads and timers.
func (p *Player) Close() {
	p.gen++
	p.stop()
	p.loading, p.playing = false, false
}

// Which way a load goes past a track that can't play: on to the next, back to
// the one before, or nowhere, staying on it to say why.
const (
	forward = 1
	back    = -1
	stay    = 0
)

// load plays the track at order[at], skipping those that cannot play.
func (p *Player) load(at int) {
	p.loadSkipping(at, 0, forward, true, 0)
}

// stopForFeed stops what plays, at the end of a radio's queue, while its feed
// gives more.
func (p *Player) stopForFeed() {
	if was := p.remote; was != nil && was.started && p.playing {
		p.command(was.src, was.src.RemotePause)
	}
	p.stop()
}

// sourceOf returns the source of track t, or nil if it's gone.
func (p *Player) sourceOf(t *music.Track) music.Source {
	if p.Source == nil {
		return nil
	}
	return p.Source(t.Source)
}

// record tells the source of the current track, if it wants to know, that it
// plays, once for each time it's played.
func (p *Player) record() {
	if p.track == nil || !p.playing {
		return
	}
	src, ok := p.sourceOf(p.track).(music.Recorder)
	key := p.track.Source + "\x00" + p.track.ID
	if !ok || key == p.recorded {
		return
	}
	p.recorded = key
	t := *p.track
	go src.Played(t)
}

// loadSkipping loads the track at order[at], to play or not, from from,
// skipping those that cannot play the way dir goes: skipped of them so far.
func (p *Player) loadSkipping(at, skipped, dir int, play bool, from time.Duration) {
	t := p.queue[p.order[at]]
	src := p.sourceOf(&t)
	if was := p.remote; was != nil && was.started && p.playing && (src == nil || src.Info().ID != was.src.Info().ID) {
		// Whatever plays next isn't its to play.
		p.command(was.src, was.src.RemotePause)
	}
	p.stop()
	p.gen++
	gen := p.gen
	p.at = at
	p.track = &t
	p.loading, p.playing, p.err = true, play, ""
	p.awaiting = false
	p.lyrics, p.lyricsLoaded = nil, false
	p.pending = from
	p.recorded = ""
	p.announce(&t, nil)
	p.refill()

	p.loadCover(gen, &t)
	p.loadLyrics(gen, src, t)
	if src != nil && src.Info().Playback == music.PlaybackRemote {
		p.loadRemote(gen, src, at, skipped, dir, play, from)
		return
	}
	quality := music.Qualities[len(music.Qualities)/2]
	if p.Quality != nil {
		quality = p.Quality()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.loadCancel = cancel
	go func() {
		var info *music.Stream
		var err error
		if src == nil {
			err = errNoSource
		} else {
			info, err = src.Stream(t.ID, quality)
			if err == nil && (info == nil || info.URL == "") {
				err = music.ErrUnavailable
			}
		}

		var relay *audioRelay
		if err == nil && ctx.Err() == nil {
			if _, local := music.PathOf(info.URL); !local {
				relay, err = newAudioRelay(info)
				if err == nil {
					// Closing the load cancels both validation and an unadopted relay.
					context.AfterFunc(ctx, relay.Close)
					err = relay.validate(ctx)
				}
			}
		}
		if ctx.Err() != nil || err != nil {
			if relay != nil {
				relay.Close()
				relay = nil
			}
		}
		p.post(func() {
			if gen != p.gen || ctx.Err() != nil {
				if relay != nil {
					relay.Close()
				}
				return
			}
			if err != nil {
				p.loading = false
				p.loadFailed(t.Source, err, at, skipped, dir)
				return
			}
			p.relay = relay
			p.startEngine(info, engineLoad{at: at, skipped: skipped, dir: dir})
		})
	}()
}

// loadCover shows the system the cover of t, once it has it.
func (p *Player) loadCover(gen int, t *music.Track) {
	url := t.Album.ImageURL
	if url == "" {
		return
	}
	go func() {
		// The system shows it small, as Now Playing does.
		cover, err := music.Fetch(music.SizedImage(url, 600))
		if err == nil {
			p.post(func() {
				if gen == p.gen {
					p.announce(t, cover)
				}
			})
		}
	}()
}

// loadLyrics asks the source of t for its lyrics, if it has any.
func (p *Player) loadLyrics(gen int, src music.Source, t music.Track) {
	go func() {
		var lines []lyrics.Line
		if src != nil && src.Can("lyrics") {
			if l, err := src.Lyrics(t.ID); err == nil && l != nil {
				lines = lyrics.FromSource(l)
			}
		}
		p.post(func() {
			if gen == p.gen {
				p.lyrics = lines
				p.lyricsLoaded = true
			}
		})
	}()
}

// loadFailed says why the track at order[at] can't play, and skips it the way
// dir goes, unless it stays, nothing in the queue can play, or the listener has
// to do something first, which would stop the next track too.
func (p *Player) loadFailed(source string, err error, at, skipped, dir int) {
	p.err = errorText(err)
	if errors.Is(err, music.ErrSignIn) || errors.Is(err, music.ErrSetup) {
		p.playing = false
		if p.Failed != nil {
			p.Failed(source, err)
		}
		p.sync()
		return
	}
	if dir != stay && skipped+1 < len(p.order) {
		next := (at + dir + len(p.order)) % len(p.order)
		p.loadSkipping(next, skipped+1, dir, p.playing, 0)
		return
	}
	p.playing = false
	p.sync()
}

// errorText says what went wrong with a track, in the listener's terms where
// the source's error is one the app knows.
func errorText(err error) string {
	switch {
	case errors.Is(err, errNoSource):
		return i18n.T("The plugin of this song is gone")
	case errors.Is(err, music.ErrUnavailable):
		return i18n.T("This song isn't available")
	case errors.Is(err, music.ErrSignIn):
		return i18n.T("Sign in to play this song")
	case errors.Is(err, music.ErrSetup):
		if reason := music.Reason(err); reason != "" {
			return reason
		}
		return i18n.T("The plugin needs setting up")
	}
	return err.Error()
}

var errNoSource = errors.New("player: no such source")

// Session is where playback was left, to pick up again.
type Session struct {
	Queue    []music.Track `json:"queue"`
	Order    []int         `json:"order"`
	At       int           `json:"at"`
	Shuffle  bool          `json:"shuffle"`
	Repeat   RepeatMode    `json:"repeat"`
	Position int64         `json:"position"` // milliseconds
	Radio    string        `json:"radio,omitempty"`
}

// Session returns where playback is, or false with nothing queued.
func (p *Player) Session() (Session, bool) {
	if p.track == nil || p.at < 0 {
		return Session{}, false
	}
	return Session{
		Queue:    p.queue,
		Order:    p.order,
		At:       p.at,
		Shuffle:  p.shuffle,
		Repeat:   p.repeat,
		Position: p.Position().Milliseconds(),
		Radio:    p.radio,
	}, true
}

// Restore picks up a session where it was left, paused, and loads its
// track to play at once when asked.
func (p *Player) Restore(s Session) {
	n := len(s.Queue)
	if n == 0 || len(s.Order) != n || s.At < 0 || s.At >= n {
		return
	}
	seen := make([]bool, n)
	for _, i := range s.Order {
		if i < 0 || i >= n || seen[i] {
			return
		}
		seen[i] = true
	}
	p.queue, p.order = s.Queue, s.Order
	p.shuffle, p.repeat = s.Shuffle, s.Repeat%3
	p.loadSkipping(s.At, 0, stay, false, time.Duration(s.Position)*time.Millisecond)
}
