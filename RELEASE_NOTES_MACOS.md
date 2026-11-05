## v0.2.1 — one-time VPN authorization

- Install a persistent per-user launchd VPN helper on the first connection. Approve the administrator dialog once; subsequent VPN connections no longer invoke osascript or ask for the password, including after restarting the app or macOS.
- Keep a root-owned system copy of the helper separate from the app. Compatible interface updates retain this copy without requiring administrator approval.
- Restrict the local socket to the installing user, authenticate both peers, reject overlapping VPN sessions and release session resources on disconnect or shutdown.
- Provide explicit helper repair and removal commands. Repair/removal requires administrator approval; no password is stored or sudo policy changed.
- Test service installation, repeated non-root connections, launchd restart, removal and session cleanup on isolated Apple Silicon and Intel runners. Retain updater rollback and scoped-routing regression checks.

**From v0.2.0:** install this release from Settings > Download and install. **From v0.1.x:** quit FreeTurn and replace the entire FreeTurn.app manually. The next VPN connection asks once to install the persistent helper.

Requires macOS 13+. Choose `arm64` for Apple Silicon or `amd64` for Intel. Profiles and preferences are retained. Builds remain ad-hoc signed and are not Apple-notarized.
