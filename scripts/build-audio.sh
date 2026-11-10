#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
target=${AUDIO_TARGET:-darwin-arm64}
configure_flags=()
case "$target" in
  darwin-arm64)
    [[ $(uname -s) == Darwin && $(uname -m) == arm64 ]] || exit 1
    configure_flags=(--cc=clang --extra-cflags=-mmacosx-version-min=13.0 --extra-ldflags=-mmacosx-version-min=13.0) ;;
  linux-amd64)
    configure_flags=(--cc=gcc --disable-x86asm) ;;
  windows-amd64)
    configure_flags=(--cc=x86_64-w64-mingw32-gcc-posix --target-os=mingw32 --arch=x86_64 --enable-cross-compile --cross-prefix=x86_64-w64-mingw32- --disable-x86asm) ;;
  *) echo "Unsupported audio target: $target" >&2; exit 1 ;;
esac
root="$PWD/build/audio"
prefix="$root/$target"
sources="$root/sources"
recipe=$(shasum -a 256 "$0" | awk '{print $1}')
if [[ -f $prefix/recipe.sha256 && $(cat "$prefix/recipe.sha256") == "$recipe" && -f $prefix/artifacts.sha256 ]] && (cd "$prefix" && shasum -a 256 -c artifacts.sha256 >/dev/null 2>&1); then
  echo 'Audio dependencies are up to date.'
  exit 0
fi
mkdir -p "$sources"
fetch() {
  local url=$1 destination=$2 sha=$3
  if [[ ! -f $destination ]] || [[ $(shasum -a 256 "$destination" | awk '{print $1}') != "$sha" ]]; then
    curl --fail --location --retry 3 "$url" -o "$destination.tmp"
    [[ $(shasum -a 256 "$destination.tmp" | awk '{print $1}') == "$sha" ]] || { echo "Checksum mismatch: $destination" >&2; exit 1; }
    mv "$destination.tmp" "$destination"
  fi
}
fetch https://github.com/FFmpeg/FFmpeg/archive/refs/tags/n9.0.2.tar.gz "$sources/ffmpeg-n9.0.2.tar.gz" 6e374ed621e48faa40639307dff48ba6fe574a509977956d2cce9669b7cc27e9
fetch https://raw.githubusercontent.com/mackron/miniaudio/0.11.23/miniaudio.h "$sources/miniaudio-0.11.23.h" 7e4f3f13c8fe66df2080ac3dd12a89193e3c2463cb7f067c798abd7331cd8ee6
work=$(mktemp -d "$root/compile.XXXXXX")
trap 'rm -rf "$work"' EXIT
tar -xzf "$sources/ffmpeg-n9.0.2.tar.gz" -C "$work"
mkdir -p "$prefix/include" "$prefix/licenses"
cd "$work/FFmpeg-n9.0.2"
./configure --prefix="$prefix" "${configure_flags[@]}" \
  --disable-everything --disable-autodetect --disable-programs --disable-doc --disable-debug \
  --disable-avdevice --disable-avfilter --disable-swscale --enable-network --enable-static --disable-shared \
  --enable-decoder=aac,mp3,flac,alac,opus,vorbis,wavpack,ape,wmav1,wmav2,wmapro,wmalossless,pcm_s8,pcm_u8,pcm_s16le,pcm_s16be,pcm_s24le,pcm_s24be,pcm_s32le,pcm_s32be,pcm_f32le,pcm_f32be,pcm_f64le,pcm_f64be \
  --enable-demuxer=aac,mov,mp3,flac,wav,matroska,ogg,wv,ape,asf,aiff \
  --enable-parser=aac,mpegaudio,flac,opus,vorbis \
  --enable-protocol=file,http,tcp > "$root/configure.log" 2>&1
make -j4 > "$root/build.log" 2>&1
make install >> "$root/build.log" 2>&1
cp COPYING.LGPLv2.1 "$prefix/licenses/FFmpeg-LGPL-2.1.txt"
cp "$sources/miniaudio-0.11.23.h" "$prefix/include/miniaudio.h"
(cd "$prefix" && find include lib licenses -type f -exec shasum -a 256 {} \; > artifacts.sha256)
printf '%s\n' "$recipe" > "$prefix/recipe.sha256"
echo "Built audio dependencies: $prefix"
