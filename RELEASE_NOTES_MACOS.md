## v0.2.0 — in-app macOS updates

- Automatically check for new releases and install them from Settings after confirmation.
- Verify the download checksum, native architecture, app identity and complete bundle signature before installation.
- Disconnect the VPN and atomically replace the entire app, including its bundled helper, then restart.
- Restore the previous version if the new interface fails to start. Preserve profiles and preferences; retain the previous app and update logs.
- Test installation and automatic rollback with signed app bundles and real updater processes on Apple Silicon and Intel. Validate the packaged release ZIP on both architectures.
- Includes the v0.1.1 fix for TURN traffic after VPN route setup.

**Install v0.2.0 manually once** to enable future in-app installations: quit FreeTurn and replace the entire FreeTurn.app. Keep it in `/Applications` or `~/Applications`, with write access for your account. Installation requires confirmation; updates are not installed silently.

Requires macOS 13+. Choose `arm64` for Apple Silicon or `amd64` for Intel. Archives include SHA-256 checksums.

Builds remain ad-hoc signed and are not Apple-notarized.
