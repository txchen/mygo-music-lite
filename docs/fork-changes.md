# MyGo Music Lite: installation and fork changes

MyGo Music Lite is a fork of [mygo-music](https://github.com/egoist/mygo-music). It targets native local and direct network playback on macOS 13 or later, on Apple Silicon. The app release version is `0.4.4`. The upstream plugin compatibility baseline is separately fixed at `0.1.22`.

## Install and upgrade

Requires macOS 13 or later on Apple Silicon. FFmpeg and miniaudio are bundled; they do not need to be installed separately.

```sh
brew install --cask txchen/tap/mygo-music-lite
```

To upgrade, quit the running app, then run:

```sh
brew update
brew upgrade --cask txchen/tap/mygo-music-lite
```

You can also download the app from [Lite releases](https://github.com/txchen/mygo-music-lite/releases/latest). Release bundles are signed and notarized.

## Downloads

Use **Download…** from a song's menu to save its original audio file. Network downloads use bounded byte ranges when supported, with a full-file fallback for servers that ignore the first range. Downloads validate range offsets, lengths, total size and available file validators; failed transfers leave an existing destination untouched. A connection or network read stalled for 30 seconds ends with an error.

The downloading notice stays visible until completion or dismissal. Errors remain visible until dismissed; successful downloads offer **Show in Folder**.

## Diagnose intermittent buffering

1. Open **Settings → Playback** and enable **Playback Diagnostic Log**.
2. Play or switch songs normally until the delay occurs.
3. Choose **Export…** in the same settings page and share the saved JSON file for analysis. Export also reveals the file in Finder.

Export before quitting: recording is session-only. Switching the log off keeps the evidence; switching it on again starts a fresh recording. The latest 16,384 events are retained, with an overflow count in the export. There is no automatic upload or playback-time disk logging. Song names, URLs, account credentials and raw error text are excluded.

Logs distinguish source resolution, validation, network connection stages, yt-dlp subprocesses, native versus fallback engines, playback position and output underruns. First-output observation uses the existing 250 ms player poll, so it is not a sample-accurate startup timestamp. Plugin requests have individual IDs; match them to playback attempts by time rather than assuming all concurrent requests belong to a song.

## Build

Requires Xcode Command Line Tools, Go as specified in `go.mod`, Bun, curl, and make on macOS Apple Silicon. The audio bridge requires cgo.

```sh
scripts/build-audio.sh
CGO_ENABLED=1 go tool mygo dev
CGO_ENABLED=1 go tool mygo build -platform darwin/arm64 -skip-dmg
go test ./internal/...
```

Open `build/darwin-arm64/MyGo Music Lite.app`. Development builds are ad-hoc signed, not notarized. See [release instructions](../RELEASING.md) for publishing and [bundled library licenses and rebuilding](../THIRD_PARTY.md) for dependency sources.

## Playback

Lite uses bundled FFmpeg decoding and miniaudio output instead of the upstream Go audio path. The backend performs decoding, resampling and volume adjustment with a bounded PCM ring. Homebrew's libmpv is retained as an optional fallback for failed tracks; it is loaded on demand and not bundled.

Local scanning includes MP3, FLAC, Ogg, Opus, WAV, M4A, AAC, AIFF, WMA, APE, WavPack, and WebM. The bundled backend includes decoders for these formats; unsupported variants can use the optional fallback. Install that fallback with `brew install mpv` if needed.

Direct network audio passes through an opaque per-track loopback relay. Native engines never receive the original URL or its headers. The relay streams bounded byte buffers and forwards Range and If-Range requests. It performs no decoding, transcoding, or complete-file disk buffering. A small offset-zero probe checks known audio container signatures before native requests proceed. The actual full response is checked too, so a successful probe cannot disguise a later playlist response.

External HLS, DASH, and playlist playback are unsupported. The relay rejects declared manifest types, manifest paths, manifest MIME types, and responses without a known audio signature. This validation is not a parser or sandbox for native media containers. libmpv has external references and adjacent-file autoloading disabled.

The FFmpeg backend reads fragmented MP4 directly through the relay, including byte-range seeking. The AVPlayer-specific generated HLS playlist and metadata rewriting from 0.3.1 have been removed. FFmpeg's enabled demuxers and protocols are restricted; secondary media-resource opens are denied.

A plugin's audio URL and each redirect must match its declared network permissions, including CDN hosts. A granted process permission also permits the same loopback hosts as plugin fetch. Headers apply only to the exact initial origin and remain stripped after an origin change. HTTPS downgrades and URL user information are rejected. Failures use redacted messages without credentials or signed URLs. Native engines can buffer while paused, but Lite adds no paused polling or background track downloader. Stop, track switch, closure, and terminal failure cancel the track's relay. Stop, switch and closure also cancel pending format validation. Initial connection, response headers, and stalled reads have timeouts without a whole-song duration limit.

Plugin metadata requests, artwork, and manual file downloads retain their existing behavior. Plugins that control playback in another program retain the remote playback API.

Lite removes spectrum analysis, the lyric stage, word animation, speech transcription, persistent audio caching, normalization, fades, and gapless playback. Ordinary line-synced lyrics, local folders and tags, covers, playlists, search, queue controls, session restore, and media keys remain. Image caching and manually saving an audio file remain too.

Playback time and progress refresh together once per second during steady playback. Pointer interaction, seeking, and other animations still update as needed.

See [playback verification](playback-verification.md) for native checks and the CPU measurement procedure. The engine change alone does not establish a measured performance improvement for every file or machine.

## App identity and distribution

Lite uses the bundle identifier `dev.txchen.mygo-music-lite`, the `mygo-music-lite` URL scheme, and a separate app data directory. Development uses `mygo-music-lite-dev` for URL links. Lite does not import the upstream app's settings or install its app updates. Plugin updates still use their original repositories.

The supported app bundle targets macOS Apple Silicon only. The release process and Homebrew cask belong to Lite. See [release instructions](../RELEASING.md) for building and publishing the bundle.

## Plugin compatibility

The manifest field `minAppVersion` keeps its upstream JSON name for existing plugins. Lite compares it with the immutable upstream compatibility baseline `0.1.22`, rather than Lite's app release version. This check applies to installation, updates, discovery, and development builds.

A missing requirement or a valid requirement at or below `0.1.22` passes the version check. A requirement above that baseline, such as `0.1.23` or `0.2.0`, is rejected. A malformed requirement is rejected too. Raising Lite's app release version does not raise the baseline.

Passing the version check does not guarantee that every plugin feature works. Plugins still need to respect Lite's direct audio requirement and removed features. A network `stream` needs audio and redirect host permissions, and cannot be an HLS or DASH manifest. Timed lyric words do not restore the removed word animation. The baseline records the inherited API requirement, not complete upstream feature parity.

For `playback: "stream"`, Lite accepts local file URLs and direct HTTP(S) audio returned by `stream`. It ignores the `gain` and `peak` normalization fields. For `playback: "remote"`, the plugin controls playback elsewhere through the retained remote API.

Plugin install links use `mygo-music-lite://install-plugin?repo=owner/repo`; development builds use `mygo-music-lite-dev://`. The upstream README's `mygo-music://` links target the original app.

## Linux and Windows preview (0.4.5)

The first Linux and Windows builds target x86-64. They use the same bundled
FFmpeg/miniaudio playback engine as macOS. The optional libmpv fallback is only
available on macOS. These packages have build validation but still need desktop,
real audio device, installation, and plugin acceptance testing.

Download the matching package from the 0.4.5 release. On Windows, run the
`mygo-music-lite-setup-0.4.5-amd64.exe` installer. This first Windows release is
unsigned. On Debian/Ubuntu, install the `.deb` with `sudo apt install ./PACKAGE.deb`.
Other Linux distributions can extract the `linux-amd64.tar.gz` and run
`./mygo-music-lite` from the extracted folder. Linux requires a graphical desktop
and a working audio service. Keep the extracted resources beside the executable.

For upgrades, quit the player and install the newer package. Keep your existing
app data. Please test local playback, network playback, pause/resume, seek,
track switching, plugins, tray behavior, and quitting/reopening the app.

To build on Ubuntu x86-64, install Go, Bun, make, gcc, and curl, then run
`AUDIO_TARGET=linux-amd64 scripts/build-audio.sh` and
`CGO_ENABLED=1 go tool mygo build -platform linux/amd64`.
For Windows cross-compilation, additionally install
`gcc-mingw-w64-x86-64-posix` and `nsis`, use `AUDIO_TARGET=windows-amd64`, then
`CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc-posix go tool mygo build -platform windows/amd64`.
