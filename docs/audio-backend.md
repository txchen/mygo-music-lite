# Audio backend

`Player` owns queue order, repeat, pause intent, pending seeks and source
generations. It calls the existing `engine`/`Transport` interface on the main
thread. The default implementation is `portablePlayer`; libmpv remains an
optional fallback, attempted once per failed load.

The Go wrapper owns one opaque C handle. Its worker owns FFmpeg demuxing,
decoding and resampling. A separate device mutex serializes initialization and
start/stop; Pause can stop the device while the worker is blocked in network I/O. The miniaudio callback only consumes
the bounded stereo 48 kHz float ring, applies gain and updates counters. No Go
callback, decoding or network I/O occurs on the audio thread. Player already
squares the user volume; the native gain is linear.

Opening and seeking enqueue work. A seek preserves listener intent and
supersedes older seeks. The worker stops the device before resetting the ring,
flushes decoder/resampler state and discards samples before the requested
timestamp. A pending seek must suppress an old EOF immediately, including while
the remote seek is blocked. Demuxer EOF is not playback EOF: decoder/resampler
tails and queued PCM must finish first.

Close interrupts FFmpeg I/O, wakes the worker and joins it before freeing
callback-owned memory. Close is synchronous; blocked-open/read/seek regression
tests enforce a 500 ms bound on the tested machine. Pause synchronously stops device output;
the producer can retain bounded decoded audio but sleeps when paused/full.
Unexpected output-device stops become failures. Physical device switching still
requires device-level acceptance beyond the automated suite.

Go's per-track relay remains the sole HTTPS, authorization-header and redirect
boundary. FFmpeg sees a local path or an opaque loopback HTTP URL. It cannot
open secondary container resources, use an environment HTTP proxy, or enable
arbitrary demuxers/protocols. Network reads request bounded ranges. The former
AVPlayer-specific HLS adapter is removed.

Two designs were compared: a C-owned worker and a Go worker repeatedly calling
C decoder operations. The C-owned worker was selected because device/ring
lifetime already lives in C and a single owner keeps cancellation, seeks and
cleanup together. Custom AVIO callbacks into Go were rejected: the existing
relay already owns the required network policy. Independent review added
proxy suppression, paused-device race handling and stale-EOF regression checks.

The portable libraries do not make the whole application cross-platform. This
release builds and tests macOS arm64. Build inputs, licenses and corresponding
sources are described in [THIRD_PARTY.md](../THIRD_PARTY.md).
