# Publish MyGo Music Lite

Use macOS Apple Silicon, Xcode Command Line Tools, Go from `go.mod`, Bun, and authenticated GitHub CLI. The Developer ID certificate and its private key must be in the signing keychain. Store notarization credentials there with `xcrun notarytool store-credentials` first.

Lite releases have their own version, independent of the upstream plugin compatibility baseline. This release uses app version `0.4.1` and tag `v0.4.1`. Do not replace an asset already published under that tag.

## Prepare the signed submission

Commit the source to publish, then run:

```sh
scripts/build-audio.sh
go test ./internal/...
scripts/release-app.sh prepare 0.4.1 \
  'Developer ID Application: ROBOPUNK FOUNDATION (68S3R4UWP6)' \
  "$HOME/Library/Keychains/login.keychain-db"
```

The script creates `build/release/0.4.1/`, signs with hardened runtime, checks the bundle version, identifier, arm64 architecture, and Mach-O minimum OS version, and saves `submission.zip` and its SHA-256 checksum. It refuses an existing release directory. The only entitlement disables library validation so the optional Homebrew libmpv can load. The embedded moejs interpreter does not need JIT entitlements. libmpv is not bundled. FFmpeg and miniaudio are statically linked; the app includes their license notices. Publish the matching source archive described in THIRD_PARTY.md.

## Notarize and staple

```sh
scripts/release-app.sh notarize 0.4.1 robopunk-notary \
  "$HOME/Library/Keychains/login.keychain-db"
```

The command verifies the submission hash, submits it, saves the submission ID in `submission.json`, and waits for Apple's result. It saves Apple's log and checks its submission ID and archive hash. Rerun the command after an interruption to resume that submission. If interruption happens before the receipt is saved, recover the ID with `xcrun notarytool history --keychain-profile robopunk-notary --keychain "$HOME/Library/Keychains/login.keychain-db"` and save the matching receipt as `submission.json` before rerunning.

After acceptance, the script extracts the original submission, staples and validates the ticket, checks Gatekeeper and the signature, then creates `mygo-music-lite-0.4.1-darwin-arm64.zip` and its `.sha256` file. It refuses to overwrite a final archive. Keep the whole release directory as the release record.

Open the stapled app from the final zip and check local playback, direct network playback with seek and pause, optional mpv fallback, and a plugin before publishing. Signing and notarization do not verify those behaviors.

## Publish the app and Homebrew cask

Create the tag on the tested source commit and publish the final zip, corresponding audio/application sources, and their checksums:

```sh
scripts/package-audio-sources.sh 0.4.1
git tag v0.4.1
git push origin main v0.4.1
gh release create v0.4.1 \
  build/release/0.4.1/mygo-music-lite-0.4.1-darwin-arm64.zip \
  build/release/0.4.1/mygo-music-lite-0.4.1-darwin-arm64.zip.sha256 \
  build/release/0.4.1/mygo-music-lite-0.4.1-sources.tar.gz \
  build/release/0.4.1/mygo-music-lite-0.4.1-sources.tar.gz.sha256 \
  --title 'MyGo Music Lite 0.4.1' --notes-file /path/to/release-notes.md
scripts/render-cask.sh 0.4.1 > /path/to/homebrew-tap/Casks/mygo-music-lite.rb
```

Review and commit the cask in `txchen/homebrew-tap`, then push it. Verify installation from the public release:

```sh
brew install --cask txchen/tap/mygo-music-lite
```

The manual GitHub Actions build workflow produces an ad-hoc signed artifact for development. It does not publish releases. The app does not connect to the upstream app updater. Plugin updates retain the upstream behavior.
