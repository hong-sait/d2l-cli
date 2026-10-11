# M2 — Chrome Native Messaging

M2 adds a small Go host and a popup **Test Native Messaging** action. The only accepted operation is `ping`, which returns `{status:"ready"}`. This milestone does not proxy Brightspace API requests, expose arbitrary URLs/JavaScript, or implement the M3 broker/CLI operations.

## Protocol and validation

- Each message is UTF-8 JSON preceded by a four-byte native-endian byte length, as required by Chrome Native Messaging.
- Chrome permits up to 64 MiB from extension to host and 1 MiB from host to extension. This host caps application requests and responses at 64 KiB and rejects larger frames before allocating their bodies.
- Request IDs are limited to 64 safe ASCII characters. The host rejects malformed JSON, unknown schema fields, invalid IDs, unexpected launch arguments, empty frames, truncated frames, and non-UTF-8 messages. Unsupported operations and invalid ping parameters receive bounded correlated error responses.
- Standard output is reserved for framed protocol messages. The host writes only fixed diagnostics to standard error.
- The host binary is built with the exact extension origin and checks Chrome's origin argument. The native-host manifest also has that one extension origin in `allowed_origins`; it has no wildcard.

## Register on macOS Chrome

The macOS Chrome installer installs a user-scoped `d2l-native-host` binary and writes Chrome's native-host registration file. It was run after the user approved installation, using the extension ID shown in Chrome.

The registered executable is `~/.local/bin/d2l-native-host`; the manifest is `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.sait.d2l.json`. The manifest uses an absolute executable path and allows only the exact extension origin `chrome-extension://pmoonbhnkggdeollkfmnpnhjkkoioehc/`. The script does not register Edge or Windows/Linux hosts. To register again after changing the extension ID, load the unpacked extension, copy its current ID from the extension details page, and run `./installers/install-chrome-native-host.sh <extension-id>` from the repository root.

## Verification and observed result

| Check | Result | Evidence |
|---|---|---|
| Framing, native byte order, UTF-8, malformed schema, operation allowlist, origin checks | PASS | `go test ./...` |
| Oversized inbound message rejected before body allocation; oversized response rejected | PASS | `TestServeRejectsApplicationOversizeBeforeBodyAllocation`, `TestWriteFrameEnforcesResponseCap` |
| Build and static checks | PASS | Go vet, Windows amd64 cross-build, JavaScript syntax checks, installer `sh -n`, `git diff --check` |
| Registered Chrome manifest and binary | PASS | Manifest parsed; executable path and exact extension origin allowlist verified; host exited cleanly on EOF |
| Live Chrome extension connection | PASS | User screenshot shows **“Native Messaging host connected.”** in the popup after clicking **Test Native Messaging** |
| Live Edge extension connection | PASS (user reported) | User confirmed **“Native Messaging host connected.”** after the Edge Brightspace API run. Edge's exact version and host-registration lookup path were not captured; the installer only writes the Chrome registration manifest. |

M2's Chrome Native Messaging connection and malformed/oversized-message checks are verified; the user also reports a successful Edge connection. The Edge native-host registration path was not independently verified. This is not M3: the host implements only the fixed ping and does not broker API or CLI operations. M1 remains open in `docs/m1-test-report.md` until the user explicitly accepts the milestone.
