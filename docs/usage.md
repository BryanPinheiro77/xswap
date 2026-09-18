# Usage

## Add and select accounts

```sh
xswap add
# Opens browser login and creates account-1, account-2, etc.
xswap add work
xswap add work --label Work
xswap rename work --label Personal
xswap list
xswap switch work
codex
xswap run account-1 -- exec "Summarize this repository"
xswap switch default
```

Do not log out of your current account first. `add` starts login in a separate
profile. Choose the intended account in the browser. If cancelled, run
`xswap login NAME`; device-code login is available with `--device-auth`.

Profiles retain their assigned names. Creating a new profile does not select it.
Global manual switching affects new Codex processes. Existing processes retain
their original account. Selection does not alter the Codex desktop application.

## Project accounts and conversation handoff

```sh
xswap project use work
xswap project current
xswap project switch account-2
xswap project clear
```

`project use` pins new Codex processes in the current repository without copying
history or restarting anything. XSwap stores the name in `.xswap-account` at the
repository root, and the nearest parent file wins. Add the file to `.gitignore`
when each contributor uses different local account names.

Choose **Switch project account…** in the panel, or run `project switch`, for a
full handoff. Before making changes, XSwap shows the source and destination,
the number of project conversations to copy, and the number of running sessions
it can restart. After confirmation it:

1. pins the destination account for the repository;
2. stops only Codex child processes supervised by the XSwap wrapper in that project;
3. copies every conversation whose recorded working directory is the repository
   or one of its descendants; and
4. resumes each managed conversation in its original terminal and working directory.

Other projects, terminals, and unmanaged processes are unchanged. Conversations
remain in the source profile and identical repeated transfers are safe. XSwap
does not copy authentication, config, cache, database, or unrelated sessions.
Sessions opened before installing a version with supervision cannot be restarted
automatically; their conversations are still copied and can be opened with
`codex resume` under the destination project account.

Use `--path DIR` to target another repository and `--yes` for a confirmed
noninteractive `project switch`. An explicit `CODEX_HOME` still takes priority
and bypasses project selection and supervision.

Display names are optional labels stored locally in XSwap settings. When set,
the terminal panel shows the label instead of the account e-mail. Clear one with
`xswap rename NAME --label ""`; accounts without a label continue to use the
e-mail returned by Codex.

## Menu and watching

Run `xswap` for the account menu. Arrow keys navigate; Enter activates the
selected item. `s` selects accounts, `w` watches, `u` opens auto-switch view,
`a` adds an account, Ctrl+T changes the theme, and `q` quits.

Choose **Rename account…** in the menu to edit an optional display name. Leave
the name empty to return to the e-mail fallback.

```sh
xswap watch
xswap watch work --interval 30
xswap watch --once
xswap limits
xswap limits work
xswap limits --all
```

The watch screen displays thin usage bars, used percentages, and reset timing.
`r` refreshes; Esc returns to the menu. Refresh defaults to 60 seconds with a
minimum of 10. Errors keep the last successful display and mark it as old.
Quota queries require a ChatGPT account and network access; API keys do not have
these subscription quota windows.

## Auto-switch

```sh
xswap auto on
xswap auto on --threshold 80 --interval 60
xswap auto status
xswap auto view
xswap auto --once --dry-run
xswap auto off
```

Auto-switch is opt-in. The default threshold is 90% usage in either the main
short-window or weekly Codex quota. It chooses another eligible account with the
most remaining quota, requires at least five percentage points of improvement,
and waits five minutes between switches. Unknown or stale quotas hold selection.

The view shows status, account eligibility, and recent switches. Press `e` to
enable or stop the monitor; `+`/`-` change the threshold. The detached monitor
continues after the view closes. Auto-switch changes **newly launched Codex
processes**, not a running conversation. Use the confirmed project-switch action
when a project's managed sessions should move immediately.

A one-shot check runs even while the persistent monitor is off. `--dry-run`
previews its decision without changing selection; temporary threshold overrides
do not change the persistent configuration.

## Disable, enable, and remove

```sh
xswap disable work
xswap enable work
xswap remove work
```

Disabled accounts remain registered, can be viewed, and can be selected manually.
They cannot be chosen automatically. Disabling the active account also holds it
in place during automatic checks.

Removal asks for confirmation, removes the account from the list, and moves its
whole profile into `~/.codex-swap/removed/NAME-TIMESTAMP`. Credentials and history
are preserved there. The active account cannot be removed until another account
is selected; `default` is protected. In scripts, `--yes` confirms removal.
To recover an archived profile, move its directory back into `profiles` under an
unused valid name. Archival is not permanent credential deletion.

## Configuration and upgrades

Manager state defaults to `~/.codex-swap`; `CODEX_SWAP_HOME` changes that location.
Explicit `CODEX_HOME` takes priority for the `codex` wrapper. Run
`unset CODEX_HOME` to return to normal manager selection.

Profiles copy `config.toml` only on creation. Subsequent config updates and plugin
installations are not synchronized. Skills are shared by symlink. Each account
has separate Codex history and runtime state.

If npm or another installer replaces the `codex` wrapper, run `xswap install`
to repair command links. `xswap uninstall` stops auto-switch and restores the
original CLI without deleting account data.

## Updating

Use `xswap version` for the installed version, `xswap update --check` for a fresh
GitHub check, and `xswap update` to install with confirmation. Automation can use
`xswap update --yes` as explicit installation approval. The home menu displays
**Update version…** only when its background check detects a newer stable release.
Drafts and prereleases are excluded. Failed checks do not show an update.

Checks are cached for 15 minutes. The CLI check always refreshes that cache.
Source builds need `xswap update --repo OWNER/xswap --check` once; release binaries
embed the repository. Updates support macOS, Linux, and Windows on arm64 and
amd64. Account data is preserved. Restart existing XSwap processes and
an enabled auto-switch daemon after a CLI update.

Homebrew installations use the same update check and panel action. After
confirmation, XSwap runs `brew update` and `brew upgrade xswap`, then restarts
through Homebrew's stable executable path. Standalone installations use XSwap's
checksum-validated binary updater.

On Windows, extract the release `.zip`, run `.\xswap.exe install` from
PowerShell, and open a new terminal after the installer adds the XSwap command
directory to your user `PATH`. The updater activates a versioned executable so
it never needs to overwrite the running `.exe`.
