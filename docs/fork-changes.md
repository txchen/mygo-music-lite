# Differences from upstream

MyGo Music Lite is a fork of [mygo-music](https://github.com/egoist/mygo-music). It targets native local and direct network playback on macOS 13 or later, on Apple Silicon. The app release version is `0.4.0`. The upstream plugin compatibility baseline is separately fixed at `0.1.22`.

## Playback

Lite uses bundled FFmpeg decoding and miniaudio output instead of the upstream Go audio path. The backend performs decoding, resampling and volume adjustment with a bounded PCM ring. Homebrew's libmpv is retained as an optional fallback for failed tracks; it is loaded on demand and not bundled.

Direct network audio passes through an opaque per-track loopback relay. Native engines never receive the original URL or its headers. The relay streams bounded byte buffers and forwards Range and If-Range requests. It performs no decoding, transcoding, or complete-file disk buffering. A small offset-zero probe checks known audio container signatures before native requests proceed. The actual full response is checked too, so a successful probe cannot disguise a later playlist response.

External HLS, DASH, and playlist playback are unsupported. The relay rejects declared manifest types, manifest paths, manifest MIME types, and responses without a known audio signature. This validation is not a parser or sandbox for native media containers. libmpv has external references and adjacent-file autoloading disabled.

The FFmpeg backend reads fragmented MP4 directly through the relay, including byte-range seeking. The AVPlayer-specific generated HLS playlist and metadata rewriting from 0.3.1 have been removed. FFmpeg's enabled demuxers and protocols are restricted; secondary media-resource opens are denied.

A plugin's audio URL and each redirect must match its declared network permissions, including CDN hosts. A granted process permission also permits the same loopback hosts as plugin fetch. Headers apply only to the exact initial origin and remain stripped after an origin change. HTTPS downgrades and URL user information are rejected. Failures use redacted messages without credentials or signed URLs. Native engines can buffer while paused, but Lite adds no paused polling or background track downloader. Stop, track switch, closure, and terminal failure cancel the track's relay. Stop, switch and closure also cancel pending format validation. Initial connection, response headers, and stalled reads have timeouts without a whole-song duration limit.

Plugin metadata requests, artwork, and manual file downloads retain their existing behavior. Plugins that control playback in another program retain the remote playback API.

Lite removes spectrum analysis, the lyric stage, word animation, speech transcription, persistent audio caching, normalization, fades, and gapless playback. Ordinary line-synced lyrics, local folders and tags, covers, playlists, search, queue controls, session restore, and media keys remain. Image caching and manually saving an audio file remain too.

See [playback verification](playback-verification.md) for native checks and the CPU measurement procedure. The engine change alone does not establish a measured performance improvement for every file or machine.

## App identity and distribution

Lite uses the bundle identifier `dev.txchen.mygo-music-lite`, the `mygo-music-lite` URL scheme, and a separate app data directory. Development uses `mygo-music-lite-dev` for URL links. Lite does not import the upstream app's settings or install its app updates. Plugin updates still use their original repositories.

The supported app bundle targets macOS Apple Silicon only. The release process and Homebrew cask belong to Lite. See [release instructions](../RELEASING.md) for building and publishing the bundle.

## Plugin compatibility

The manifest field `minAppVersion` keeps its upstream JSON name for existing plugins. Lite compares it with the immutable upstream compatibility baseline `0.1.22`, rather than Lite's app release version. This check applies to installation, updates, discovery, and development builds.

A missing requirement or a valid requirement at or below `0.1.22` passes the version check. A requirement above that baseline, such as `0.1.23` or `0.2.0`, is rejected. A malformed requirement is rejected too. Raising Lite's app release version does not raise the baseline.

Passing the version check does not guarantee that every plugin feature works. Plugins still need to respect Lite's direct audio requirement and removed features. A network `stream` needs audio and redirect host permissions, and cannot be an HLS or DASH manifest. Timed lyric words do not restore the removed word animation. The baseline records the inherited API requirement, not complete upstream feature parity.
