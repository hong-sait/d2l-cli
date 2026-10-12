# M3 — Bash CLI integration

M3 adds `d2l status` and `d2l whoami`. The CLI uses a per-user local IPC endpoint to ask the native host, which forwards one fixed request over the already-authorized Native Messaging connection to the extension. The extension makes the tested Brightspace `whoami` GET request and returns only `Identifier` and `UniqueName`.

```text
d2l whoami -> protected per-user IPC -> native host
            -> Chrome/Edge Native Messaging -> extension service worker
            -> GET /d2l/api/lp/1.63/users/whoami
```

## Security and lifecycle

- On macOS and Linux, the endpoint is a Unix socket under the current user's OS configuration directory at `d2l-cli/bridge.sock`; its directory is mode 0700 and the socket is mode 0600. Symlink, wrong-owner, overly broad directory, and unexpected existing socket paths are rejected.
- On Windows, the endpoint is a named pipe derived from the current user's configuration directory. It rejects remote clients and uses the Windows default security descriptor for the current user. A per-user mutex ensures that only one browser host owns the CLI endpoint at a time.
- The local protocol uses bounded, length-prefixed JSON and only allows `ping` and `whoami` with empty parameter objects. Active requests are correlated by ID and capped at 32.
- The extension uses only the fixed Brightspace `whoami` GET route. It returns only `Identifier` and `UniqueName`; it never sends cookies, tokens, arbitrary URLs, or the full response body to the CLI.
- The extension opens a persistent native connection on service-worker startup and reconnects with bounded backoff after disconnects. When the browser exits, the host and socket stop; `d2l status` reports disconnected and `d2l whoami` exits nonzero with an actionable message.
- Chrome and Edge can each keep a host connected. One host owns the per-user IPC endpoint; another waits for ownership. Use the browser session you want the CLI to use.

## Install updated M3 files on macOS Chrome

The repository installer builds the native host and CLI with the extension ID embedded. A fresh install refuses to overwrite an existing `~/.local/bin/d2l` command. For an existing binary built by this project and registered to this extension ID, use `--upgrade`; the installer verifies that provenance and saves timestamped backups before replacing the files:

```sh
./installers/install-chrome-native-host.sh pmoonbhnkggdeollkfmnpnhjkkoioehc
# Existing managed installation:
./installers/install-chrome-native-host.sh pmoonbhnkggdeollkfmnpnhjkkoioehc --upgrade
```

Reload the unpacked extension and restart Chrome. The registered native host stays at `~/.local/bin/d2l-native-host`; the CLI command is `~/.local/bin/d2l`.

## Install on Windows

From PowerShell at the repository root, build the CLI with `go build -o .\d2l.exe .`. Load the unpacked `extension/` directory in Chrome or Edge, copy the extension ID from each browser, then register one or both hosts:

```powershell
.\installers\install-windows-native-host.ps1 `
  -ChromeExtensionId YOUR_CHROME_EXTENSION_ID `
  -EdgeExtensionId YOUR_EDGE_EXTENSION_ID
```

The installer rebuilds the CLI and separate extension-ID-bound native hosts, places them under `%LOCALAPPDATA%\Programs\BrightspaceCLI`, and registers per-user Native Messaging manifests in the selected browser registry hives. Add `-Upgrade` only for an existing installation managed by this script. Close the browsers before upgrading.

Restart the selected browser, sign in to Brightspace, and use **Test Native Messaging**. Run the CLI with `& "$env:LOCALAPPDATA\Programs\BrightspaceCLI\d2l.exe" status`.

## Manual acceptance check

1. With Chrome open and signed in to Brightspace, run `d2l status`; expect `Browser bridge: connected`.
2. Run `d2l whoami`; expect the account identifier and unique name, with no other profile fields.
3. Sign out in Brightspace and run `d2l whoami`; expect a sign-in-required error without response-body text.
4. Close the browser, then run `d2l status` and `d2l whoami`; status must say disconnected and whoami must exit nonzero.
5. Reopen the browser and sign in normally. The extension reconnects; `d2l status` and `d2l whoami` should work again without copying credentials or restarting the CLI.

## Automated verification

`go test ./...` includes Unix-socket permission/correlation checks and a simulated extension-to-host-to-CLI round trip. Windows named-pipe behavior still requires validation on a Windows machine. `node --test extension/core.test.js` verifies that the extension returns only allowlisted identity fields and sanitizes expired-session responses. These checks do not replace the live-browser acceptance check above.

## Current state

M3 was accepted by the user on 2026-10-11. Automated checks pass. The user reported that the native host connected in Edge; a live CLI `d2l whoami` and browser restart transcript were not provided, so those outcomes are not independently recorded here. The separate M4 additions are described in `m4-read-only-operations.md`.
