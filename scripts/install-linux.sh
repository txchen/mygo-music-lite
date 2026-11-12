#!/bin/sh
# Install the published Linux build for the current user; preserve application data.
set -eu
main() {
  [ "$(uname -s)" = Linux ] || { echo 'This installer requires Linux.' >&2; exit 1; }
  [ "$(uname -m)" = x86_64 ] || { echo 'This release requires x86-64.' >&2; exit 1; }
  [ "$(id -u)" != 0 ] || { echo 'Run without sudo.' >&2; exit 1; }
  for tool in curl sha256sum tar; do
    command -v "$tool" >/dev/null || { echo "Missing command: $tool" >&2; exit 1; }
  done
  app_dir="$HOME/.local/mygo-music-lite.app"
  data_dir="${XDG_DATA_HOME:-$HOME/.local/share}"
  case "$app_dir$data_dir" in
    *'"'*|*'%'*|*'`'*|*'$'*|*'\'*|*'
'*) echo 'Unsupported characters in installation path.' >&2; exit 1 ;;
  esac
  mkdir -p "$HOME/.local"
  work=$(mktemp -d "$HOME/.local/.mygo-install.XXXXXX")
  trap 'rm -rf "$work"' EXIT
  trap 'exit 1' HUP INT TERM
  archive=mygo-music-lite-0.4.6-linux-amd64.tar.gz
  echo 'Downloading MyGo Music Lite 0.4.6...'
  curl --fail --location --retry 3 "https://github.com/txchen/mygo-music-lite/releases/download/v0.4.6/$archive" -o "$work/$archive"
  (cd "$work" && printf '%s  %s\n' 469260cd2f8e618bc62c8e576542509df18ffeb87c20cbe90bdec398a3f6041d "$archive" | sha256sum -c -)
  mkdir "$work/app"
  tar -xzf "$work/$archive" -C "$work/app"
  [ -x "$work/app/mygo-music-lite" ]
  # Keep the old installation until its replacement is ready.
  if [ -e "$app_dir" ]; then mv "$app_dir" "$work/previous"; fi
  if ! mv "$work/app" "$app_dir"; then
    if [ -e "$work/previous" ]; then mv "$work/previous" "$app_dir"; fi
    exit 1
  fi
  mkdir -p "$data_dir/applications"
  cat > "$data_dir/applications/mygo-music-lite.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=MyGo Music Lite
Exec="$app_dir/mygo-music-lite" %U
Icon=$app_dir/mygo-music-lite.png
Categories=AudioVideo;Audio;Player;
Terminal=false
StartupWMClass=mygo-music-lite
MimeType=x-scheme-handler/mygo-music-lite;
DESKTOP
  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$data_dir/applications" || true
  fi
  echo 'Installed. Open MyGo Music Lite from your application menu.'
  echo "Or run: \"$app_dir/mygo-music-lite\""
}
main "$@"
