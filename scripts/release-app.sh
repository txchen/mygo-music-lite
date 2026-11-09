#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."

usage() {
  echo 'Usage: scripts/release-app.sh prepare VERSION SIGNING_IDENTITY KEYCHAIN' >&2
  echo '       scripts/release-app.sh notarize VERSION NOTARY_PROFILE KEYCHAIN' >&2
  exit 1
}
[[ $# == 4 ]] || usage
stage=$1
version=$2
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
[[ $(uname -s) == Darwin && $(uname -m) == arm64 ]] || { echo 'Requires macOS Apple Silicon.' >&2; exit 1; }
release="$PWD/build/release/$version"
app='MyGo Music Lite.app'
submission="$release/submission.zip"
archive="$release/mygo-music-lite-$version-darwin-arm64.zip"
keychain=$4

case "$stage" in
  prepare)
    [[ ! -e $release ]] || { echo "Release directory already exists: $release" >&2; exit 1; }
    [[ -z $(git status --porcelain) ]] || { echo 'Commit all source changes before preparing a release.' >&2; exit 1; }
    scripts/build-audio.sh
    GOFLAGS='' MACOSX_DEPLOYMENT_TARGET=13.0 CGO_ENABLED=1 \
      CGO_CFLAGS='-mmacosx-version-min=13.0' \
      CGO_CXXFLAGS='-mmacosx-version-min=13.0' \
      CGO_LDFLAGS='-mmacosx-version-min=13.0' go tool mygo build -platform darwin/arm64 -skip-dmg
    built="$PWD/build/darwin-arm64/$app"
    [[ $(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$built/Contents/Info.plist") == "$version" ]]
    [[ $(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$built/Contents/Info.plist") == dev.txchen.mygo-music-lite ]]
    executable=$(/usr/libexec/PlistBuddy -c 'Print CFBundleExecutable' "$built/Contents/Info.plist")
    minos=$(otool -l "$built/Contents/MacOS/$executable" | awk '$1 == "minos" {print $2}')
    [[ $minos == 13.0 ]] || { echo "Expected Mach-O minos 13.0, got: $minos" >&2; exit 1; }
    [[ $(lipo -archs "$built/Contents/MacOS/$executable") == arm64 ]]
    mkdir -p "$release"
    git rev-parse HEAD > "$release/source-commit.txt"
    ditto "$built" "$release/$app"
    mkdir -p "$release/$app/Contents/Resources/Licenses"
    cp LICENSE THIRD_PARTY.md LICENSES/* "$release/$app/Contents/Resources/Licenses/"
    cat > "$release/entitlements.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>com.apple.security.cs.disable-library-validation</key><true/>
</dict></plist>
PLIST
    codesign --force --options runtime --timestamp --sign "$3" --keychain "$keychain" \
      --entitlements "$release/entitlements.plist" "$release/$app"
    codesign --verify --deep --strict --verbose=2 "$release/$app"
    codesign --display --verbose=4 "$release/$app" 2> "$release/signature.txt"
    grep -q '^Authority=Developer ID Application:' "$release/signature.txt"
    grep -q 'flags=.*runtime' "$release/signature.txt"
    grep -q '^Timestamp=' "$release/signature.txt"
    ! grep -q '^Timestamp=none' "$release/signature.txt"
    ditto -c -k --sequesterRsrc --keepParent "$release/$app" "$submission"
    (cd "$release" && shasum -a 256 submission.zip > submission.zip.sha256)
    echo "Prepared $submission"
    ;;
  notarize)
    (cd "$release" && shasum -a 256 -c submission.zip.sha256)
    if [[ -e $archive || -e $archive.sha256 ]]; then
      [[ -f $archive && -f $archive.sha256 ]] || { echo 'Incomplete final archive; inspect it before continuing.' >&2; exit 1; }
      (cd "$release" && shasum -a 256 -c "$(basename "$archive").sha256")
      echo "Final archive already exists: $archive"
      exit 0
    fi
    if [[ ! -f $release/submission.json ]]; then
      xcrun notarytool submit "$submission" --keychain-profile "$3" --keychain "$keychain" \
        --output-format json > "$release/submission.json.tmp"
      plutil -extract id raw -o - "$release/submission.json.tmp" >/dev/null
      mv "$release/submission.json.tmp" "$release/submission.json"
    fi
    id=$(plutil -extract id raw -o - "$release/submission.json")
    xcrun notarytool wait "$id" --keychain-profile "$3" --keychain "$keychain" \
      --output-format json > "$release/notarization.json.tmp"
    mv "$release/notarization.json.tmp" "$release/notarization.json"
    xcrun notarytool log "$id" --keychain-profile "$3" --keychain "$keychain" "$release/notarization-log.json"
    [[ $(plutil -extract jobId raw -o - "$release/notarization-log.json") == "$id" ]]
    expected_sha=$(shasum -a 256 "$submission" | awk '{print $1}')
    [[ $(plutil -extract sha256 raw -o - "$release/notarization-log.json") == "$expected_sha" ]]
    [[ $(plutil -extract status raw -o - "$release/notarization.json") == Accepted ]] || {
      echo 'Notarization was not accepted. See notarization-log.json.' >&2
      exit 1
    }
    staging=$(mktemp -d "$release/staple.XXXXXX")
    trap 'rm -rf "$staging"' EXIT
    ditto -x -k "$submission" "$staging"
    codesign --verify --deep --strict --verbose=2 "$staging/$app"
    xcrun stapler staple "$staging/$app"
    xcrun stapler validate "$staging/$app"
    spctl --assess --type execute --verbose=2 "$staging/$app"
    codesign --verify --deep --strict --verbose=2 "$staging/$app"
    ditto -c -k --sequesterRsrc --keepParent "$staging/$app" "$staging/final.zip"
    # Hard-link creation refuses an existing destination, even if another run creates it.
    ln "$staging/final.zip" "$archive"
    (cd "$release" && shasum -a 256 "$(basename "$archive")" > "$(basename "$archive").sha256")
    echo "Ready to publish: $archive"
    ;;
  *) usage ;;
esac
