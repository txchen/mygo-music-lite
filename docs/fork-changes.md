# Differences from upstream

MyGo Music Lite is a fork of [mygo-music](https://github.com/egoist/mygo-music). It targets local playback on macOS 13 or later, on Apple Silicon. The app release version is `0.2.0`. The upstream plugin compatibility baseline is separately fixed at `0.1.22`.

## Playback

Lite replaces the upstream audio decoding and PCM processing path with native engines. It tries AVPlayer first for a local file. If AVPlayer fails, it tries Homebrew's libmpv once. libmpv is optional, loaded on demand, and not included in the app bundle. Install it with `brew install mpv` when needed. Actual codec support depends on the native engine and macOS version.

The native engine boundary rejects network audio URLs. Plugin metadata requests, artwork, and manual file downloads can still use the network under the plugin's permissions. Plugins that control playback in another program retain the remote playback API.

Lite removes PCM processing, spectrum analysis, the lyric stage, word animation, speech transcription, persistent audio caching, normalization, fades, and gapless playback. Ordinary line-synced lyrics, local folders and tags, covers, playlists, search, queue controls, session restore, and media keys remain. Image caching and manually saving an audio file remain too.

See [playback verification](playback-verification.md) for native checks and the CPU measurement procedure. The engine change alone does not establish a measured performance improvement for every file or machine.

## App identity and distribution

Lite uses the bundle identifier `dev.txchen.mygo-music-lite`, the `mygo-music-lite` URL scheme, and a separate app data directory. Development uses `mygo-music-lite-dev` for URL links. Lite does not import the upstream app's settings or install its app updates. Plugin updates still use their original repositories.

The supported app bundle targets macOS Apple Silicon only. The release process and Homebrew cask belong to Lite. See [release instructions](../RELEASING.md) for building and publishing the bundle.

## Plugin compatibility

The manifest field `minAppVersion` keeps its upstream JSON name for existing plugins. Lite compares it with the immutable upstream compatibility baseline `0.1.22`, rather than Lite's app release version. This check applies to installation, updates, discovery, and development builds.

A missing requirement or a valid requirement at or below `0.1.22` passes the version check. A requirement above that baseline, such as `0.1.23` or `0.2.0`, is rejected. A malformed requirement is rejected too. Raising Lite's app release version does not raise the baseline.

Passing the version check does not guarantee that every plugin feature works. Plugins still need to respect Lite's local audio restriction and removed features. For example, returning a network URL from `stream` cannot enable native network playback, and timed lyric words do not restore the removed word animation. The baseline records the inherited API requirement, not complete upstream feature parity.
