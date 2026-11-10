//go:build darwin && arm64 && cgo

package player

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mygo-music/internal/music"
)

// An expired keep-alive connection previously broke playback at the next audio range.
func TestPortableExpiredRangeConnection(t *testing.T) {
	frames := 48000 * 9
	b := make([]byte, 44+frames*4)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 192000)
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(frames*4))
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.ServeContent(w, r, "range.wav", time.Time{}, bytes.NewReader(b))
	}))
	server.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateIdle {
			c.Close()
		}
	}
	server.Start()
	defer server.Close()
	out, err := openPortable(&music.Stream{URL: server.URL + "/range.wav"})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	out.SetVolume(0)
	out.Play()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		_, err = out.Poll()
		if err != nil {
			t.Fatalf("failed after %.2fs, %d HTTP requests: %v", out.Position().Seconds(), requests.Load(), err)
		}
		if out.Position() > 7*time.Second {
			if requests.Load() < 2 {
				t.Fatal("did not cross a range boundary")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("playback did not cross range boundary")
}
