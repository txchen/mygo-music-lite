package diagnostics

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"strings"
)

// TraceHTTP records connection phases without addresses, headers or URLs.
func TraceHTTP(req *http.Request, id uint64) *http.Request {
	if !Enabled() {
		return req
	}
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { Record(id, "dns_start", 0) },
		DNSDone:           func(httptrace.DNSDoneInfo) { Record(id, "dns_end", 0) },
		ConnectStart:      func(string, string) { Record(id, "connect_start", 0) },
		ConnectDone:       func(_, _ string, err error) { Record(id, "connect_end", failed(err)) },
		TLSHandshakeStart: func() { Record(id, "tls_start", 0) },
		TLSHandshakeDone:  func(_ tls.ConnectionState, err error) { Record(id, "tls_end", failed(err)) },
		GotConn: func(info httptrace.GotConnInfo) {
			var reused int64
			if info.Reused {
				reused = 1
			}
			Record(id, "connection_reused", reused)
		},
		GotFirstResponseByte: func() { Record(id, "http_first_byte", 0) },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}
func failed(err error) int64 {
	if err != nil {
		return 1
	}
	return 0
}

// Transport uses the normal HTTP transport and labels only known request roles.
// Unrecognized plugin endpoints are deliberately not identified in the log.
type Transport struct{}

func (Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !Enabled() {
		return http.DefaultTransport.RoundTrip(req)
	}
	id := ID()
	stage := "plugin_http"
	switch {
	case req.URL.Hostname() == "music.youtube.com" && req.URL.Path == "/youtubei/v1/player":
		stage = "youtube_player"
	case req.URL.Hostname() == "music.youtube.com" && req.URL.Path == "/youtubei/v1/visitor_id":
		stage = "youtube_visitor"
	case strings.HasSuffix(req.URL.Hostname(), ".googlevideo.com"):
		stage = "youtube_media_probe"
	}
	Record(id, stage, 0)
	res, err := http.DefaultTransport.RoundTrip(TraceHTTP(req, id))
	if err != nil {
		Record(id, "http_error", 1)
	} else {
		Record(id, "http_headers", int64(res.StatusCode))
	}
	return res, err
}
