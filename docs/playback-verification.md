# Playback verification

## Run the checks

Use macOS Apple Silicon with Xcode Command Line Tools. Set `CGO_ENABLED=1` when using the MyGo CLI, which otherwise disables cgo.

```sh
go test ./internal/...
go test -race ./internal/player
go test ./internal/player -count=5
go test ./internal/app -run 'Test(SettingsSearchNavigation|ScrubSeeksOnRelease|SpaceAfterOpeningNowPlaying|NowPlayingFrameBudget|BackdropFadeEnds|SessionRestsWhilePaused)$' -count=5
```

The player tests run the macOS event loop and use real AVPlayer and libmpv instances. Tests cover MP3, FLAC, ALAC, AAC, Vorbis through libmpv, WavPack fallback, pause, seek, native EOF, queue advance, and repeat. libmpv-specific cases skip when that optional library is absent. Controller tests cover generated seek and pause sequences, missing fallback, stale loads, and polling that stops while paused.

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

The native test serves checked-in MP3 and WavPack fixtures over a local HTTP server with an authorization header. It drives Player through the byte relay, checks playback progress, pauses, seeks while paused, and resumes. WavPack verifies AVPlayer to optional libmpv fallback. Tests isolate app data through the player package's TestMain. These checks do not establish compatibility with every service or measure network CPU use.

For hardened-runtime verification, compile the player tests with `go test -c -o /tmp/mygo-lite-network.test ./internal/player`, sign that executable with the release identity and release entitlements, and run `(cd internal/player && /tmp/mygo-lite-network.test -test.run TestNativeNetworkTransport -test.count=5 -test.v)` so the fixture paths resolve. Repeat direct network playback in the signed app before publishing. HLS, DASH, and playlist playback are unsupported.
