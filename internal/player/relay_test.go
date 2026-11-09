package player

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mygo-music/internal/music"
)

func testRelay(t *testing.T, st *music.Stream) *audioRelay {
	t.Helper()
	r, err := newAudioRelay(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

// Native range and HEAD requests retain audio response metadata without source header overrides.
func TestAudioRelayRangeAndHead(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3, 4}, 1024)
	copy(data, "ID3")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("missing credential or identity encoding")
		}
		w.Header().Set("ETag", `"track"`)
		http.ServeContent(w, r, "track.mp3", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL + "/track.mp3", Headers: map[string]string{"Authorization": "Bearer secret", "Range": "bytes=0-0", "Host": "evil", "Accept-Encoding": "gzip"}})
	for _, method := range []string{"GET", "HEAD"} {
		req, _ := http.NewRequest(method, relay.url, nil)
		req.Header.Set("Range", "bytes=1024-2047")
		req.Header.Set("If-Range", `"track"`)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 206 || res.Header.Get("Content-Range") != "bytes 1024-2047/4096" || res.ContentLength != 1024 {
			t.Fatalf("bad range response %s %v", res.Status, res.Header)
		}
		if method == "GET" && !bytes.Equal(body, data[1024:2048]) || method == "HEAD" && len(body) != 0 {
			t.Fatal("incorrect range body")
		}
	}
	req, _ := http.NewRequest("GET", relay.url, nil)
	req.Header.Set("Range", "bytes=9000-")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 416 || res.Header.Get("Content-Range") != "bytes */4096" {
		t.Fatal("unsatisfied range was lost")
	}
}

// Redirects preserve same-origin credentials but never restore them after leaving the origin.
func TestAudioRelayRedirectCredentials(t *testing.T) {
	var first *httptest.Server
	var seen atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Secret") != "" || r.Header.Get("Referer") != "" {
			t.Error("redirect leaked credentials or signed referrer")
		}
		http.Redirect(w, r, first.URL+"/finish", http.StatusFound)
	}))
	defer second.Close()
	first = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			if r.Header.Get("Authorization") != "secret" {
				t.Error("initial header missing")
			}
			http.Redirect(w, r, "/same", http.StatusFound)
		case "/same":
			if r.Header.Get("Authorization") != "secret" {
				t.Error("same-origin header missing")
			}
			http.Redirect(w, r, second.URL, http.StatusFound)
		default:
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Secret") != "" {
				t.Error("credentials restored on return")
			}
			seen.Add(1)
			w.Write(bytes.Repeat([]byte{0xff}, 512))
		}
	}))
	defer first.Close()
	relay := testRelay(t, &music.Stream{URL: first.URL + "/start?signed=secret", Headers: map[string]string{"Authorization": "secret", "Cookie": "secret", "X-Secret": "secret"}})
	res, err := http.Get(relay.url)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || seen.Load() != 2 {
		t.Fatal("redirect chain failed")
	}
}

// Every redirect is permission checked and HTTPS cannot redirect to cleartext audio.
func TestAudioRelayRedirectPolicy(t *testing.T) {
	var reached atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer destination.Close()
	for _, tls := range []bool{false, true} {
		redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) })
		var origin *httptest.Server
		if tls {
			origin = httptest.NewTLSServer(redirect)
		} else {
			origin = httptest.NewServer(redirect)
		}
		st := &music.Stream{URL: origin.URL + "/audio"}
		if !tls {
			st.AuthorizeURL = func(u *url.URL) error {
				if u.Host == strings.TrimPrefix(origin.URL, "http://") {
					return nil
				}
				return errors.New("denied secret URL")
			}
		}
		relay := testRelay(t, st)
		if tls {
			relay.transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig
		}
		res, err := http.Get(relay.url)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 502 || relay.Err() == nil || strings.Contains(string(body), "secret") || strings.Contains(string(body), destination.URL) {
			t.Fatal("policy denial was not redacted")
		}
		relay.Close()
		origin.Close()
	}
	if reached.Load() != 0 {
		t.Fatal("forbidden destination was contacted")
	}
}

// Manifest aliases, malformed URLs and injected headers never cross the audio boundary.
func TestAudioRelayRejectsUnsupportedStreams(t *testing.T) {
	for _, st := range []music.Stream{
		{URL: "https://user:pass@example.com/a.mp3"}, {URL: "ftp://example.com/a"}, {URL: "https://example.com/a.m3u8"}, {URL: "https://example.com/a", Type: "dash"},
		{URL: "https://example.com/a", Headers: map[string]string{"Cookie": "a\r\nb"}}, {URL: "https://example.com/a", Headers: map[string]string{"bad header": "a"}},
		{URL: "https://example.com/a", AuthorizeURL: func(*url.URL) error { return errors.New("denied") }},
	} {
		r, err := newAudioRelay(&st)
		if err == nil {
			r.Close()
			t.Errorf("accepted invalid stream %q", st.URL)
		}
	}
	for _, c := range []struct{ ct, body string }{{"application/vnd.apple.mpegurl", "binary"}, {"audio/mpeg", "#EXTM3U\nhttps://secret/a"}, {"application/octet-stream", "<?xml version=\"1.0\"?><MPD>"}, {"text/html", "<html>"}, {"audio/mpeg", strings.Repeat(" ", 1024) + "#EXTM3U\nhttps://secret/a"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", c.ct)
			io.WriteString(w, c.body)
		}))
		relay := testRelay(t, &music.Stream{URL: server.URL + "/misleading.mp3"})
		req, _ := http.NewRequest("GET", relay.url, nil)
		req.Header.Set("Range", "bytes=900-")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 415 || relay.Err() == nil {
			t.Fatal("manifest reached native consumer")
		}
		relay.Close()
		server.Close()
	}
}

// Closing a track or disconnecting its consumer cancels upstream transfer without an eager download.
func TestAudioRelayCancellation(t *testing.T) {
	for _, closeRelay := range []bool{true, false} {
		started, cancelled := make(chan struct{}), make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "audio/mpeg")
			if r.Header.Get("Range") == "bytes=0-511" {
				w.Write(bytes.Repeat([]byte{0xff}, 512))
				return
			}
			w.Write(bytes.Repeat([]byte{0xff}, 1024))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			close(cancelled)
		}))
		relay := testRelay(t, &music.Stream{URL: server.URL + "/audio"})
		res, err := http.Get(relay.url)
		if err != nil {
			t.Fatal(err)
		}
		<-started
		if closeRelay {
			relay.Close()
		} else {
			res.Body.Close()
		}
		select {
		case <-cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("upstream transfer survived cancellation")
		}
		res.Body.Close()
		relay.Close()
		server.Close()
	}
}

// Only the exact opaque loopback route and read methods can trigger an upstream request.
func TestAudioRelayRoute(t *testing.T) {
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL})
	for _, c := range []struct{ method, suffix, host string }{{"GET", "?x=1", ""}, {"GET", "?", ""}, {"GET", "/extra", ""}, {"POST", "", ""}, {"GET", "", "localhost"}} {
		req, _ := http.NewRequest(c.method, relay.url+c.suffix, nil)
		if c.host != "" {
			req.Host = c.host
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatal("accepted non-opaque route")
		}
	}
	if reached.Load() != 0 {
		t.Fatal("invalid route contacted upstream")
	}
}

// A native seek can abandon one request without poisoning the next range request.
func TestAudioRelayDisconnectThenRange(t *testing.T) {
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		if r.Header.Get("Range") == "bytes=0-511" {
			w.Write(bytes.Repeat([]byte{0xff}, 512))
			return
		}
		if r.Header.Get("Range") == "bytes=1000-" {
			w.Write(bytes.Repeat([]byte{0xff}, 512))
			return
		}
		w.Write(bytes.Repeat([]byte{0xff}, 1024))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL})
	res, err := http.Get(relay.url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("abandoned request not cancelled")
	}
	req, _ := http.NewRequest("GET", relay.url, nil)
	req.Header.Set("Range", "bytes=1000-")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || len(body) != 512 || relay.Err() != nil {
		t.Fatalf("seek poisoned stream: body=%d err=%v relay=%v", len(body), err, relay.Err())
	}
}

// A valid probe cannot let a later full response disguise a playlist as audio.
func TestAudioRelayRejectsChangedManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		if r.Header.Get("Range") == "bytes=0-511" {
			w.Write(bytes.Repeat([]byte{0xff}, 512))
			return
		}
		io.WriteString(w, "#EXTM3U\nhttps://outside-permission.invalid/segment")
	}))
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL + "/track.mp3"})
	res, err := http.Get(relay.url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 415 || relay.Err() == nil {
		t.Fatal("changed manifest was forwarded")
	}
}

// A server's HTML range-error body must not turn a normal 416 into a failed track.
func TestAudioRelayHTMLRangeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-511" {
			w.Write(bytes.Repeat([]byte{0xff}, 512))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Range", "bytes */512")
		w.WriteHeader(416)
		io.WriteString(w, "<html>out of range</html>")
	}))
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL})
	req, _ := http.NewRequest("GET", relay.url, nil)
	req.Header.Set("Range", "bytes=9999-")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 416 || len(body) != 0 || res.Header.Get("Content-Range") != "bytes */512" || relay.Err() != nil {
		t.Fatal("normal range error poisoned audio")
	}
}

// A truncated upstream body must fail downstream rather than masquerading as a clean song ending.
func TestAudioRelayTruncatedTransfer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-511" {
			w.Write(bytes.Repeat([]byte{0xff}, 512))
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", "4096")
		w.Write(bytes.Repeat([]byte{0xff}, 1024))
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL})
	res, err := http.Get(relay.url)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil || relay.Err() == nil {
		t.Fatal("truncated transfer appeared to end cleanly")
	}
}
