## v0.1.1 — restore macOS VPN traffic

- Fix `network is unreachable` for additional TURN streams after VPN routes are installed. Preserve interface-scoped defaults through the physical gateway before activating VPN routes for ordinary traffic.
- Retain pre-existing scoped defaults; remove only helper-owned routes during disconnect. Reject connection setup if the protected physical path cannot be established.
- Propagate socket-protection failures instead of silently opening an unprotected connection.
- Add a privileged regression test on isolated Apple Silicon and Intel macOS runners. The test reproduced the v0.1.0 failure using temporary non-production routes and a UDP route lookup; it requires no VPN profile or external server.

Quit FreeTurn completely, replace the **entire FreeTurn.app** (including its bundled helper), and reconnect the VPN.

Requires macOS 13+. Choose `arm64` for Apple Silicon or `amd64` for Intel. Archives include SHA-256 checksums.

Builds remain ad-hoc signed and are **not Apple-notarized**. This release is experimental; live traffic through your specific VPS/profile still needs acceptance testing.
