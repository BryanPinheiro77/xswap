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

The first version supports **Codex on macOS and Linux**. Claude support is a
future improvement. This is an independent community project, not an official
OpenAI or Anthropic product. `codex-swap` remains a compatibility alias.

## Install

Prerequisites: the official Codex CLI and `stty` (included on macOS and common
Linux distributions). Building from source also requires Go 1.26+; release
binaries do not require Go. Add `~/.local/bin` to your `PATH`.

From the source checkout:

```sh
make install
xswap
```

The installer creates `xswap` and `codex-swap` links and adds a `codex` wrapper.
The wrapper calls your original CLI with the selected home; it does not modify
the Codex package. Your existing login stays available as `default`.

[GitHub Releases](https://github.com/BryanPinheiro77/xswap/releases) provide
macOS/Linux archives for arm64/amd64. Extract the matching archive into a permanent directory and run
`./xswap install`. Keep the executable there after installation.

## Quick start

```sh
xswap add                     # Creates account-1, account-2, etc. and opens login
xswap add work                # Or choose your own account name
xswap switch work
codex

xswap watch                   # Watch every account
xswap limits --all            # One-time quota query
xswap run default -- --version
```

No prior login or logout is needed: `add` logs in directly inside a new profile.
The menu offers switching, watching, auto-switch status, adding, enable/disable,
removal, theme, and quit. Arrow keys navigate; Enter selects; Esc goes back.

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
deleting profiles. Windows and Claude are not supported in this version.

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

GitHub releases provide macOS and Linux binaries for Apple Silicon/ARM64 and
Intel/AMD64. Windows support is planned for a later version.

Run `xswap version` to see the installed version. XSwap checks published stable
releases in the background when the panel opens, with a six-hour cache.
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

The updater validates SHA-256 checksums and platform compatibility, then replaces
the executable atomically. Account profiles and settings are preserved. The
previous executable is saved at `~/.codex-swap/previous-xswap`. Updates replace
the binary, not a source checkout. Source developers can use `make install`.
Existing processes continue running their original executable; restart an enabled
auto-switch monitor with `xswap auto off` followed by `xswap auto on` after updating.
