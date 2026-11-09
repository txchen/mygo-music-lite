package player

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mygo-music/internal/music"
)

const relayIdleTimeout = 30 * time.Second

var errDirectAudio = errors.New("network playback supports direct audio files; HLS, DASH and playlists are unsupported")

// audioRelay owns one track's requests. The native engine only sees its opaque
// loopback route, so redirects and credentials stay inside the Go HTTP boundary.
type audioRelay struct {
	url          string
	target       *url.URL
	headers      http.Header
	authorize    func(*url.URL) error
	ctx          context.Context
	cancel       context.CancelFunc
	server       *http.Server
	transport    *http.Transport
	client       *http.Client
	mu           sync.Mutex
	err          error
	validationMu sync.Mutex
	validated    bool
	validator    string
}

func audioURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return nil, errors.New("invalid network audio URL")
	}
	if playlistType(path.Ext(u.Path)) {
		return nil, errDirectAudio
	}
	return u, nil
}

func playlistType(v string) bool {
	v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), ".")
	switch v {
	case "m3u", "m3u8", "mpd", "hls", "dash", "pls", "xspf", "asx", "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl", "application/dash+xml", "audio/x-scpls", "application/xspf+xml":
		return true
	}
	return false
}

func newAudioRelay(st *music.Stream) (*audioRelay, error) {
	target, err := audioURL(st.URL)
	if err != nil {
		return nil, err
	}
	if playlistType(st.Type) {
		return nil, errDirectAudio
	}
	if st.AuthorizeURL != nil {
		if err := st.AuthorizeURL(target); err != nil {
			return nil, errors.New("network audio URL is outside the plugin's network permission")
		}
	}
	headers := make(http.Header)
	for k, v := range st.Headers {
		if !validHeader(k, v) {
			return nil, errors.New("invalid network audio header")
		}
		switch strings.ToLower(k) {
		case "host", "connection", "proxy-connection", "proxy-authorization", "content-length", "transfer-encoding", "range", "if-range", "accept-encoding", "trailer", "upgrade", "te":
			continue
		}
		headers.Set(k, v)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("could not open the network audio relay")
	}
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		listener.Close()
		return nil, errors.New("could not create the network audio relay")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &audioRelay{target: target, headers: headers, authorize: st.AuthorizeURL, ctx: ctx, cancel: cancel}
	r.url = "http://" + listener.Addr().String() + "/" + hex.EncodeToString(token)
	r.transport = &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout: 30 * time.Second, DisableCompression: true, MaxIdleConnsPerHost: 2,
	}
	r.client = &http.Client{Transport: r.transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many audio redirects")
		}
		if _, err := audioURL(req.URL.String()); err != nil {
			return err
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("insecure audio redirect")
		}
		if r.authorize != nil {
			if err := r.authorize(req.URL); err != nil {
				return errors.New("audio redirect is outside the plugin's network permission")
			}
		}
		// net/http rebuilds redirect headers from the initial request. Once the
		// origin changes, none of the source's headers may be restored on a return.
		for _, previous := range via {
			if !sameOrigin(previous.URL, req.URL) {
				for k := range r.headers {
					req.Header.Del(k)
				}
				req.Header.Del("Referer")
				break
			}
		}
		return nil
	}}
	r.server = &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	go r.server.Serve(listener)
	return r, nil
}

func validHeader(k, v string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))) {
			return false
		}
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 32 && v[i] != '\t' || v[i] == 127 {
			return false
		}
	}
	return true
}

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return a.Scheme == b.Scheme && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func (r *audioRelay) Close() {
	r.cancel()
	r.server.Close()
	r.transport.CloseIdleConnections()
}

func (r *audioRelay) failure(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

func (r *audioRelay) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *audioRelay) ServeHTTP(w http.ResponseWriter, in *http.Request) {
	local, _ := url.Parse(r.url)
	if in.Host != local.Host || in.URL.EscapedPath() != local.EscapedPath() || in.URL.RawQuery != "" || in.URL.ForceQuery || (in.Method != "GET" && in.Method != "HEAD") {
		http.NotFound(w, in)
		return
	}
	ctx, cancel := context.WithCancel(in.Context())
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, in.Method, r.target.String(), nil)
	if err != nil {
		http.Error(w, "invalid audio request", http.StatusBadGateway)
		return
	}
	req.Header = r.headers.Clone()
	req.Header.Set("Accept-Encoding", "identity")
	for _, k := range []string{"Range", "If-Range"} {
		if v := in.Header.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}
	if err := r.validate(ctx); err != nil {
		if in.Context().Err() == nil && r.ctx.Err() == nil {
			r.failure(err)
		}
		status := http.StatusBadGateway
		if errors.Is(err, errDirectAudio) {
			status = http.StatusUnsupportedMediaType
		}
		http.Error(w, err.Error(), status)
		return
	}
	if req.Header.Get("Range") != "" && req.Header.Get("If-Range") == "" && r.validator != "" {
		req.Header.Set("If-Range", r.validator)
	}
	res, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			r.failure(errors.New("network audio request failed or was denied"))
		}
		http.Error(w, "network audio request failed or was denied", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent && res.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		r.failure(errors.New("network audio server returned an unsupported response"))
		http.Error(w, "network audio server returned an unsupported response", http.StatusBadGateway)
		return
	}
	if res.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if value := res.Header.Get("Content-Range"); value != "" {
			w.Header().Set("Content-Range", value)
		}
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if unsupportedResponse(res) {
		r.failure(errDirectAudio)
		http.Error(w, errDirectAudio.Error(), http.StatusUnsupportedMediaType)
		return
	}
	var stalled atomic.Bool
	timer := time.AfterFunc(relayIdleTimeout, func() { stalled.Store(true); cancel() })
	timer.Stop()
	defer timer.Stop()
	observed := &relayReader{body: res.Body, timer: timer}
	var body io.Reader = observed
	if in.Method == "GET" && (res.StatusCode == 200 || strings.HasPrefix(res.Header.Get("Content-Range"), "bytes 0-")) {
		prefix, e := io.ReadAll(io.LimitReader(observed, 512))
		if e != nil {
			if ctx.Err() == nil || stalled.Load() {
				r.failure(errors.New("network audio read failed or stalled"))
			}
			http.Error(w, "network audio read failed", http.StatusBadGateway)
			return
		}
		text := strings.ToLower(strings.TrimSpace(string(prefix)))
		if len(prefix) >= 12 && !directMedia(prefix) || strings.HasPrefix(text, "#extm3u") || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "[playlist]") {
			r.failure(errDirectAudio)
			http.Error(w, errDirectAudio.Error(), http.StatusUnsupportedMediaType)
			return
		}
		body = io.MultiReader(bytes.NewReader(prefix), observed)
	}
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := res.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	if in.Method == "GET" {
		// The native engine controls demand. No file cache or producer runs ahead.
		_, err = io.CopyBuffer(&relayWriter{w: w}, body, make([]byte, 32*1024))
		if err != nil {
			if stalled.Load() || observed.err != nil && observed.err != io.EOF && ctx.Err() == nil {
				r.failure(errors.New("network audio transfer failed or stalled"))
			}
			panic(http.ErrAbortHandler)
		}
	}
}

func unsupportedResponse(res *http.Response) bool {
	mediaType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	return playlistType(mediaType) || strings.Contains(mediaType, "xml") || strings.Contains(mediaType, "html") || res.Header.Get("Content-Encoding") != "" && res.Header.Get("Content-Encoding") != "identity"
}

// Validate offset zero once, independently of the byte range a native engine
// requests. A small allowlist keeps playlists out even when their MIME type lies.
func (r *audioRelay) validate(ctx context.Context) error {
	r.validationMu.Lock()
	defer r.validationMu.Unlock()
	if r.validated {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", r.target.String(), nil)
	if err != nil {
		return errors.New("invalid network audio request")
	}
	req.Header = r.headers.Clone()
	req.Header.Set("Range", "bytes=0-511")
	req.Header.Set("Accept-Encoding", "identity")
	res, err := r.client.Do(req)
	if err != nil {
		return errors.New("network audio request failed or was denied")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 206 {
		return errors.New("network audio server returned an unsupported response")
	}
	if unsupportedResponse(res) || res.StatusCode == 206 && !strings.HasPrefix(res.Header.Get("Content-Range"), "bytes 0-") {
		return errDirectAudio
	}
	timer := time.AfterFunc(relayIdleTimeout, func() { res.Body.Close() })
	defer timer.Stop()
	data, err := io.ReadAll(io.LimitReader(res.Body, 512))
	if err != nil {
		return errors.New("network audio read failed or stalled")
	}
	if !directMedia(data) {
		return errDirectAudio
	}
	r.validator = res.Header.Get("ETag")
	if strings.HasPrefix(r.validator, "W/") {
		r.validator = ""
	}
	if r.validator == "" {
		r.validator = res.Header.Get("Last-Modified")
	}
	r.validated = true
	return nil
}

func directMedia(b []byte) bool {
	for _, magic := range [][]byte{[]byte("ID3"), []byte("fLaC"), []byte("OggS"), []byte("wvpk"), []byte("MAC "), {0x1a, 0x45, 0xdf, 0xa3}, {0x30, 0x26, 0xb2, 0x75, 0x8e, 0x66, 0xcf, 0x11, 0xa6, 0xd9, 0x00, 0xaa, 0x00, 0x62, 0xce, 0x6c}} {
		if bytes.HasPrefix(b, magic) {
			return true
		}
	}
	if len(b) >= 2 && b[0] == 0xff && b[1]&0xe0 == 0xe0 {
		return true
	}
	if len(b) >= 12 {
		if string(b[:4]) == "RIFF" && string(b[8:12]) == "WAVE" {
			return true
		}
		if string(b[:4]) == "FORM" && (string(b[8:12]) == "AIFF" || string(b[8:12]) == "AIFC") {
			return true
		}
		if string(b[4:8]) == "ftyp" {
			return true
		}
	}
	return false
}

type relayReader struct {
	body  io.Reader
	timer *time.Timer
	err   error
}

func (r *relayReader) Read(p []byte) (int, error) {
	r.timer.Reset(relayIdleTimeout)
	n, err := r.body.Read(p)
	r.err = err
	r.timer.Stop()
	return n, err
}

type relayWriter struct{ w http.ResponseWriter }

func (w *relayWriter) Write(p []byte) (int, error) {
	controller := http.NewResponseController(w.w)
	controller.SetWriteDeadline(time.Now().Add(relayIdleTimeout))
	n, err := w.w.Write(p)
	if err == nil {
		err = controller.Flush()
	}
	return n, err
}
