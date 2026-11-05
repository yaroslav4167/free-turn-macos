# FreeTurn for macOS

Native macOS desktop VPN client based on [FreeTurn Linux](https://github.com/yaroslav4167/free-turn-linux/tree/feat/linux-desktop-experience) v2.6.5 (`09ab3f9`). The shared Wails/React UI includes profiles, server setup and management, WireGuard/AmneziaWG through TURN, routing lists, traffic charts, centered notifications, and Russian/English localization.

## Download and install

Download the ZIP for your Mac from [Releases](https://github.com/yaroslav4167/free-turn-macos/releases): `arm64` for Apple Silicon or `amd64` for Intel. Requires macOS 13+. Extract the ZIP and drag **FreeTurn.app** to **Applications**. Check the archive against `checksums.txt` with `shasum -a 256`.

Initial builds are ad-hoc signed and are not notarized by Apple. After attempting to open a downloaded app, macOS may require explicit approval in System Settings → Privacy & Security → Open Anyway. No global Gatekeeper changes are needed.

The first macOS version is experimental: automated builds and tests do not replace a live VPN acceptance test against your server.

## VPN and permissions

Import a profile or add a server, including its WireGuard/AmneziaWG client configuration. On VPN connection, macOS requests administrator authorization for the bundled helper. The app interface and VPN protocol engine run without root.

The helper creates an available `utun`, configures addresses and routes, and transfers its descriptor over a private authenticated Unix socket. The relay's sockets are bound to the physical interface. Ordinary traffic, including SSH server management, follows profile routes. Full-tunnel routes are installed as two more-specific halves; the physical default routes are preserved.

DNS is a temporary SystemConfiguration session entry. Closing or losing the helper removes that entry, rather than permanently rewriting Wi-Fi DNS settings. Disconnect/quit removes routes added by the helper. A second VPN owning the default route is rejected; disconnect it before connecting FreeTurn.

The first build asks for authorization on each VPN connection. It installs no persistent privileged daemon and no third-party TUN driver.

## Desktop integration

**FT** in the menu bar opens or quits FreeTurn. Close-to-menu-bar, start at login and optional auto-connect are available in Settings. Start at login requires the app to be in `/Applications` or `~/Applications`; macOS may show it under Login Items. Captcha notifications ask for notification permission, and open verification only after a click. The in-app notification remains available if system notifications are denied.

Updates are checked in this repository and downloaded with SHA-256 verification. Quit FreeTurn and replace the entire `.app` in Applications using Finder. Automatic bundle replacement is disabled in this initial version.

Profiles and preferences are stored in `~/Library/Application Support/FreeTurnDesktop`. To uninstall, disable start at login in Settings, quit FreeTurn, and move its bundle to Trash. Removing the app retains profiles.

## Build

Install Xcode Command Line Tools, the Go version in `go.mod`, and Node.js 22.18+.

```sh
UPDATE_REPOSITORY=yaroslav4167/free-turn-macos RELEASE_TAG=v0.1.0 sh packaging/macos/build-desktop.sh
```

The script builds Wails v2.16.0, packages the native VPN helper inside the app, ad-hoc signs the bundle, verifies its signature, and produces a ZIP and SHA-256 in `dist/`. `MACOS_ARCH=arm64` or `MACOS_ARCH=amd64` selects the architecture. GitHub Actions builds and tests each architecture on its own macOS runner.

```sh
npm --prefix desktop/frontend ci
npm --prefix desktop/frontend test
npm --prefix desktop/frontend run build
go test ./desktop/... ./internal/session ./mobile
go test -race ./desktop/backend/... ./desktop/integration ./desktop/updates
go vet ./desktop/... ./cmd/macos-vpn-helper
```

Core sources and Linux packaging remain for upstream compatibility. No Apple Developer ID credentials or notarization service are configured.

## Shared interface examples

The inherited screenshots illustrate the shared UI; they are Linux captures, not macOS screenshots.

![Connection](docs/screenshots/connection.png)
![Servers](docs/screenshots/servers.png)
![Settings](docs/screenshots/settings.png)

## License

See [LICENSE](LICENSE). Upstream attribution and commit history are retained.
