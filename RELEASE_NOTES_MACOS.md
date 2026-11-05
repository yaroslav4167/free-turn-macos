## FreeTurn for macOS — first native build

- Shared desktop UI from Linux v2.6.5: centered notification overlays, retained settings, compact connection view, server management, routing lists, traffic charts and Russian/English localization.
- Native macOS `utun` VPN with administrator-authorized helper, descriptor passing, physical-interface socket protection and temporary session-owned DNS.
- FT menu bar item, native captcha notifications, optional start at login and ICMP server latency.
- Separate `.app` downloads for Apple Silicon (`arm64`) and Intel (`amd64`), with SHA-256 checksums.
- Update checking and verified downloads. Quit the app and replace the entire bundle using Finder; automatic bundle replacement is disabled.

Requires macOS 13 or later. Extract the ZIP and move `FreeTurn.app` to Applications.

These builds are ad-hoc signed, **not Apple-notarized**. Gatekeeper may require approval in System Settings → Privacy & Security after the first launch attempt. No Developer ID certificate was configured.

Platform code, unit tests and native builds are checked automatically. Live VPN traffic against a real profile still requires manual acceptance on both architectures; this first release should be treated as experimental.
