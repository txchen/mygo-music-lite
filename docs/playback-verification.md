# Playback verification

## 0.4.1 refresh verification, 2026-11-09

On the same M1 Mac mini, steady playback of a 180-second local AAC file on
the visible Home page now builds and paints once per second. Five consecutive
10-second samples measured full-process CPU at a median of 2.11% (1.88–2.14%).
The earlier four-paints-per-second Lite build measured 3.09% (3.01–3.22%).
These were separate batches, not an interleaved A/B; they do not establish
the same savings for other hardware, tracks, animated pages, or network playback.

The full internal suite and MyGo vet passed. Playback-clock accessibility,
seek-on-release, and Space-after-opening-Now-Playing checks passed five runs.
Native window checks confirmed seeking, advancing visible/accessibility time,
and a stable paused position. The shared clock retains standard text widgets.


## 0.4.0 verification, 2026-11-09

On an M1 Mac mini running macOS 27.2, the complete internal test suite, MyGo vet,
and player Go race tests passed. A separate AddressSanitizer native harness
passed 40 open/rapid-seek/pause/close cycles. Go's race detector does not cover
the C implementation; the sanitizer and ownership review provide separate
evidence, not a proof of all native races being absent.

Developer ID hardened-runtime player tests passed local formats, direct HTTP
and fragmented MP4, queue/repeat, EOF restart, gain output, and blocked-I/O
cancellation. Blocked open/read/seek closes stayed under the 500 ms test bound.
Pausing a stalled read stops the device rather than leaving silent callbacks.

The real application with its opt-in isolated driver also passed local AAC and
anonymous YouTube playback of `-Siljd9PwKs`, seeking to 5000 seconds before
continuing and pausing. The network run consumed about 3 MiB through bounded
ranges rather than fetching the whole track. It used the final playback core
and hardened signing; the driver build differs from the release by the driver
itself. That run did not verify visible UI rendering or physical listening. Output-energy tests separately confirmed nonzero samples
reach the device callback at nonzero volume. Linux/Windows and physical device
switching were not tested.

## Run the checks

Use macOS Apple Silicon with Xcode Command Line Tools. Set `CGO_ENABLED=1` when using the MyGo CLI, which otherwise disables cgo.

```sh
scripts/build-audio.sh
go test ./internal/...
go test -race ./internal/player
go test ./internal/player -count=5
go test ./internal/app -run 'Test(SettingsSearchNavigation|ScrubSeeksOnRelease|SpaceAfterOpeningNowPlaying|NowPlayingFrameBudget|BackdropFadeEnds|SessionRestsWhilePaused)$' -count=5
```

The player tests run the macOS event loop and the bundled FFmpeg/miniaudio backend. They exercise MP3, FLAC, ALAC, AAC (raw and fragmented MP4), Vorbis, WavPack, fragmented FLAC, pause, seek, EOF, queue advance, and repeat. The optional libmpv fallback has separate cases that skip when that library is absent. Controller tests cover pending seek and pause, missing fallback, stale loads, and polling that stops while paused.

`testdata/seek.wv` is a two-second WavPack conversion of the existing FLAC fixture:

```sh
ffmpeg -i internal/player/testdata/seek.flac -t 2 -c:a wavpack internal/player/testdata/seek.wv
```

These are executable tests, not formal verification. TLC was unavailable for this work.

## Measure the complete app

The `playbackcheck` build tag adds a driver to the normal application. It creates a separate catalog and cache under `MM_CHECK_DIR`, scans the source folder through the local library, plays the selected file at zero volume, and records CPU time and playback position. It does not alter the normal app's settings or the system volume.

Use a track longer than two minutes:

```sh
OUT="$(mktemp -d)"
CGO_ENABLED=1 GOFLAGS=-tags=playbackcheck go tool mygo build \
  -platform darwin/arm64 -skip-dmg -o "$OUT/build"
MM_CHECK_DIR="$OUT/run" MM_CHECK_FILE="$HOME/bench-music/ghost-town.mp3" \
  "$OUT/build/darwin-arm64/MyGo Music Lite.app/Contents/MacOS/MyGo Music Lite"
```

Read `$OUT/run/results.json`. Each CPU value is the process's cumulative user and system CPU time divided by elapsed time. A single fully occupied core is 100%. The driver fails a sample if the playback clock does not advance throughout it, or if it advances while paused. `assertions.txt` records the CoreAudio output assertion.

The default sequence measures five hidden-window intervals, one visible interval, one Now Playing interval, and one paused interval. Each interval lasts ten seconds after a two-second settling period. Set `MM_CHECK_MODES=hidden,paused` for a shorter diagnostic run. Visible cases are marked unmeasured when the session is locked. Do not treat a stale screenshot from a locked session as rendering evidence.

CPU excludes `coreaudiod`, WindowServer, and the driver’s child commands. This is not a power measurement. Use a separate profile run because profiling changes CPU usage.

## Local result, 2026-11-08

Mac mini M1, eight cores, macOS 27.2. The screen was locked. The app used AVPlayer, an isolated local catalog, no installed plugins, and zero player volume. Native audio conversion appears in the separate `sample` trace, and macOS reported an `audio-out` assertion. Playback position advanced throughout all playing intervals.

| File | Hidden-window CPU median | Range | Paused CPU, one interval |
| --- | ---: | ---: | ---: |
| MP3, `ghost-town.mp3` | 1.41% | 1.39% to 1.55% | 0.17% |
| FLAC, `zhou.flac` | 1.73% | 1.70% to 1.82% | 0.17% |

Each median uses five consecutive ten-second intervals in one process, not five cold launches. The files ran sequentially, not as an alternating comparison against upstream. These results establish low background CPU on this machine, not a measured speedup over another build.

Raw samples are in [playback-samples.json](playback-samples.json). The full local artifacts, including the profile and CoreAudio assertions, are under `/Users/hibiki/.pi/agent/pstack-runs/mygo-lite-native-20261108/`.

Visible-window CPU remains unverified because the session was locked. The UI interaction and frame-budget tests passed; a tester-rendered Now Playing screenshot was inspected separately. macOS 13 and later are the declared deployment target, but only this machine's macOS version was exercised. Independent review was unavailable.

## Network playback

Run the native HTTP transport checks with:

```sh
go test ./internal/player -run 'TestNativeNetworkTransport|TestAudioRelay|TestNetworkStream' -count=5
go test ./internal/plugin -run TestStreamNetworkAuthorization -count=5
```

The native test serves checked-in MP3 and WavPack fixtures over a local HTTP server with an authorization header. It drives Player through the byte relay, checks playback progress, pauses, seeks while paused, and resumes. WavPack and fragmented MP4 now verify the bundled backend without fallback. Tests isolate app data through the player package's TestMain. These checks do not establish compatibility with every service or measure network CPU use.

For hardened-runtime verification, compile the player tests with `go test -c -o /tmp/mygo-lite-network.test ./internal/player`, sign that executable with the release identity and release entitlements, and run `(cd internal/player && /tmp/mygo-lite-network.test -test.run TestNativeNetworkTransport -test.count=5 -test.v)` so the fixture paths resolve. Repeat direct network playback in the signed app before publishing. External HLS, DASH, and playlist playback are unsupported.

The AVPlayer-specific HLS adapter was removed in 0.4.0. Network tests now exercise fragmented AAC and FLAC directly through the relay. Authorization, redirect, invalid-container and cancellation checks remain.

For isolated real-application network verification, use the `playbackcheck` build with `MM_CHECK_URL` instead of `MM_CHECK_FILE`; `MM_CHECK_SEEK=5000` also verifies a distant seek before sampling. Give it a public test URL or a temporary fixture server, never account credentials. These variables are absent from the normal release's behavior.


## Network range interruption regression (2026-11-09)

A signed-in real-app run of Daft Punk's “Give Life Back to Music” reproduced the listener's interruption near 1:02 twice. Diagnostic playback state changed from `portablePlayer` to `mpvPlayer` at that boundary. Earlier 30-second online probes missed this failure; their fast startup results do not establish uninterrupted playback.

The native decoder requests 1 MiB ranges and previously kept its loopback HTTP connection alive. The relay's server expires idle connections after 30 seconds, while consuming a range of this AAC stream takes roughly a minute. The next range could reuse the expired socket and fail. Native range requests now use separate loopback connections; Go's upstream HTTP/TLS connection pool remains in use. `TestPortableExpiredRangeConnection` closes idle server connections and checks that native playback crosses the range boundary without a fallback. The original option failed at about 3.64 seconds with a larger-bitrate WAV fixture; the fix passed three repeated runs.

A separate PCM-output regression exposed the volume mismatch on fallback. mpv's [volume implementation](https://github.com/mpv-player/mpv/blob/v0.41.0/player/audio.c#L161) cubes its percentage. Passing linear gain 0.64 therefore produced an energy ratio of 0.068719 instead of 0.409600. The adapter now converts linear gain to mpv's percentage, and `TestMPVLinearGain` verifies actual rendered float PCM, not just the property value. Five repeated runs passed after the correction.

Diagnostic artifacts are in `/tmp/mygo-buffer-audit/`. They are not committed because this investigation uses a local signed-in session. Logs omit credentials and signed audio URLs. Startup delays above five seconds remain a separate report until reproduced through the complete app.

The corrected full app retained the signed-in YouTube session and played the complete first track without backend switching or buffer underruns, then advanced to the next track. The listener confirmed the previously failing minute boundary no longer interrupted playback. Five manual album switches and eight further listener-selected songs also stayed on the primary engine. In the eight listener-selected starts, resolution took 122–267 ms and the first positive playback-position sample arrived within 771–1527 ms. Position sampling is periodic, so these are observation bounds, not precise first-sound latencies. The reported five-second startup on the other MacBook was not reproduced and is not claimed fixed by 0.4.2.
