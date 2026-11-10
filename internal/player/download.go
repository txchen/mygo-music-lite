package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"mygo-music/internal/music"
)

// Song is the audio to copy into a file chosen by the listener.
type Song struct {
	// Type is its file's: one of songTypes, as its source said.
	Type string
	file *os.File      // a local file
	st   *music.Stream // or else where it streams from
}

// FindSong resolves a track for a manual download. Call it off the main
// thread, then close or save the returned song.
func (p *Player) FindSong(src music.Source, t music.Track, quality string) (*Song, error) {
	if src == nil {
		return nil, errNoSource
	}
	st, err := src.Stream(t.ID, quality)
	if err != nil {
		return nil, err
	}
	if st == nil || st.URL == "" {
		return nil, music.ErrUnavailable
	}
	s := &Song{Type: songType(st.Type), st: st}
	if path, ok := music.PathOf(st.URL); ok {
		if s.file, err = os.Open(path); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// songTypes are the types of song the app knows, which name their files.
var songTypes = []string{"mp3", "flac", "ogg", "oga", "opus", "wav", "m4a", "aac", "aif", "aiff", "wma", "ape", "wv", "webm"}

// songType limits the filename extension to known audio containers.
func songType(typ string) string {
	if typ = strings.ToLower(typ); slices.Contains(songTypes, typ) {
		return typ
	}
	return "mp3"
}

// Close lets go of the song, unsaved.
func (s *Song) Close() {
	if s.file != nil {
		s.file.Close()
	}
}

// SaveTo writes the song to path, in place of what's there, and closes it. It
// comes into a file of its own beside path first, so one cut short leaves path
// as it was.
func (s *Song) SaveTo(path string) error {
	defer s.Close()
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.part")
	if err != nil {
		return err
	}
	err = s.copy(tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// copy writes all of the song to w.
func (s *Song) copy(w io.Writer) error {
	if s.file != nil {
		_, err := io.Copy(w, s.file)
		return err
	}

	target, err := audioURL(s.st.URL)
	if err != nil {
		return err
	}
	if s.st.AuthorizeURL != nil {
		if err := s.st.AuthorizeURL(target); err != nil {
			return err
		}
	}
	client := *music.HeadersStay(http.DefaultClient, target.Hostname(), s.st.Headers)
	redirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many audio redirects")
		}
		if _, err := audioURL(req.URL.String()); err != nil {
			return err
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("insecure audio redirect")
		}
		if s.st.AuthorizeURL != nil {
			if err := s.st.AuthorizeURL(req.URL); err != nil {
				return err
			}
		}
		if redirect != nil {
			return redirect(req, via)
		}
		return nil
	}
	var offset, total int64
	total = -1
	validator := ""
	for {
		n, size, tag, err := s.copyRange(w, &client, offset, total, validator)
		if err != nil {
			return err
		}
		offset += n
		if size < 0 || offset == size {
			return nil
		}
		total, validator = size, tag
	}
}

var contentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

const downloadChunk = int64(1024 * 1024)

// copyRange bounds each CDN request; a server ignoring the first Range may send
// the whole file. Later full responses must never be appended to partial data.
func (s *Song) copyRange(w io.Writer, client *http.Client, offset, total int64, validator string) (int64, int64, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(30*time.Second, cancel)
	defer timer.Stop()
	req, err := http.NewRequestWithContext(ctx, "GET", s.st.URL, nil)
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("User-Agent", music.Agent)
	for k, v := range s.st.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+downloadChunk-1))
	if validator != "" {
		req.Header.Set("If-Range", validator)
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, 0, "", errors.New("audio download connection failed or timed out")
	}
	defer res.Body.Close()
	reader := &relayReader{body: res.Body, timer: timer}
	if res.StatusCode == http.StatusOK && offset == 0 {
		n, err := io.Copy(w, reader)
		return n, -1, "", err
	}
	if res.StatusCode != http.StatusPartialContent {
		return 0, 0, "", fmt.Errorf("audio download returned HTTP %d", res.StatusCode)
	}
	match := contentRangePattern.FindStringSubmatch(res.Header.Get("Content-Range"))
	if match == nil {
		return 0, 0, "", errors.New("audio server returned an invalid byte range")
	}
	values := make([]int64, 3)
	for i := range values {
		values[i], err = strconv.ParseInt(match[i+1], 10, 64)
		if err != nil {
			return 0, 0, "", errors.New("audio server returned an invalid byte range")
		}
	}
	start, end, size := values[0], values[1], values[2]
	tag := res.Header.Get("ETag")
	if strings.HasPrefix(tag, "W/") {
		tag = ""
	}
	if tag == "" {
		tag = res.Header.Get("Last-Modified")
	}
	if start != offset || end < start || end-start >= downloadChunk || end >= size || total >= 0 && total != size || validator != "" && validator != tag {
		return 0, 0, "", errors.New("audio file changed or the server returned an unexpected byte range")
	}
	expected := end - start + 1
	n, err := io.Copy(w, io.LimitReader(reader, expected+1))
	if err == nil && n != expected {
		err = io.ErrUnexpectedEOF
	}
	return n, size, tag, err
}
