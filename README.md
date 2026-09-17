# XSwap

**Account switcher for Codex.**

A terminal account manager for the official OpenAI Codex CLI, written in Go.
Use `xswap` to manage isolated accounts, watch quotas, and select the account
used by newly launched Codex processes.

## Features

- English account menu with thin quota bars and reset timing.
- Browser or device-code login in a separate home for each account.
- Manual selection and parallel runs without copying tokens between accounts.
- Live quota watching through the official Codex app server.
- Opt-in background auto-switch with fresh-quota checks and a cooldown.
- Enable/disable controls and account removal with local archival.
- One Go executable, with no Python or third-party Go runtime dependencies.

XSwap supports **Codex on macOS, Linux, and Windows**. Claude support is a
future improvement. This is an independent community project, not an official
OpenAI or Anthropic product. `codex-swap` remains a compatibility alias.

## Install

Prerequisite: the official Codex CLI. Building from source also requires Go
1.26+; release binaries do not require Go. On macOS/Linux, add `~/.local/bin`
to your `PATH`.

Recommended on macOS or Linux with Homebrew:

```sh
brew install BryanPinheiro77/tap/xswap
xswap install
xswap
```

Homebrew manages the installed files. XSwap still detects new releases and shows
**Update version…**; after confirmation it refreshes Homebrew and runs
`brew upgrade xswap`. Before removing the formula, run `xswap uninstall` to
restore the original Codex command, followed by `brew uninstall xswap`.

From a source checkout on macOS/Linux:

```sh
make install
xswap
```

On Windows PowerShell:

```powershell
go build -trimpath -o xswap.exe ./cmd/xswap
.\xswap.exe install
```

The installer creates the `xswap`, `codex-swap`, and `codex` commands using
platform-specific links or wrappers. The `codex` command calls your original CLI
with the selected home; it does not modify the Codex package. Your existing login
stays available as `default`.

[GitHub Releases](https://github.com/BryanPinheiro77/xswap/releases) provide
macOS, Linux, and Windows archives for arm64/amd64. On macOS/Linux, extract the
matching archive into a permanent directory, run `./xswap install`, and keep the
executable there. On Windows, extract the `.zip`, run `.\xswap.exe install` in
PowerShell, then open a new terminal. The installer adds
`%LOCALAPPDATA%\XSwap\bin` to your user `PATH`.

## Quick start

```sh
xswap add                     # Creates account-1, account-2, etc. and opens login
xswap add work                # Or choose your own account name
xswap add work --label Work   # Optional private-friendly display name
xswap rename work --label Personal
xswap switch work
codex

xswap watch                   # Watch every account
xswap limits --all            # One-time quota query
xswap run default -- --version
```

No prior login or logout is needed: `add` logs in directly inside a new profile.
The menu offers switching, watching, auto-switch status, adding, enable/disable,
removal, theme, and quit. Arrow keys navigate; Enter selects; Esc goes back.

Switching accounts changes the account used by new Codex processes. Existing
Codex sessions keep the account they started with, so close and reopen a session
after switching accounts. A future `Restart with another account` flow is tracked
in [issue #27](https://github.com/BryanPinheiro77/xswap/issues/27).

## Auto-switch

```sh
xswap auto on
xswap auto on --threshold 80
xswap auto view
xswap auto --once --dry-run
xswap auto off
```

Auto-switch starts **off**. At the default 90% usage threshold, it selects an
eligible account with the most remaining main Codex quota. It requires five
percentage points of improvement and a five-minute cooldown. Disabled accounts,
stale or missing quotas, expired windows, and failed reads cannot become targets.

**Automatic and manual switching affect newly started Codex processes. Running
conversations remain on their original account.** The monitor continues after
the panel closes. After reboot, opening `xswap` or `codex` restarts it if enabled.

## Account controls

```sh
xswap disable work            # Exclude from automatic rotation
xswap enable work             # Include again
xswap remove work             # Confirm removal and archive local data
```

Disabling keeps credentials, history, and manual selection available. Disabling
the active account holds its selection during automatic checks. Removal moves
the profile to a private local archive; it does not erase credentials.
Switch away before removing the active account. `default` is protected.

## Data and compatibility

State lives in `~/.codex-swap`. `default` uses `~/.codex`; named accounts use
isolated `profiles/NAME` directories. Config is copied at creation, skills are
linked, and history/caches remain separate. Later config changes and plugin
installations are not synchronized. Existing local prototype profiles are preserved.

An explicitly exported `CODEX_HOME` overrides the `codex` wrapper's selection.
`xswap run NAME` always uses that profile. Do not share manager data: it contains
credentials and private account information.

Run `xswap install` if a Codex update overwrites the wrapper.
`xswap uninstall` stops auto-switch and restores the original CLI without
deleting profiles. Claude is not supported in this version.

## Development

```sh
make check
make build
xswap version
```

Tests use temporary homes and a fake app server, not real credentials.

- [Usage](docs/usage.md)
- [Architecture](docs/architecture.md)
- [Contributing](.github/CONTRIBUTING.md)
- [Security policy](.github/SECURITY.md)
- [Code of conduct](.github/CODE_OF_CONDUCT.md)
- [Changelog](CHANGELOG.md)
- [Release process](docs/releasing.md)
- [GitHub setup and branch protections](docs/github-setup.md)

The interface and primary documentation use English. Brazilian Portuguese
documentation will be added after repository publication.

Licensed under [MIT](LICENSE).

## Updates

GitHub releases provide macOS, Linux, and Windows binaries for ARM64 and AMD64.

Run `xswap version` to see the installed version. XSwap checks published stable
releases in the background when the panel opens, with a 15-minute cache.
**Update version…** appears only when a newer release is available. Downloads
and installation require your confirmation; updates are never installed silently.

```sh
xswap update --check
xswap update
```

Published release binaries include their GitHub repository. Before the first
release, source builds can configure it with
`xswap update --repo OWNER/xswap --check`. No update is available until a stable
release has been published. Offline checks leave the update menu hidden.

For Homebrew installations, the same menu action confirms and runs `brew update`
followed by `brew upgrade xswap`, then restarts XSwap through its stable Homebrew
path. Standalone installations validate SHA-256 checksums and platform
compatibility before replacing the executable safely. Account profiles and settings
are preserved. For standalone installations, the previous executable is saved at
`~/.codex-swap/previous-xswap`. Updates replace the binary, not a source
checkout. Source developers can rerun the platform installation command above.
Existing processes continue running their original executable; restart an enabled
auto-switch monitor with `xswap auto off` followed by `xswap auto on` after updating.
