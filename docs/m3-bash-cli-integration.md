# M3 — Bash CLI integration

M3 adds `d2l status` and `d2l whoami`. The CLI uses a local Unix-domain socket to ask the native host, which forwards one fixed request over the already-authorized Native Messaging connection to the extension. The extension makes the tested Brightspace `whoami` GET request and returns only `Identifier` and `UniqueName`.

```text
d2l whoami -> private per-user Unix socket -> native host
            -> Chrome/Edge Native Messaging -> extension service worker
            -> GET /d2l/api/lp/1.63/users/whoami
```

## Security and lifecycle

- The socket is under the current user's OS configuration directory at `d2l-cli/bridge.sock`; its directory is mode 0700 and the socket is mode 0600. Symlink, wrong-owner, overly broad directory, and unexpected existing socket paths are rejected.
- The local protocol uses bounded, length-prefixed JSON and only allows `ping` and `whoami` with empty parameter objects. Active requests are correlated by ID and capped at 32.
- The extension uses only the fixed Brightspace `whoami` GET route. It returns only `Identifier` and `UniqueName`; it never sends cookies, tokens, arbitrary URLs, or the full response body to the CLI.
- The extension opens a persistent native connection on service-worker startup and reconnects with bounded backoff after disconnects. When the browser exits, the host and socket stop; `d2l status` reports disconnected and `d2l whoami` exits nonzero with an actionable message.
- Unix-domain IPC is implemented for macOS and Linux. Windows builds still compile but report that this transport is not available there. Run one browser profile at a time for this user-scoped socket.

## Install updated M3 files on macOS Chrome

The repository installer now builds the native host and CLI with the extension ID embedded. It refuses to overwrite an existing `~/.local/bin/d2l` command. After confirming that path is available, run:

```sh
./installers/install-chrome-native-host.sh pmoonbhnkggdeollkfmnpnhjkkoioehc
```

Reload the unpacked extension and restart Chrome. The registered native host stays at `~/.local/bin/d2l-native-host`; the CLI command is `~/.local/bin/d2l`.

## Manual acceptance check

1. With Chrome open and signed in to Brightspace, run `d2l status`; expect `Browser bridge: connected`.
2. Run `d2l whoami`; expect the account identifier and unique name, with no other profile fields.
3. Sign out in Brightspace and run `d2l whoami`; expect a sign-in-required error without response-body text.
4. Close the browser, then run `d2l status` and `d2l whoami`; status must say disconnected and whoami must exit nonzero.
5. Reopen the browser and sign in normally. The extension reconnects; `d2l status` and `d2l whoami` should work again without copying credentials or restarting the CLI.

## Automated verification

`go test ./...` includes a Unix-socket permission/correlation test, a simulated extension-to-host-to-CLI `whoami` round trip, disconnected status behavior, and CLI error/output checks. `node --test extension/core.test.js` verifies that the extension returns only the two allowlisted identity fields and sanitizes expired-session responses. These tests do not replace the live-browser acceptance check above.

## Current state

Implementation and automated checks pass. The updated host/CLI binaries have not yet been installed, and the updated extension has not yet been reloaded in the browser; live `d2l whoami`, browser shutdown/restart recovery, and disconnected behavior remain pending manual acceptance.
