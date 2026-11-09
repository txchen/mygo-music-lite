package player

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errAudioRange = errors.New("network audio returned an inconsistent byte range")

// prepare runs in the load worker. Only its completed immutable plan is published.
func (r *audioRelay) prepare(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := r.validate(ctx); err != nil {
		return err
	}
	if r.validator == "" || !r.indexedCandidate {
		return nil
	}
	data, err := r.readAudioRange(ctx, byteSpan{0, 16 * 1024}, true)
	if errors.Is(err, errNotIndexed) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return nil
	}
	// Grow only across bounded metadata, never through media to find a later index.
	for at, count := 0, 0; count < 128; count++ {
		if len(data)-at < 16 {
			if int64(len(data)) == r.total {
				return nil
			}
			next := min(maxHLSMetadata, max(len(data)*2, at+16))
			if next <= len(data) {
				return nil
			}
			data, err = r.readAudioRange(ctx, byteSpan{0, int64(next)}, true)
			if err != nil {
				return err
			}
		}
		if len(data)-at < 8 {
			return errInvalidMP4
		}
		kind := string(data[at+4 : at+8])
		if kind != "ftyp" && kind != "moov" && kind != "sidx" {
			return nil
		}
		n := uint64(u32(data[at:]))
		header := uint64(8)
		if n == 1 {
			if len(data)-at < 16 {
				return errInvalidMP4
			}
			n = uint64(u32(data[at+8:]))<<32 | uint64(u32(data[at+12:]))
			header = 16
		}
		if n == 0 {
			return nil
		}
		if n < header || n > uint64(r.total)-uint64(at) {
			return errInvalidMP4
		}
		if n > maxHLSMetadata || uint64(at)+n > maxHLSMetadata {
			return nil
		}
		end := at + int(n)
		if end > len(data) {
			data, err = r.readAudioRange(ctx, byteSpan{0, int64(end)}, true)
			if err != nil {
				return err
			}
		}
		if kind == "sidx" {
			asset, e := parseIndexedMP4(data[:end], r.total)
			if errors.Is(e, errNotIndexed) {
				return nil
			}
			if e != nil {
				return e
			}
			first, e := r.readAudioRange(ctx, asset.segments[0].span, false)
			if e != nil {
				return e
			}
			if e = validateFragment(first, asset.defaults); errors.Is(e, errNotIndexed) {
				return nil
			} else if e != nil {
				return e
			}
			r.hls = asset
			r.url += "/index.m3u8"
			return nil
		}
		at = end
		if at == len(data) && int64(at) == r.total {
			return nil
		}
	}
	return nil
}

// readAudioRange pins both validator and total and never retains a complete song.
func (r *audioRelay) readAudioRange(ctx context.Context, span byteSpan, discovery bool) ([]byte, error) {
	if span.offset < 0 || span.length <= 0 || span.length > maxHLSSegment {
		return nil, errAudioRange
	}
	req, e := http.NewRequestWithContext(ctx, "GET", r.target.String(), nil)
	if e != nil {
		return nil, errAudioRange
	}
	req.Header = r.headers.Clone()
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", span.offset, span.offset+span.length-1))
	req.Header.Set("If-Range", r.validator)
	res, e := r.client.Do(req)
	if e != nil {
		return nil, errors.New("network audio request failed or was denied")
	}
	defer res.Body.Close()
	if unsupportedResponse(res) {
		return nil, errDirectAudio
	}
	if res.StatusCode == http.StatusOK && discovery && r.total == 0 {
		return nil, errNotIndexed
	}
	if res.StatusCode != http.StatusPartialContent {
		return nil, errAudioRange
	}
	var start, end, total int64
	value := res.Header.Get("Content-Range")
	if _, e = fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total); e != nil || value != fmt.Sprintf("bytes %d-%d/%d", start, end, total) || total <= 0 {
		return nil, errAudioRange
	}
	expected := span.length
	if discovery && span.offset == 0 && expected > total {
		expected = total
	}
	if start != span.offset || end != span.offset+expected-1 || end >= total || r.total != 0 && total != r.total {
		return nil, errAudioRange
	}
	if res.Header.Get(r.validatorHeader) != r.validator {
		return nil, errAudioRange
	}
	if res.ContentLength >= 0 && res.ContentLength != expected {
		return nil, errAudioRange
	}
	timer := time.AfterFunc(relayIdleTimeout, func() { res.Body.Close() })
	defer timer.Stop()
	data, e := io.ReadAll(io.LimitReader(res.Body, expected+1))
	if e != nil || int64(len(data)) != expected {
		return nil, errAudioRange
	}
	if r.total == 0 {
		r.total = total
	}
	return data, nil
}

func (r *audioRelay) serveHLS(w http.ResponseWriter, in *http.Request, route string) {
	var data []byte
	switch route {
	case "/index.m3u8":
		data = r.hls.manifest
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	case "/init.mp4":
		data = r.hls.init
		w.Header().Set("Content-Type", "audio/mp4")
	default:
		if !strings.HasPrefix(route, "/seg/") || !strings.HasSuffix(route, ".m4s") {
			http.NotFound(w, in)
			return
		}
		number := strings.TrimSuffix(strings.TrimPrefix(route, "/seg/"), ".m4s")
		i, e := strconv.Atoi(number)
		if e != nil || i < 0 || i >= len(r.hls.segments) || strconv.Itoa(i) != number {
			http.NotFound(w, in)
			return
		}
		segment := r.hls.segments[i]
		w.Header().Set("Content-Type", "audio/mp4")
		if in.Method == "HEAD" {
			http.ServeContent(w, in, "", time.Time{}, io.NewSectionReader(headOnlyReader{}, 0, segment.span.length))
			return
		}
		ctx, cancel := context.WithCancel(in.Context())
		stop := context.AfterFunc(r.ctx, cancel)
		defer cancel()
		defer stop()
		select {
		case r.segmentSlots <- struct{}{}:
			defer func() { <-r.segmentSlots }()
		case <-ctx.Done():
			return
		}
		data, e = r.readAudioRange(ctx, segment.span, false)
		if e == nil {
			e = validateFragment(data, r.hls.defaults)
		}
		if e != nil {
			if ctx.Err() == nil {
				r.failure(e)
			}
			http.Error(w, "network audio segment failed validation", http.StatusBadGateway)
			return
		}
	}
	http.ServeContent(w, in, "", time.Time{}, bytes.NewReader(data))
}

// ServeContent only seeks for HEAD; no media bytes or upstream request are needed.
type headOnlyReader struct{}

func (headOnlyReader) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
