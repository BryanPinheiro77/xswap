# Architecture

XSwap is a Go executable with no third-party runtime libraries. It targets
macOS, Linux, and Windows. Terminal and process operations use platform-specific
Go implementations, and state changes use portable lock directories.

Source files and their tests live together in `cmd/xswap/`. The root contains
build configuration and the project entry documents; community policies live
in `.github/` and longer guides in `docs/`.

| File in `cmd/xswap/` | Responsibility |
| --- | --- |
| `main.go` | CLI commands, installation, and wrapper restoration |
| `storage.go` | Profiles, config, private atomic writes, and file locks |
| `rpc.go` | Official Codex process execution and app-server quota queries |
| `auto.go` | Background monitor, quota scoring, and rotation decisions |
| `update.go` | GitHub release checks, archive validation, and atomic executable updates |
| `install_unix.go`, `install_windows.go` | Platform command installation and removal |
| `process_unix.go`, `process_windows.go` | Platform process and terminal primitives |
| `project.go` | Project-root discovery and local account selection |
| `session.go` | Validated project-session discovery, copying, and Codex indexing |
| `supervisor.go` | Managed Codex lifecycle and confirmed project handoff |
| `tui.go` | Account menu, watch screen, management, and auto-switch view |
| `swap_test.go` | Behavioral tests with isolated data and a fake app server |

## Account isolation

`default` refers to `~/.codex`. Each named account has a distinct home at
`~/.codex-swap/profiles/NAME`. Creation copies the original config and links the
skills directory; credentials, sessions, caches, and databases are not copied.
Existing named profiles remain compatible across manager upgrades.

On macOS/Linux, the installer replaces command symlinks in `~/.local/bin`. On
Windows, it creates owned `.cmd` wrappers in `%LOCALAPPDATA%\XSwap\bin` and adds
that directory to the user `PATH`. The `codex` entry
executes the original CLI with the selected home, retaining normal arguments and
exit behavior. An explicitly exported `CODEX_HOME` takes priority. `xswap run
NAME` always uses the requested profile. The original package is not modified.

Windows updates place each release in `%LOCALAPPDATA%\XSwap\app` and atomically
redirect the owned wrappers. This avoids replacing an executable while Windows
is still running it. Release archives and executable formats are checked against
the current operating system and architecture before activation.

## Project handoff

A `.xswap-account` file selects an account for its directory tree. The Codex
wrapper resolves the nearest file before launch; an explicit `CODEX_HOME` keeps
precedence. Each wrapper process supervises only the Codex child it started and
publishes private runtime metadata under the XSwap state directory.

The session-continuation action requires confirmation. It first validates and
copies a safe snapshot of every selected conversation, then writes a handoff
request for each live managed child in the selected project. It signals only the
Codex child process, which stays in the terminal's foreground process group, and
lets the original wrapper fast-forward and resume the matching conversation in
the same terminal and working directory. The coordinator performs a final sync.
Before confirmation, XSwap probes Codex's per-thread writer locks. A held lock
without a matching live XSwap supervisor identifies an open unmanaged
conversation; the handoff is blocked and the panel names it so the user can
close it and reopen it through `codex resume`. This prevents the source and
destination histories from accepting new messages independently.

Repository and directory scopes use `.xswap-account`. Git repositories receive
a local `/.xswap-account` rule in `.git/info/exclude` before the pin is written;
linked worktrees resolve their common Git metadata directory. A handoff rooted
at the user home changes the global account for unpinned directories without
creating a home-wide project file. Filesystem-root handoffs are rejected.

Transfers parse the rollout's `session_meta`, require a UUID and an absolute
working directory inside the project, reject symlinks and path traversal, and
copy one JSONL file atomically. Rollouts are treated as append-only: an older
identical prefix is safely advanced in either account, while histories that
changed independently are rejected as divergent. Managed sessions register
themselves when the wrapper resumes them. XSwap registers copied inactive
sessions through Codex's `thread/resume` app-server request so they appear in
the destination picker. Authentication, configuration, caches, and profile
databases are never copied.

## Quota queries

For each account, the manager starts `codex app-server --stdio`, completes the
initialize handshake, reads account identity, and calls
`account/rateLimits/read`. It does not start a thread or send a prompt.
The request context bounds lock wait and query duration. Cancellation terminates
the whole app-server process group and waits for cleanup.

Quota responses are accepted only when they contain complete, finite usage
windows. A hollow or partial response retains the last valid in-memory reading
as stale instead of replacing it or refreshing its timestamp. The panel labels
valid, stale, and unavailable readings explicitly; stale readings never qualify
for automatic rotation.

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

## Package-managed installations

The Homebrew formula runs the release binary through a stable wrapper under
Homebrew's `opt` prefix. `xswap install` points its user-level Codex links at that
stable path, so Cellar version directories can change during upgrades without
breaking commands. The wrapper identifies Homebrew to XSwap through restricted
process environment metadata.

Package-managed builds use the same release discovery and panel action as
standalone builds. After confirmation, Homebrew installations refresh the tap
and run `brew upgrade xswap`, keeping package ownership and rollback state
consistent. Standalone release archives retain XSwap's confirmed,
checksum-validated binary updater.

## Future providers

Claude support is outside the initial scope. A future provider interface should
separate credential storage, quota queries, and execution without weakening
Codex isolation. Keep shared UI and rotation policy independent from provider
OAuth details. Do not route unknown provider credentials through Codex commands.
