# FreeTurn for macOS

Native macOS desktop VPN client based on [FreeTurn Linux](https://github.com/yaroslav4167/free-turn-linux/tree/feat/linux-desktop-experience) v2.6.5 (`09ab3f9`). The shared Wails/React UI includes profiles, server setup and management, WireGuard/AmneziaWG through TURN, routing lists, traffic charts, centered notifications, and Russian/English localization.

## Download and install

Download the ZIP for your Mac from [Releases](https://github.com/yaroslav4167/free-turn-macos/releases): `arm64` for Apple Silicon or `amd64` for Intel. Requires macOS 13+. Extract the ZIP and drag **FreeTurn.app** to **Applications**. Check the archive against `checksums.txt` with `shasum -a 256`.

Initial builds are ad-hoc signed and are not notarized by Apple. After attempting to open a downloaded app, macOS may require explicit approval in System Settings → Privacy & Security → Open Anyway. No global Gatekeeper changes are needed.

The first macOS version is experimental: automated builds and tests do not replace a live VPN acceptance test against your server.

## VPN and permissions

Import a profile or add a server, including its WireGuard/AmneziaWG client configuration. On the first VPN connection, macOS requests administrator authorization to install a persistent, per-user VPN helper. Subsequent connections use the installed service without another password prompt, including after restarting FreeTurn or macOS. The app interface and VPN protocol engine run without root.

The helper creates an available `utun`, configures addresses and routes, and transfers its descriptor over a private authenticated Unix socket. The relay's sockets are bound to the physical interface. Before enabling VPN routes, the helper ensures a scoped default through the physical gateway for those sockets; existing scoped defaults are retained and only helper-owned routes are removed on disconnect. Ordinary traffic, including SSH server management, follows profile routes. Full-tunnel routes are installed as two more-specific halves; the physical default routes are preserved.

DNS is a temporary SystemConfiguration session entry. Closing or losing the helper removes that entry, rather than permanently rewriting Wi-Fi DNS settings. Disconnect/quit removes routes added by the helper. A second VPN owning the default route is rejected; disconnect it before connecting FreeTurn.

The root-owned helper is copied into `/Library/PrivilegedHelperTools/org.freeturn.vpn.<UID>` and managed by a per-user plist in `/Library/LaunchDaemons`. Only the installing user can connect through its private Unix socket; no executable from a user-writable app folder is run as root on subsequent connections. There is no password storage or change to sudo authorization. The service remains idle without an active VPN session, and disconnect releases session routes/DNS. GUI updates retain the installed helper while its protocol remains compatible; an explicit helper repair or upgrade requires administrator approval.

To repair the helper, disconnect VPN and run the app executable with `--configure-vpn-helper`. To remove the service before deleting the app:

```sh
"/Applications/FreeTurn.app/Contents/MacOS/freeturn-desktop" --remove-vpn-helper
```

Removal requests administrator approval and revokes only this user's helper access. No third-party TUN driver is installed.

## Desktop integration

**FT** in the menu bar opens or quits FreeTurn. Close-to-menu-bar, start at login and optional auto-connect are available in Settings. Start at login requires the app to be in `/Applications` or `~/Applications`; macOS may show it under Login Items. Captcha notifications ask for notification permission, and open verification only after a click. The in-app notification remains available if system notifications are denied.

FreeTurn checks this repository for updates automatically. In Settings, choose **Download and install** and confirm the restart. The updater verifies the SHA-256 checksum, native architecture and complete bundle signature, disconnects the VPN, replaces the entire app including its helper, and restarts it. If the new interface fails to start, the previous version is restored. Profiles and preferences are retained.

When upgrading from v0.1.x, install v0.2.1 manually once to enable in-app installation. v0.2.0 can install this release from Settings. Keep FreeTurn.app in `/Applications` or `~/Applications`; your account must be able to write to that folder. If a managed Applications folder is protected, use `~/Applications` or install manually. Installation always requires confirmation. The previous bundle and update logs are retained in a private `.freeturn-mac-update-*` folder beside the app; its location is included in the update result.

Profiles and preferences are stored in `~/Library/Application Support/FreeTurnDesktop`. To uninstall, disconnect VPN, remove the helper as described above, disable start at login in Settings, quit FreeTurn, and move its bundle to Trash. Removing the app retains profiles.

## Build

Install Xcode Command Line Tools, the Go version in `go.mod`, and Node.js 22.18+.

```sh
UPDATE_REPOSITORY=yaroslav4167/free-turn-macos RELEASE_TAG=v0.2.1 sh packaging/macos/build-desktop.sh
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
