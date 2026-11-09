package player

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mygo-music/internal/music"
)

func indexedServer(t *testing.T, all []byte, hook func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil && hook(w, r) {
			return
		}
		w.Header().Set("Content-Type", "audio/mp4")
		w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 00:00:00 GMT")
		http.ServeContent(w, r, "audio.mp4", time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), bytes.NewReader(all))
	}))
}
func preparedRelay(t *testing.T, url string) *audioRelay {
	t.Helper()
	r := testRelay(t, &music.Stream{URL: url})
	if e := r.prepare(context.Background()); e != nil {
		t.Fatal(e)
	}
	return r
}
func getRelay(t *testing.T, method, url, span string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	if span != "" {
		req.Header.Set("Range", span)
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	return res, b
}

// Internal manifests expose only fixed token routes, preserve sample bytes and answer local subranges.
func TestInternalHLSRoutes(t *testing.T) {
	_, fragment, all := indexedFixture(0, 11)
	var requests atomic.Int32
	server := indexedServer(t, all, func(w http.ResponseWriter, r *http.Request) bool { requests.Add(1); return false })
	defer server.Close()
	relay := preparedRelay(t, server.URL)
	if relay.hls == nil {
		t.Fatal("indexed audio stayed direct")
	}
	root := strings.TrimSuffix(relay.url, "/index.m3u8")
	_, manifest := getRelay(t, "GET", relay.url, "")
	if bytes.Contains(manifest, []byte(server.URL)) || !bytes.Contains(manifest, []byte("seg/1.m4s")) {
		t.Fatal("invalid generated route")
	}
	before := requests.Load()
	head, _ := getRelay(t, "HEAD", root+"/seg/0.m4s", "bytes=2-6")
	if requests.Load() != before {
		t.Fatal("HEAD fetched media")
	}
	got, b := getRelay(t, "GET", root+"/seg/0.m4s", "bytes=2-6")
	if got.StatusCode != 206 || head.StatusCode != got.StatusCode || head.ContentLength != got.ContentLength || head.Header.Get("Content-Range") != got.Header.Get("Content-Range") || !bytes.Equal(b, fragment[2:7]) {
		t.Fatalf("range HEAD=%d GET=%d data=%x", head.StatusCode, got.StatusCode, b)
	}
	for _, suffix := range []string{"/other", "/seg/00.m4s", "/seg/-1.m4s", "/seg/2.m4s", "/seg/0.m4s?url=else", "/seg/%30.m4s"} {
		res, _ := getRelay(t, "GET", root+suffix, "")
		if res.StatusCode != 404 {
			t.Fatalf("accepted route %s", suffix)
		}
	}
}

// Servers without a stable validator or byte ranges, and ordinary audio, keep direct playback.
func TestInternalHLSDirectFallback(t *testing.T) {
	_, _, all := indexedFixture(0, 0)
	for _, kind := range []string{"no validator", "ignored range", "ordinary audio", "no index"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			body := all
			if kind == "ordinary audio" {
				body = append([]byte("ID3"), make([]byte, 600)...)
			}
			if kind == "no index" {
				body = testBox("ftyp", []byte("iso6"), mp4Words(0), []byte("iso6"))
				body = append(body, testBox("mdat", make([]byte, 800))...)
			}
			server := indexedServer(t, body, func(w http.ResponseWriter, r *http.Request) bool {
				requests.Add(1)
				if kind == "ignored range" || kind == "no validator" {
					w.Header().Set("Content-Type", "audio/mp4")
					if kind == "ignored range" {
						w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 00:00:00 GMT")
					}
					w.Write(body)
					return true
				}
				return false
			})
			defer server.Close()
			relay := preparedRelay(t, server.URL)
			if relay.hls != nil || strings.HasSuffix(relay.url, ".m3u8") {
				t.Fatal("unsupported file adapted")
			}
			if (kind == "ordinary audio" || kind == "no validator") && requests.Load() != 1 {
				t.Fatal("unnecessary discovery of ordinary audio")
			}
		})
	}
}

// Exact range and validator checks reject changed, short and incorrectly addressed upstream bytes.
func TestInternalHLSRepresentationChecks(t *testing.T) {
	_, _, all := indexedFixture(0, 0)
	for _, kind := range []string{"total", "validator", "start", "short", "ignored", "overlong"} {
		t.Run(kind, func(t *testing.T) {
			var corrupt atomic.Bool
			server := indexedServer(t, all, func(w http.ResponseWriter, r *http.Request) bool {
				if !corrupt.Load() {
					return false
				}
				start, end := 0, 0
				fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
				total := len(all)
				if kind == "total" {
					total++
				}
				if kind == "start" {
					start++
				}
				w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 00:00:00 GMT")
				if kind == "validator" {
					w.Header().Set("Last-Modified", "Tue, 10 Nov 2026 00:00:00 GMT")
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
				if kind == "ignored" {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(206)
				}
				body := append([]byte(nil), all[start:end+1]...)
				if kind == "short" {
					body = body[:len(body)-1]
				}
				if kind == "overlong" {
					body = append(body, 0)
				}
				w.Write(body)
				return true
			})
			defer server.Close()
			relay := preparedRelay(t, server.URL)
			corrupt.Store(true)
			res, _ := getRelay(t, "GET", strings.TrimSuffix(relay.url, "index.m3u8")+"seg/1.m4s", "")
			if res.StatusCode != 502 || relay.Err() == nil {
				t.Fatal("representation change accepted")
			}
		})
	}
}

// A malformed later fragment terminates a published asset rather than switching representations.
func TestInternalHLSLaterFragmentValidation(t *testing.T) {
	metadata, fragment, all := indexedFixture(0, 0)
	all[len(metadata)+len(fragment)+bytes.Index(fragment, []byte("tfhd"))+11] = 2
	server := indexedServer(t, all, nil)
	defer server.Close()
	relay := preparedRelay(t, server.URL)
	res, _ := getRelay(t, "GET", strings.TrimSuffix(relay.url, "index.m3u8")+"seg/1.m4s", "")
	if res.StatusCode != 502 || relay.Err() == nil || relay.hls == nil {
		t.Fatal("later incompatible fragment was not terminal")
	}
}

// Cancellation interrupts metadata discovery and releases the upstream connection.
func TestInternalHLSCancelPreparation(t *testing.T) {
	_, _, all := indexedFixture(0, 0)
	started, cancelled := make(chan struct{}), make(chan struct{})
	server := indexedServer(t, all, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Range") == "bytes=0-511" {
			return false
		}
		close(started)
		<-r.Context().Done()
		close(cancelled)
		return true
	})
	defer server.Close()
	relay := testRelay(t, &music.Stream{URL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.prepare(ctx) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("preparation ignored cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream connection remained active")
	}
	if relay.Err() != nil || relay.hls != nil {
		t.Fatal("cancelled preparation published or poisoned relay")
	}
}
