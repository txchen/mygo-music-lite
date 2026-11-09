package player

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"mygo-music/internal/music"
)

// nothing is a source that does nothing: tests embed it and add what they need.
type nothing struct{}

func (nothing) Info() music.Info                                 { return music.Info{} }
func (nothing) Can(string) bool                                  { return false }
func (nothing) Setting(string) string                            { return "" }
func (nothing) SetSetting(string, string) error                  { return nil }
func (nothing) SignIn() (*music.Account, error)                  { return nil, music.ErrUnsupported }
func (nothing) SignOut() error                                   { return music.ErrUnsupported }
func (nothing) Account() (*music.Account, error)                 { return nil, nil }
func (nothing) Suggest(string) ([]string, error)                 { return nil, music.ErrUnsupported }
func (nothing) Playlists() ([]music.Playlist, error)             { return nil, music.ErrUnsupported }
func (nothing) Playlist(string) (*music.Playlist, error)         { return nil, music.ErrUnsupported }
func (nothing) Album(string) (*music.AlbumPage, error)           { return nil, music.ErrUnsupported }
func (nothing) Artist(string) (*music.ArtistPage, error)         { return nil, music.ErrUnsupported }
func (nothing) Liked(int) (*music.Page, error)                   { return nil, music.ErrUnsupported }
func (nothing) Recent() ([]music.Track, error)                   { return nil, music.ErrUnsupported }
func (nothing) Home() (*music.Browse, error)                     { return nil, music.ErrUnsupported }
func (nothing) Browse(string) (*music.Browse, error)             { return nil, music.ErrUnsupported }
func (nothing) IsLiked([]string) (map[string]bool, error)        { return nil, music.ErrUnsupported }
func (nothing) Like(string, bool) error                          { return music.ErrUnsupported }
func (nothing) LikeEpisode(string, bool) error                   { return music.ErrUnsupported }
func (nothing) CreatePlaylist(string) (*music.Playlist, error)   { return nil, music.ErrUnsupported }
func (nothing) RenamePlaylist(string, string) error              { return music.ErrUnsupported }
func (nothing) DeletePlaylist(string) error                      { return music.ErrUnsupported }
func (nothing) AddToPlaylist(string, []string) error             { return music.ErrUnsupported }
func (nothing) RemoveFromPlaylist(string, []string) error        { return music.ErrUnsupported }
func (nothing) MovePlaylistTrack(string, int, int) error         { return music.ErrUnsupported }
func (nothing) SavePlaylist(string, bool) error                  { return music.ErrUnsupported }
func (nothing) FollowArtist(string, bool) error                  { return music.ErrUnsupported }
func (nothing) SaveAlbum(string, bool) error                     { return music.ErrUnsupported }
func (nothing) SavedAlbums(int) (*music.AlbumList, error)        { return nil, music.ErrUnsupported }
func (nothing) Shows() ([]music.Show, error)                     { return nil, music.ErrUnsupported }
func (nothing) Show(string, int) (*music.ShowPage, error)        { return nil, music.ErrUnsupported }
func (nothing) SubscribeShow(string, bool) error                 { return music.ErrUnsupported }
func (nothing) Stations() ([]music.Station, error)               { return nil, music.ErrUnsupported }
func (nothing) StationNext(string) ([]music.Track, error)        { return nil, music.ErrUnsupported }
func (nothing) Comments(string, int) (*music.CommentPage, error) { return nil, music.ErrUnsupported }
func (nothing) LikeComment(string, string, bool) error           { return music.ErrUnsupported }
func (nothing) PostComment(string, string) (*music.Comment, error) {
	return nil, music.ErrUnsupported
}
func (nothing) Search(string, music.SearchKind, int) (*music.SearchResult, error) {
	return nil, music.ErrUnsupported
}
func (nothing) Lyrics(string) (*music.Lyrics, error)         { return nil, music.ErrUnsupported }
func (nothing) Stream(string, string) (*music.Stream, error) { return nil, music.ErrUnsupported }
func (nothing) RemotePlay([]string, int, int64) error        { return music.ErrUnsupported }
func (nothing) RemotePause() error                           { return music.ErrUnsupported }
func (nothing) RemoteResume() error                          { return music.ErrUnsupported }
func (nothing) RemoteSeek(int64) error                       { return music.ErrUnsupported }
func (nothing) RemoteVolume(int) error                       { return music.ErrUnsupported }
func (nothing) RemoteState() (*music.RemoteState, error)     { return nil, music.ErrUnsupported }
func (nothing) RemoteDevices() ([]music.Device, error)       { return nil, music.ErrUnsupported }
func (nothing) RemoteUse(string) error                       { return music.ErrUnsupported }

// remoteFake plays elsewhere: it keeps the commands it's given, and answers the
// state its test sets.
type remoteFake struct {
	nothing
	mu       sync.Mutex
	calls    []string
	state    *music.RemoteState
	stateErr error
	playErr  error
	played   [][]string
	starts   []int
	from     []int64
	asks     int
	cmd      chan string
}

func (s *remoteFake) Info() music.Info {
	return music.Info{ID: "remote", Playback: music.PlaybackRemote}
}

func (s *remoteFake) note(what string) {
	s.mu.Lock()
	s.calls = append(s.calls, what)
	s.mu.Unlock()
	s.cmd <- what
}

func (s *remoteFake) RemotePlay(ids []string, start int, position int64) error {
	s.mu.Lock()
	s.played, s.starts, s.from = append(s.played, ids), append(s.starts, start), append(s.from, position)
	err := s.playErr
	s.mu.Unlock()
	s.note("play")
	return err
}
func (s *remoteFake) RemotePause() error  { s.note("pause"); return nil }
func (s *remoteFake) RemoteResume() error { s.note("resume"); return nil }
func (s *remoteFake) RemoteSeek(p int64) error {
	s.note("seek")
	return nil
}
func (s *remoteFake) RemoteVolume(n int) error {
	s.note("volume")
	return nil
}
func (s *remoteFake) RemoteState() (*music.RemoteState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks++
	return s.state, s.stateErr
}

func (s *remoteFake) set(st *music.RemoteState, err error) {
	s.mu.Lock()
	s.state, s.stateErr = st, err
	s.mu.Unlock()
}

// remoteRig is a player with a remote source, a clock its test moves, a queue
// of what it posts, counting what would redraw, and polls that run when told.
type remoteRig struct {
	t      *testing.T
	p      *Player
	src    *remoteFake
	posted chan func()
	quiet  chan func()
	polls  []func()
	now    time.Time
	redraw int
}

func newRemoteRig(t *testing.T, n int) *remoteRig {
	r := &remoteRig{t: t, src: &remoteFake{cmd: make(chan string, 64)}, posted: make(chan func(), 64), quiet: make(chan func(), 64), now: time.Unix(1000, 0)}
	r.p = New(func(fn func()) { r.posted <- fn })
	r.p.Quiet = func(fn func()) { r.quiet <- fn }
	r.p.Source = func(id string) music.Source {
		if id == "remote" {
			return r.src
		}
		return nil
	}
	r.p.now = func() time.Time { return r.now }
	r.p.after = func(d time.Duration, fn func()) { r.polls = append(r.polls, fn) }
	queue := make([]music.Track, n)
	for i := range queue {
		queue[i] = music.Track{ID: string(rune('a' + i)), Source: "remote", Duration: 200000}
	}
	r.p.PlayQueue(queue, 0)
	r.await("play")
	r.settle()
	return r
}

// await waits for the source to get the command what.
func (r *remoteRig) await(what string) {
	r.t.Helper()
	select {
	case got := <-r.src.cmd:
		if got != what {
			r.t.Fatalf("the source was told %q, not %q", got, what)
		}
	case <-time.After(2 * time.Second):
		r.t.Fatalf("the source wasn't told %q", what)
	}
}

// settle runs what's posted from what the source was told, until it comes to
// rest: the lyrics and the answer to RemotePlay.
func (r *remoteRig) settle() {
	r.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case fn := <-r.posted:
			r.redraw++
			fn()
			if !r.p.Loading() && r.p.lyricsLoaded && len(r.posted) == 0 {
				return
			}
		case <-time.After(5 * time.Millisecond):
			if !r.p.Loading() && r.p.lyricsLoaded {
				return
			}
		}
	}
	r.t.Fatal("it never settled")
}

// poll runs the poll due, as its timer would, and takes its answer quietly;
// what it posts counts as a redraw. It reports whether one was due.
func (r *remoteRig) poll(by time.Duration) bool {
	r.t.Helper()
	r.now = r.now.Add(by)
	if len(r.polls) == 0 {
		return false
	}
	fn := r.polls[0]
	r.polls = r.polls[1:]
	fn()
	select {
	case answered := <-r.quiet:
		answered()
	case <-time.After(2 * time.Second):
		r.t.Fatal("no answer")
	}
	return true
}

// drain runs what the last answer posted.
func (r *remoteRig) drain() {
	for {
		select {
		case fn := <-r.posted:
			r.redraw++
			fn()
		default:
			return
		}
	}
}

// settled gets past the window after a command, in which answers don't count.
func (r *remoteRig) settled() { r.now = r.now.Add(remoteSettle + time.Millisecond) }

func playingState(id string, pos int64) *music.RemoteState {
	return &music.RemoteState{Playing: true, TrackID: id, Position: pos, Duration: 200000}
}

// Playing a remote source's track decodes nothing: it tells the source the queue
// and where to start.
func TestRemotePlaysThroughSource(t *testing.T) {
	r := newRemoteRig(t, 3)
	if !reflect.DeepEqual(r.src.played[0], []string{"a", "b", "c"}) || r.src.starts[0] != 0 {
		t.Errorf("told to play %v from %d", r.src.played[0], r.src.starts[0])
	}
	if r.p.out != nil {
		t.Error("it decoded something")
	}
	if !r.p.Ready() || !r.p.Playing() {
		t.Errorf("ready %v, playing %v", r.p.Ready(), r.p.Playing())
	}
	if len(r.polls) != 1 {
		t.Errorf("%d polls due", len(r.polls))
	}
}

// The position runs on by the clock between answers, and follows what the source
// says.
func TestRemotePosition(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.settled()
	r.src.set(playingState("a", 30000), nil)
	r.poll(0)
	r.drain()
	if p := r.p.Position(); p != 30*time.Second {
		t.Fatalf("position %v, the source said 30s", p)
	}
	r.now = r.now.Add(4 * time.Second)
	if p := r.p.Position(); p != 34*time.Second {
		t.Errorf("position %v, 4s after it said 30s", p)
	}
	if r.p.Duration() != 200*time.Second {
		t.Errorf("duration %v", r.p.Duration())
	}
}

// An answer that changes nothing that shows doesn't redraw: an idle view stays
// idle. One the listener would see does.
func TestRemotePollRedrawsOnlyOnChange(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.settled()
	r.src.set(playingState("a", 10000), nil)
	r.poll(0)
	r.drain()
	r.redraw = 0
	for i := 1; i <= 5; i++ {
		r.src.set(playingState("a", 10000+int64(i)*1000), nil)
		if !r.poll(time.Second) {
			t.Fatal("it stopped asking while playing")
		}
		r.drain()
	}
	if r.redraw != 0 {
		t.Errorf("%d redraws with nothing changed", r.redraw)
	}
	// The listener jumped ahead at the other end.
	r.src.set(playingState("a", 90000), nil)
	r.poll(time.Second)
	r.drain()
	if r.redraw != 1 || r.p.Position() != 90*time.Second {
		t.Errorf("%d redraws for a jump, at %v", r.redraw, r.p.Position())
	}
}

// Pausing tells the source and stops asking it anything, until playing again.
func TestRemotePauseStopsPolling(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.settled()
	r.src.set(playingState("a", 10000), nil)
	r.poll(0)
	r.drain()
	r.now = r.now.Add(2 * time.Second)
	r.p.TogglePlay()
	r.await("pause")
	at := r.p.Position()
	// The poll under way when it paused schedules no more.
	r.poll(time.Second)
	if r.poll(time.Second) {
		t.Error("it asked again while paused")
	}
	r.now = r.now.Add(time.Minute)
	if r.p.Position() != at {
		t.Errorf("the position moved while paused: %v from %v", r.p.Position(), at)
	}
	r.p.TogglePlay()
	r.await("resume")
	if len(r.polls) != 1 {
		t.Errorf("%d polls due after playing again", len(r.polls))
	}
}

// Paused at the other end, the player says so, and stops asking.
func TestRemotePausedElsewhere(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.settled()
	r.src.set(playingState("a", 10000), nil)
	r.poll(0)
	r.drain()
	r.settled()
	r.src.set(&music.RemoteState{TrackID: "a", Position: 11000, Duration: 200000}, nil)
	r.poll(0)
	r.drain()
	if r.p.Playing() {
		t.Error("still playing")
	}
	if r.p.Position() != 11*time.Second {
		t.Errorf("at %v", r.p.Position())
	}
	if len(r.polls) != 0 {
		t.Error("it goes on asking")
	}
}

// A seek tells the source and shows there at once, whatever an answer from
// before it landed says.
func TestRemoteSeek(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.settled()
	r.src.set(playingState("a", 10000), nil)
	r.poll(0)
	r.drain()
	r.p.Seek(60 * time.Second)
	r.await("seek")
	if r.p.Position() != 60*time.Second {
		t.Fatalf("at %v after the seek", r.p.Position())
	}
	r.poll(time.Millisecond)
	r.drain()
	if p := r.p.Position(); p < 60*time.Second || p > 61*time.Second {
		t.Errorf("a stale answer moved it to %v", p)
	}
}

// Changing the volume tells the source.
func TestRemoteVolume(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.p.SetVolume(0.5)
	r.await("volume")
}

// The source going on to another song of the queue by itself shows it playing,
// without telling it anything.
func TestRemoteFollowsTrackChange(t *testing.T) {
	r := newRemoteRig(t, 3)
	r.settled()
	r.src.set(playingState("a", 10000), nil)
	r.poll(0)
	r.drain()
	r.settled()
	r.src.set(playingState("b", 500), nil)
	r.poll(0)
	r.drain()
	if tr := r.p.Current(); tr == nil || tr.ID != "b" {
		t.Fatalf("current %+v", r.p.Current())
	}
	if _, at := r.p.PlayOrder(); at != 1 {
		t.Errorf("at %d of the queue", at)
	}
	if r.p.Position() != 500*time.Millisecond {
		t.Errorf("position %v", r.p.Position())
	}
	if len(r.src.played) != 1 {
		t.Errorf("told to play again: %v", r.src.played)
	}
	if len(r.polls) != 1 {
		t.Errorf("%d polls due", len(r.polls))
	}
}

// The source stopping at the end of a track goes on with the queue, by the
// listener's repeat: the next, or at the queue's end nothing.
func TestRemoteEndOfTrack(t *testing.T) {
	r := newRemoteRig(t, 2)
	r.settled()
	r.src.set(playingState("a", 198000), nil)
	r.poll(0)
	r.drain()
	r.settled()
	r.src.set(&music.RemoteState{TrackID: "a", Position: 200000, Duration: 200000}, nil)
	r.poll(0)
	r.await("play")
	r.drain()
	r.settle()
	if tr := r.p.Current(); tr.ID != "b" {
		t.Fatalf("current %+v after the end", tr)
	}
	if got := r.src.starts[1]; got != 1 {
		t.Errorf("told to start from %d", got)
	}
	r.settled()
	r.src.set(playingState("b", 199000), nil)
	r.poll(0)
	r.drain()
	r.settled()
	r.src.set(nil, nil)
	r.poll(0)
	r.drain()
	if r.p.Playing() {
		t.Error("still playing at the end of the queue")
	}
	if r.p.Position() != 0 {
		t.Errorf("at %v at the end", r.p.Position())
	}
	if len(r.polls) != 0 {
		t.Error("it goes on asking with nothing playing")
	}
}

// Repeating one, the track playing starts over as the source moves on.
func TestRemoteRepeatOne(t *testing.T) {
	r := newRemoteRig(t, 2)
	r.p.CycleRepeat()
	r.p.CycleRepeat()
	r.settled()
	r.src.set(playingState("a", 199000), nil)
	r.poll(0)
	r.drain()
	r.settled()
	r.src.set(playingState("b", 100), nil)
	r.poll(0)
	r.await("play")
	r.drain()
	r.settle()
	if tr := r.p.Current(); tr.ID != "a" {
		t.Errorf("current %+v", tr)
	}
	if r.src.starts[1] != 0 {
		t.Errorf("told to start from %d", r.src.starts[1])
	}
}

// Answers of what played before don't count at first: the source takes a moment
// to start what it was told.
func TestRemoteStartupGrace(t *testing.T) {
	r := newRemoteRig(t, 2)
	r.settled()
	r.src.set(playingState("somewhere-else", 1000), nil)
	for range remoteGrace - 1 {
		r.poll(time.Second)
	}
	r.drain()
	if !r.p.Playing() || r.p.Current().ID != "a" {
		t.Fatalf("playing %v, current %v", r.p.Playing(), r.p.Current())
	}
	r.src.set(playingState("somewhere-else", 2000), nil)
	r.poll(time.Second)
	r.drain()
	if r.p.Playing() {
		t.Error("still following with the source on something the queue doesn't have")
	}
	if r.p.Err() == "" {
		t.Error("it doesn't say why it stopped")
	}
}

// A source that keeps failing to answer stops the player, which says why, rather
// than asking forever; one failure in a while doesn't.
func TestRemoteErrors(t *testing.T) {
	r := newRemoteRig(t, 1)
	r.settled()
	r.src.set(playingState("a", 10000), nil)
	r.poll(0)
	r.drain()
	r.src.set(nil, errors.New("no network"))
	r.poll(time.Second)
	r.drain()
	if !r.p.Playing() {
		t.Fatal("one failed answer stopped it")
	}
	for range remoteFailures {
		r.poll(time.Second)
	}
	r.drain()
	if r.p.Playing() || r.p.Err() != "no network" {
		t.Errorf("playing %v, err %q", r.p.Playing(), r.p.Err())
	}
	if len(r.polls) != 0 {
		t.Error("it goes on asking")
	}
}

// A source that wants the listener signed in stops there, saying so to the app,
// rather than skipping through the queue.
func TestRemoteSignIn(t *testing.T) {
	src := &remoteFake{cmd: make(chan string, 8), playErr: music.ErrSignIn}
	posted := make(chan func(), 16)
	p := New(func(fn func()) { posted <- fn })
	p.Source = func(string) music.Source { return src }
	var failed error
	p.Failed = func(source string, err error) { failed = err }
	p.PlayQueue([]music.Track{{ID: "a", Source: "remote"}, {ID: "b", Source: "remote"}}, 0)
	<-src.cmd
	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(failed, music.ErrSignIn) && time.Now().Before(deadline) {
		select {
		case fn := <-posted:
			fn()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !errors.Is(failed, music.ErrSignIn) {
		t.Fatal("the app never heard")
	}
	if p.Playing() || p.Current().ID != "a" || len(src.played) != 1 {
		t.Errorf("playing %v, on %s, told %v", p.Playing(), p.Current().ID, src.played)
	}
}

// A session picked up on a remote track waits, telling the source nothing, until
// the listener plays it, from where it was left.
func TestRemoteRestoreWaits(t *testing.T) {
	src := &remoteFake{cmd: make(chan string, 8)}
	posted := make(chan func(), 16)
	p := New(func(fn func()) { posted <- fn })
	p.Source = func(string) music.Source { return src }
	p.after = func(time.Duration, func()) {}
	p.Restore(Session{Queue: []music.Track{{ID: "a", Source: "remote", Duration: 200000}}, Order: []int{0}, At: 0, Position: 42000})
	if p.Playing() || p.Loading() || p.Position() != 42*time.Second {
		t.Fatalf("playing %v, loading %v, at %v", p.Playing(), p.Loading(), p.Position())
	}
	if len(src.played) != 0 {
		t.Error("it told the source to play")
	}
	p.TogglePlay()
	<-src.cmd
	src.mu.Lock()
	from := src.from[0]
	src.mu.Unlock()
	if from != 42000 {
		t.Errorf("told to play from %d", from)
	}
}

// A source that can't be found says so, rather than crashing the queue.
func TestSourceGone(t *testing.T) {
	posted := make(chan func(), 16)
	p := New(func(fn func()) { posted <- fn })
	p.Source = func(string) music.Source { return nil }
	p.PlayQueue([]music.Track{{ID: "a", Source: "gone"}}, 0)
	deadline := time.Now().Add(2 * time.Second)
	for p.Err() == "" && time.Now().Before(deadline) {
		select {
		case fn := <-posted:
			fn()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if p.Err() == "" || p.Playing() {
		t.Errorf("err %q, playing %v", p.Err(), p.Playing())
	}
}
