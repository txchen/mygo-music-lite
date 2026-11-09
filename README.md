# MyGo Music Lite

A macOS Apple Silicon fork of [mygo-music](https://github.com/egoist/mygo-music), focused on low CPU use during local playback.

## Install

Requires macOS 13 or later on Apple Silicon.

```sh
brew install --cask txchen/tap/mygo-music-lite
```

Release bundles are Developer ID signed and notarized. See [release instructions](RELEASING.md) to build and publish one.

## Playback

AVPlayer plays local audio first. If it fails, the app tries Homebrew's libmpv once. libmpv is optional, loaded only when needed, and is not bundled.

```sh
brew install mpv
```

Without libmpv, supported files still play through AVPlayer. Unsupported files show an installation hint. The app does not install dependencies automatically.

The library retains folders, tags, covers, playlists, search, ordinary line-synced lyrics, queue controls, session restore, and media keys. Local scanning includes MP3, FLAC, Ogg, Opus, WAV, M4A, AAC, AIFF, WMA, APE, WavPack, and WebM. Actual codec support depends on the engine and macOS version.

There is no PCM processing, spectrum, lyric stage, word animation, speech transcription, persistent audio cache, normalization, fade, or gapless playback. Image caching and manually saving a file remain.

Stream playback is deliberately local-only in this first version. Plugin APIs remain, but native network playback needs separate authentication, redirect, and seeking validation. A new YouTube Music plugin comes later. Navidrome's direct stream URLs fit the existing API, but network playback is not yet enabled or verified in Lite.

The app uses its own identifier, URL scheme, and data directory. It does not import upstream settings or install upstream app updates. The version stays at 0.1.22 to preserve the plugin API version check.

## Build

Requires macOS 13 or later on Apple Silicon, Xcode Command Line Tools, Go as specified in `go.mod`, and Bun. The AVPlayer bridge needs cgo.

```sh
CGO_ENABLED=1 go tool mygo dev
CGO_ENABLED=1 go tool mygo build -platform darwin/arm64 -skip-dmg
go test ./internal/...
```

Open `build/darwin-arm64/MyGo Music Lite.app`. It is ad-hoc signed, not notarized.

See [playback verification](docs/playback-verification.md) for native tests and the CPU measurement command.

## Plugins

A plugin is a folder in the app's `plugins` directory (Settings > Plugins > + > Show Plugins Folder) with a `plugin.json` and one JavaScript or TypeScript file. Install one with the + in Settings > Plugins from GitHub, by its repository or its address, or from its folder or a zip of it on this computer, or drop the folder in by hand. Remove one from its ⋯ menu there. The plugin API is retained from upstream. This fork targets macOS Apple Silicon only. [`examples/demo-plugin`](examples/demo-plugin) does everything a plugin can with made-up data: copy it to start.

```json
{
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "minAppVersion": "0.1.8",
  "main": "index.js",
  "playback": "stream",
  "permissions": { "network": ["api.example.com"] },
  "settings": [{ "key": "apiKey", "label": "API Key", "help": "From your account page", "link": "https://example.com/keys" }],
  "navigation": [
    { "builtin": "home" },
    { "title": "Charts", "page": "charts", "icon": "chart" },
    { "title": "Stations", "builtin": "stations" }
  ]
}
```

```js
export async function search({ query, kind, offset }) {
  const res = await fetch(`https://api.example.com/search?q=${encodeURIComponent(query)}&type=${kind}&offset=${offset}`, {
    headers: { authorization: `Bearer ${host.setting("apiKey")}` },
  });
  return { tracks: (await res.json()).items.map(toTrack), more: false };
}

export function stream({ id, quality }) {
  return { url: `https://api.example.com/audio/${id}.mp3`, type: "mp3" };
}
```

Export a function for each thing the plugin does. Each takes one object of parameters and returns a value or a promise. What it doesn't export, the app leaves out of its pages.

| Export | Parameters | Returns |
| --- | --- | --- |
| `signIn`, `signOut`, `account` | | `{id, name, imageUrl, page}`; `account` returns `null` when signed out. `page` is a `browse` page of the listener's own, which the account menu opens as My Page |
| `setSetting` | `{key, value}` | |
| `home` | | `Browse` |
| `browse` | `{page}` | `Browse` |
| `search` | `{query, kind, offset}`, where `kind` is `tracks`, `albums`, `artists`, `playlists` or `shows` | `{tracks or albums or ..., more}` |
| `suggest` | `{query}` | `[string]` |
| `recent` | | `[Track]` |
| `played` | `{id}` | |
| `liked` | `{offset}` | `{tracks, more}` |
| `isLiked` | `{ids}` | `{id: bool}` |
| `like`, `likeEpisode` | `{id, on}` | `like` likes a song, and `likeEpisode` an episode: without it, episodes show no heart |
| `album` | `{id}` | `{album, tracks}` |
| `artist` | `{id}` | `{artist, tracks, albums}` |
| `saveAlbum`, `followArtist`, `savePlaylist`, `subscribeShow` | `{id, on}` | |
| `savedAlbums` | `{offset}` | `{albums, more}` |
| `playlists` | | `[Playlist]` |
| `playlist` | `{id}` | `Playlist` with `tracks` |
| `createPlaylist` | `{name}` | `Playlist` |
| `renamePlaylist` | `{id, name}` | |
| `deletePlaylist` | `{id}` | |
| `addToPlaylist`, `removeFromPlaylist` | `{id, trackIds}` | |
| `movePlaylistTrack` | `{id, from, to}` | |
| `shows` | | `[Show]` |
| `show` | `{id, offset}` | `{show, episodes, more}` |
| `stations` | | `[Station]` |
| `stationNext` | `{id}` | `[Track]` |
| `comments` | `{trackId, offset}` | `{comments, total, more}` |
| `likeComment` | `{trackId, commentId, on}` | |
| `postComment` | `{trackId, text}` | `Comment` |
| `lyrics` | `{id}` | `{lrc, translation}`, or `{lines}` with timed `words` |
| `stream` | `{id, quality}` | `{url, type, headers, size, gain, peak}`, where `type` identifies the container. Lite ignores `gain` and `peak`; native engines decide which codecs they can play |
| `remotePlay` | `{ids, start, position}` | |
| `remotePause`, `remoteResume` | | |
| `remoteSeek` | `{position}` | |
| `remoteVolume` | `{percent}` | |
| `remoteState` | | `{playing, trackId, position, duration, volume, device}` or `null` |
| `remoteDevices` | | `[{id, name, active}]` |
| `remoteUse` | `{deviceId}` | |

The shapes are in [`internal/music/music.go`](internal/music/music.go) by their JSON names. Times are in milliseconds. A `Browse` is `{title, subtitle, imageUrl, description, sections}`: with an `imageUrl`, such as a listener's avatar, it's headed round, as an artist's page is, and `description` goes under the title a line to each line. A section can hold `tracks`, `albums`, `artists`, `playlists`, `shows`, `stations`, `links` (`{title, subtitle, imageUrl, page}`), a `morePage`, and `empty`, what it says while it holds nothing. A source's first section of `tracks` leads its Home as a card of the day's songs, which opens a page of all of them, headed by the section's `pageTitle` if it has one, else its `title`, and its `subtitle` after the count of songs. A show's `publisherPage` is a `browse` page of who publishes it, which their name on the show's page opens.

An `imageUrl` can say where its size goes, with `{size}`, for an image server that makes pictures the size asked: `https://img.example.com/cover.jpg?param={size}y{size}` or `https://img.example.com/{size}/cover.jpg`. The app fills in how many pixels across it shows the picture at, 40 in a row and 600 in Now Playing, or 600 where nothing asks for a size. `%7Bsize%7D`, as JavaScript's `URL` writes it in a query, counts too. An app older than 0.1.20 fetches `{size}` as it is, so a plugin that gives it wants a `minAppVersion` of 0.1.20. Without it, give pictures at about 800 pixels if the service can, rather than as large as they were made: the app fetches and decodes the whole of each, a few megabytes for one thousands of pixels across, to show it in a row.

Of the `playlists`, those with `mine` are the listener's own, listed under My Playlists, and the rest are ones they saved. One that is `mine` and also `fixed`, such as the songs they liked, is kept by the source itself: the app doesn't offer to rename it, delete it, add songs to it, take them out or move them.

`navigation` is the plugin's sidebar, in its order. An entry opens a `browse` page of its own by `page`, or one of the app's by `builtin`: `home`, `liked`, `savedAlbums`, `stations` or `shows`, which can be given a `title`. Without `navigation`, the sidebar has an entry for each of the app's pages the plugin can do. Up to four stations show as a switch over one card, and more as a grid.

`minAppVersion` is the oldest version of MyGo Music the plugin works with. An older one won't install it, and says to update first. Versions before 0.1.8 don't read it.

### Translating a plugin

Put the translations in `locales/<language>.json` next to `plugin.json`: `locales/zh-CN.json` for Simplified Chinese, the language the app speaks besides English. Each one maps the English text to the translation:

```json
{ "Example": "示例", "API Key": "API 密钥", "From your account page": "在你的账号页面获取", "Charts": "排行榜" }
```

The app translates the plugin's `name`, its settings' `label` and `help` and its `navigation` titles with them. For what the plugin says itself, such as a page's titles or an error's message, `host.t(text)` answers the translation, or `text` when there's none, and `host.language()` the listener's language, `en` or `zh-CN`. Both follow the listener as they change it, and the page they're on loads again.

With `"playback": "stream"`, this version plays local file URLs returned by `stream`. Network streams are reserved for a later version. With `"playback": "remote"`, the plugin makes the sound itself, or has another program do so, and the app calls the `remote` functions and polls `remoteState`.

Throw an error with a `code` to say what's wrong: `signIn` prompts the listener to sign in, `setup` sends them to the plugin's settings, `unavailable` means this can't be played, and `unsupported` means the plugin doesn't do this. Any other error shows its message.

### Publishing a plugin

Put it in a public GitHub repository, with `plugin.json` at the top. The app takes the latest release: a `.zip` asset named for the platform, such as `plugin-darwin-arm64.zip`, or one named for none, or else the source. Without a release it takes the latest commit of the default branch. A new tag or commit is a new version, which shows as an update in the settings. An update asking for more permissions asks the listener again.

Add the topic `mygo-music-plugin` to the repository to have it listed on the [plugins page](https://mygo-music.egoist.dev/plugins).

Link to `mygo-music-lite://install-plugin?repo=owner/repo` for an install button: it opens the app, which downloads the plugin and asks the listener before installing it. The development app (`mygo dev`) answers `mygo-music-lite-dev://` instead.

### What a plugin can reach

A plugin has the JavaScript standard library, timers, `fetch` and its relatives, `URL`, `console` and `host`:

| | |
| --- | --- |
| `host.platform`, `host.arch` | such as `darwin`, `arm64` |
| `host.dir`, `host.dataDir` | the plugin's folder, and one for its data |
| `host.storage.get(key)`, `.set(key, value)`, `.remove(key)` | JSON values that outlive the app |
| `host.setting(key)` | what the listener filled in |
| `host.t(text)`, `host.language()` | the text in the listener's language, from the plugin's `locales`, and that language: `en` or `zh-CN` |
| `host.removeDataDir(name)` | removes a folder inside `host.dataDir` |
| `host.crypto.md5(text)` | its hash, in hex |
| `host.crypto.aesEcbEncrypt(key, text)` | AES-ECB with PKCS#7 padding, in hex, for services whose requests are made that way |
| `host.openURL(url)` | opens a web page in the listener's browser |
| `host.signInPrompt({qr, text})` | what the sign-in window shows while `signIn` goes on: a QR code to scan, and what to do |
| `host.waitForRedirect({port, path, timeout, page})` | the end of a browser sign-in: listens on `127.0.0.1` and answers the query it's redirected back with. Needs the network permission |
| `host.webSignIn({url, cookiesOf, until, userAgent})` | a sign-in on the service's own page, for one that has no other way in: the app shows `url` in a window of its own, apart from every other page and forgotten after, and once the cookies named in `until` are all there, closes it and answers `{cookie}`, the Cookie header a request to `cookiesOf` would send. `cookiesOf` has to be a host the network permission reaches. `userAgent` is what the page is told it's shown in |
| `host.spawn(command, args)` | starts a program, by a path from the plugin's folder or absolute, or a name on `PATH`. Needs the process permission to name it. Returns `{pid, write(text), closeStdin(), readLine(), wait(), kill(), errorOutput()}` |

Everything else takes a permission in `plugin.json`:

- `"network": ["api.example.com", "*.example.com"]` lists the hosts `fetch` can reach, or `["*"]` for any. Without it, `fetch` reaches nothing.
- `"requestHeaders"` are headers the app sends with what it fetches itself for the plugin, its pictures (`image`) and its songs' audio (`audio`), for a service that keeps those behind its session as well as its API:

  ```json
  "requestHeaders": [
    { "hosts": ["{setting:server}"], "kinds": ["image", "audio"], "headers": { "cookie": "music-token={storage:token}" } },
    { "hosts": ["img.example.com"], "kinds": ["image"], "headers": { "referer": "https://example.com/" } }
  ]
  ```

  `hosts` are hosts or `*.domain`, which the network permission has to reach, or `{setting:key}`, the host of the server a setting names, such as `https://nas.local:5666`, while the network permission reaches it. `*` isn't one. Without `kinds`, they go with both. In `headers`, `{setting:key}` is what the listener filled in and `{storage:key}` what the plugin keeps with `host.storage`, as each is when the request goes out: a token the plugin stores anew goes with the next one, and none is kept in a URL. A header whose value isn't there yet, as before signing in, is left out. The headers a `stream` answers come first. Image and manual-download requests do not forward these headers to another host on redirect. Native network playback is disabled in this version.
- `"process": ["bin/helper-*", "ffmpeg"]` lists the programs `host.spawn` can start, for what JavaScript can't do: a path from the plugin's folder or absolute, or a name looked up on `PATH`. A path only matches a path and a name only a name, and `*` matches within one folder, as in a glob. `["*"]`, or `true`, asks for any program, which the listener is warned about. The listener has to allow it in Settings first, as a program can do anything they can. Programs end when the plugin does, and once allowed, `fetch` can reach this machine too, to talk to them. Ship one binary per platform and pick it with `host.platform` and `host.arch`.

## Credits

- [mygo-music](https://github.com/egoist/mygo-music) provides the original application and plugin API.
- [folia-major](https://github.com/chthollyphile/folia-major) inspired the upstream lyric stage, which this fork removes.

## License

AGPL-3.0. See [LICENSE](LICENSE).
