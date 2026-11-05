## v0.2.2 — fix VPN initialization timeouts and stale sessions

- Correct the launchd process classification: VPN setup is user-requested work and must not run with Background CPU/I/O throttling. Reproduced long delays on a busy macOS 26 machine; Interactive scheduling restores prompt setup.
- Keep only a bare utun descriptor in the privileged helper. Preserve its nonblocking mode during transfer; the application owns the WireGuard event listener.

- Allow slower macOS utun initialization: the client waits up to 60 seconds, while helper setup has a separate 45-second budget. The initial request read deadline no longer cancels valid setup after 15 seconds.
- Detect client disconnect during initialization and cancel in-flight network commands. Cancel setup when the user disconnects, and release the session after resource cleanup so the next attempt is not blocked by an abandoned connection.
- Limit route application and cleanup separately, preserving cleanup even when the original session context is cancelled.
- Check the installed system helper protocol. Upgrade the older v0.2.1 helper once so replacing the app also activates the fix in the persistent service.
- Add regression checks for setup lasting longer than 15 seconds, cancellation of preparation and commands, slot reuse, and real IPv4/dual-stack utun sessions on Apple Silicon and Intel.

**Update from Settings > Download and install.** On the next connection, approve the one-time system-helper upgrade. Subsequent connections reuse it without another password prompt. A manual service repair is not required.

Requires macOS 13+. Choose `arm64` for Apple Silicon or `amd64` for Intel. Profiles and preferences are retained. Builds remain ad-hoc signed and are not Apple-notarized.
