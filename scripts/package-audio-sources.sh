#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
[[ $# == 1 && $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 1
version=$1
release="$PWD/build/release/$version"
destination="$release/mygo-music-lite-$version-sources.tar.gz"
[[ -z $(git status --porcelain) ]] || { echo 'Commit all source changes before packaging sources.' >&2; exit 1; }
[[ -f $release/source-commit.txt && $(cat "$release/source-commit.txt") == $(git rev-parse HEAD) ]] || { echo 'Source commit does not match the prepared app.' >&2; exit 1; }
[[ ! -e $destination ]] || { echo 'Source archive already exists.' >&2; exit 1; }
mkdir -p "$release"
stage=$(mktemp -d "$release/sources.XXXXXX")
trap 'rm -rf "$stage"' EXIT
folder="mygo-music-lite-$version"
mkdir -p "$stage/$folder"
git archive HEAD | tar -xf - -C "$stage/$folder"
mkdir -p "$stage/$folder/build/audio/sources"
cp build/audio/sources/ffmpeg-n9.0.2.tar.gz build/audio/sources/miniaudio-0.11.23.h "$stage/$folder/build/audio/sources/"
git rev-parse HEAD > "$stage/$folder/SOURCE_COMMIT"
tar -czf "$destination" -C "$stage" "$folder"
(cd "$release" && shasum -a 256 "$(basename "$destination")" > "$(basename "$destination").sha256")
echo "$destination"
