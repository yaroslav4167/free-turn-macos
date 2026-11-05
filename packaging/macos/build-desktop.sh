#!/bin/sh
# Build a native, ad-hoc-signed bundle with its privileged VPN helper.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"
[ "$(uname -s)" = Darwin ] || { echo 'Build on macOS with Xcode Command Line Tools.' >&2; exit 1; }
arch=${MACOS_ARCH:-$(go env GOARCH)}
case "$arch" in arm64|amd64) ;; *) echo 'MACOS_ARCH must be arm64 or amd64' >&2; exit 1;; esac
version=${RELEASE_TAG:-${VERSION:-dev}}
repository=${UPDATE_REPOSITORY:-yaroslav4167/free-turn-macos}
case "$version" in ''|*[!a-zA-Z0-9._+-]*) echo 'Invalid release version' >&2; exit 1;; esac
case "$repository" in *[!a-zA-Z0-9_./-]*) echo 'Invalid update repository' >&2; exit 1;; esac
export MACOSX_DEPLOYMENT_TARGET=13.0
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
gobin=$(go env GOBIN)
[ -n "$gobin" ] || gobin="$(go env GOPATH)/bin"
npm --prefix desktop/frontend ci
(
 cd desktop
 "$gobin/wails" build -platform "darwin/$arch" -ldflags "-checklinkname=0 -s -w -X main.desktopVersion=$version -X main.updateRepository=$repository" -trimpath
)
# Wails names the bundle after the configured project name.
bundle="$root/desktop/build/bin/FreeTurn.app"
[ -d "$bundle" ] || { echo "Missing Wails app bundle: $bundle" >&2; exit 1; }
mkdir -p dist
stage=$(mktemp -d "$root/dist/.macos-package.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
trap 'exit 1' HUP INT TERM
/usr/bin/ditto "$bundle" "$stage/FreeTurn.app"
CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags '-s -w -checklinkname=0' -o "$stage/FreeTurn.app/Contents/MacOS/freeturn-vpn-helper" ./cmd/macos-vpn-helper
# Version metadata matches the updater's release tag.
plist="$stage/FreeTurn.app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${version#v}" "$plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${version#v}" "$plist"
/usr/bin/plutil -lint "$plist"
cp LICENSE "$stage/FreeTurn.app/Contents/Resources/LICENSE"
cp packaging/macos/INSTALL.txt "$stage/FreeTurn.app/Contents/Resources/INSTALL.txt"
identity=${MACOS_SIGN_IDENTITY:--}
/usr/bin/codesign --force --sign "$identity" --timestamp=none "$stage/FreeTurn.app/Contents/MacOS/freeturn-vpn-helper"
/usr/bin/codesign --force --sign "$identity" --timestamp=none "$stage/FreeTurn.app"
/usr/bin/codesign --verify --deep --strict "$stage/FreeTurn.app"
archive="FreeTurn-macOS-$arch-$version.zip"
/usr/bin/ditto -c -k --sequesterRsrc --keepParent "$stage/FreeTurn.app" "$root/dist/$archive"
(cd dist && shasum -a 256 "$archive" > "$archive.sha256")
echo "Built: $root/dist/$archive"
