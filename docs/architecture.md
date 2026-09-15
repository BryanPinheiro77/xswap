# Architecture

XSwap is a Go executable with no third-party runtime libraries. It targets
macOS and Linux and uses POSIX terminal control and advisory file locks.

| File | Responsibility |
| --- | --- |
| `main.go` | CLI commands, installation, and wrapper restoration |
| `storage.go` | Profiles, config, private atomic writes, and file locks |
| `rpc.go` | Official Codex process execution and app-server quota queries |
| `auto.go` | Background monitor, quota scoring, and rotation decisions |
| `update.go` | GitHub release checks, archive validation, and atomic executable updates |
| `tui.go` | Account menu, watch screen, management, and auto-switch view |
| `swap_test.go` | Behavioral tests with isolated data and a fake app server |

## Account isolation

`default` refers to `~/.codex`. Each named account has a distinct home at
`~/.codex-swap/profiles/NAME`. Creation copies the original config and links the
skills directory; credentials, sessions, caches, and databases are not copied.
Existing named profiles remain compatible across manager upgrades.

The installer replaces command symlinks in `~/.local/bin`. The `codex` entry
executes the original CLI with the selected home, retaining normal arguments and
exit behavior. An explicitly exported `CODEX_HOME` takes priority. `xswap run
NAME` always uses the requested profile. The original package is not modified.

## Quota queries

For each account, the manager starts `codex app-server --stdio`, completes the
initialize handshake, reads account identity, and calls
`account/rateLimits/read`. It does not start a thread or send a prompt.
The request context bounds lock wait and query duration. Cancellation terminates
the whole app-server process group and waits for cleanup.

## Automatic rotation

Auto-switch is disabled initially. Enabling it starts a detached monitor with a
singleton file lock. The monitor polls enabled accounts and scores the main Codex
bucket by the greater usage of its short and weekly windows. Reserve/model buckets
are displayed but do not drive rotation in this version.

Once active-account usage reaches the configured threshold, a candidate must be
below that threshold and improve usage by at least five percentage points. The
lowest usage wins; name order breaks ties. A five-minute cooldown prevents rapid
rotation, including immediately after a manual selection.

Read errors, expired windows, missing quotas, stale snapshots, API-key auth, and
disabled accounts cannot become candidates. A disabled active account holds its
selection. The active account and settings are rechecked under a state lock before
commit, so concurrent manual selection or turning auto-switch off wins.

The monitor continues after closing the panel. After a reboot, starting `xswap`
or `codex` restarts an enabled monitor; there is no installed login service.

## Future providers

Claude support is outside the initial scope. A future provider interface should
separate credential storage, quota queries, and execution without weakening
Codex isolation. Keep shared UI and rotation policy independent from provider
OAuth details. Do not route unknown provider credentials through Codex commands.
