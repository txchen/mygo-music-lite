#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
[[ $# == 1 && $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Usage: scripts/render-cask.sh VERSION' >&2; exit 1; }
version=$1
release="$PWD/build/release/$version"
asset="mygo-music-lite-$version-darwin-arm64.zip"
(cd "$release" && shasum -a 256 -c "$asset.sha256") >&2
sha=$(shasum -a 256 "$release/$asset" | awk '{print $1}')
cat <<CASK
cask "mygo-music-lite" do
  version "$version"
  sha256 "$sha"

  url "https://github.com/txchen/mygo-music-lite/releases/download/v#{version}/mygo-music-lite-#{version}-darwin-arm64.zip"
  name "MyGo Music Lite"
  desc "Local music player with AVPlayer and optional mpv playback"
  homepage "https://github.com/txchen/mygo-music-lite"

  depends_on arch: :arm64
  depends_on macos: :ventura

  app "MyGo Music Lite.app"
end
CASK
