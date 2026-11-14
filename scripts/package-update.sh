#!/bin/bash
# Build the updater payload from the verified, notarized distribution archive.
set -euo pipefail
cd "$(dirname "$0")/.."
[[ $# == 2 && $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Usage: scripts/package-update.sh VERSION UPDATE_KEY_FILE' >&2; exit 1; }
version=$1
key=$2
release="$PWD/build/release/$version"
asset="mygo-music-lite-$version-darwin-arm64"
(cd "$release" && shasum -a 256 -c "$asset.zip.sha256")
stage=$(mktemp -d "$release/update.XXXXXX")
trap 'rm -rf "$stage"' EXIT
ditto -x -k "$release/$asset.zip" "$stage"
app="$stage/MyGo Music Lite.app"
[[ $(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$app/Contents/Info.plist") == "$version" ]]
[[ $(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$app/Contents/Info.plist") == dev.txchen.mygo-music-lite ]]
codesign --verify --deep --strict "$app"
xcrun stapler validate "$app"
spctl --assess --type execute "$app"
COPYFILE_DISABLE=1 tar -czf "$stage/$asset.tar.gz" -C "$stage" 'MyGo Music Lite.app'
go run ./cmd/sign-update "$stage/$asset.tar.gz" "$version" "$key"
mv "$stage/$asset.tar.gz" "$release/"
mv "$stage/update-darwin-arm64.json" "$release/"
echo "Publish $release/$asset.tar.gz and $release/update-darwin-arm64.json with the release."
