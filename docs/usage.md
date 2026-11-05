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
The installed `codex` wrapper supervises new terminal sessions automatically;
you do not need to open the `xswap` panel first.

## Account health and authentication

`xswap list` stays offline. It shows the last saved health observation and its
timestamps, or a local hint when an account has not been checked yet. Use
`xswap limits NAME`, `xswap watch NAME --once`, or the panel/watch refresh to
make a bounded live query through the official Codex CLI.

| Status | Meaning |
| --- | --- |
| `original account` | The `default` profile, shown alongside its authentication observation |
| `not checked` | No authentication evidence from the latest attempt, or no observation yet; the original account may use the OS credential store |
| `login pending` | A named profile has no saved login, or Codex reports no account without a saved or previously confirmed login |
| `login saved; not verified` | A login file or account identity is present, but a successful quota response has not confirmed authentication |
| `authenticated` | Codex returned a successful quota response at the recorded check time |
| `expired or invalid login` | Codex explicitly rejected authentication or no longer reports an account despite saved or previously confirmed login; run `xswap login NAME` |
| `API key; subscription quotas unsupported` | Codex reported an API-key account; subscription quotas are unavailable |
| `unsupported account type` | Codex reported another account type without supported subscription quotas |

These are observations, not a guarantee that credentials remain usable. The
list does not revalidate credentials; inspect **Last check** before relying on
an old observation. Last successful quota query, the first failed check after
that success (**Quota data stale since**), and a safe last-error summary appear
when available. Network and timeout failures do not imply an expired login.
The panel keeps its last valid quota reading after a failed refresh and marks
it stale; stale data never qualifies for automatic rotation. A later successful
query clears the failure and stale markers. Cached health metadata contains no
quota readings, so reopening the panel requires a new query to show quota bars.

XSwap stores only status, timestamps, and fixed diagnostic categories in private
`health/NAME.json` files inside the manager directory. It never stores
credentials, e-mails, or raw server error payloads there. Cache failures do not
block quota polling or change account eligibility. Reusing a removed account
name clears its old observation; completing an explicit login resets the hint
to saved and unverified until the next query.

Health checks request `account/read` with `refreshToken: false`, never launch
login, and never request a silent credential refresh. The official Codex CLI
owns its authentication behavior. A pending or invalid login needs an explicit
`xswap login NAME`.

## Project accounts and conversation handoff

```sh
xswap project use work
xswap project current
xswap project switch account-2
xswap project switch account-2 --sessions 24h
xswap project clear
```

`project use` pins new Codex processes in the current repository without copying
history or restarting anything. XSwap stores the name in `.xswap-account` at the
repository root, and the nearest parent file wins. Before writing the file,
XSwap adds `/.xswap-account` to the repository's local `.git/info/exclude` so it
cannot be committed accidentally and the shared `.gitignore` remains unchanged.
Project pins are refused at the user home and filesystem root. A handoff started
from the user home still supports standalone conversations: it changes the
global account for unpinned directories without creating `~/.xswap-account`.

Choose **Continue sessions with another account…** in the panel, or run
`project switch`, for a handoff. The panel follows this sequence:

1. when opened from the user home, select from existing projects discovered in
   Codex session metadata; directories without conversations are not listed,
   and each row counts unique conversations across registered accounts;
2. inside a project, skip that picker and show its conversations directly;
3. show the chosen project's deduplicated conversations from every account,
   label each source account, and select all initially; use the visible time
   filter controls to narrow the list;
4. use `Space` to toggle one conversation, `a` to select or clear all, and
   `Enter` to continue;
5. for each selected conversation marked as diverged, explicitly choose which
   account copy is the source of truth;
6. select the destination account; and
7. review the project, source, destination, selected conversations, any
   destination copies that will be archived, and number
   of open managed sessions that will restart, then confirm with `Enter` or `y`.

The session picker starts with **All sessions**, so older conversations remain
visible by default. These shortcuts change the filter without leaving the flow:

| Key | Filter | Included sessions |
| --- | --- | --- |
| `1` | Recent sessions | The latest 20 conversations by last activity |
| `2` | Last 24 hours | Activity at or after 24 hours before the picker opened |
| `3` | Last 7 days | Activity at or after 7 × 24 hours before the picker opened |
| `4` | All sessions | Every conversation in the project |

The active filter and all four shortcuts stay above the scrolling list. Sessions
remain ordered by most recent activity, using the rollout file's modification
time rather than the conversation's creation date. Time boundaries are fixed
when the project picker opens its session list, so waiting or moving back from
destination selection does not make conversations disappear.

Each filter change resets selection to every matching conversation. `Space`
toggles one, and `a` selects or clears the displayed set. Conversations outside
the filter are excluded from the handoff. An empty result keeps the filter
controls available: press `4` to restore all sessions, or `Esc` to go back.

For CLI use, `xswap project switch NAME --sessions recent|24h|7d|all` applies
the same rules at command start and reports the active filter and match count.
Omitting `--sessions` preserves the default of all project conversations.
`--sessions` is accepted only by `project switch`; invalid values and an empty
filtered result stop before copying sessions or changing account selection.
Existing confirmation, `--yes`, conflict resolution, and safety checks apply.

Conversation titles prefer the first real user request and skip injected
environment and AGENTS.md instructions. An agent session with no request of its
own shows **Agent ·** followed by its parent conversation's request when the
parent is present in the same account. Otherwise it shows **Agent session**;
the date and source account remain visible to help distinguish it.

A divergent conversation never blocks compatible conversations in the same
project or Home scope. Deselect the conflict to leave all its copies untouched.
If it stays selected, XSwap will not continue until a source account is chosen.
When the destination contains a different history, XSwap archives that file
privately under `~/.codex-swap/session-conflicts` before copying the chosen
source. If archival fails, the destination remains unchanged.

If a selected source conversation is currently open outside XSwap supervision,
the review names it and marks it for manual resume. After confirmation XSwap
copies and indexes the conversation but cannot stop its existing process. Close
that old Codex process before sending another message, then run `codex resume`
in the project to continue with the destination account. If the destination or
another copy of the same conversation is open, XSwap refuses the transfer to
avoid overwriting an active history.

After confirmation XSwap:

1. validates, copies, and indexes a safe snapshot of every selected conversation
   from its source account into the chosen destination;
2. pins the destination account for the project, or updates the global account
   for a home-scoped handoff;
3. stops only selected Codex child processes supervised by the XSwap wrapper;
   and
4. fast-forwards and resumes each selected managed conversation in its original
   terminal and working directory.

The project pin means future `codex` processes opened in that repository use the
destination account. A home-scoped handoff changes the global selection for
future processes in unpinned directories. Other projects and terminals are
unchanged. Conversations
remain in the source profile and append-only transfers can safely return to an
earlier account. If both copies changed independently, the panel requires an
explicit source choice and archives a replaced destination copy instead of
silently overwriting either history. The noninteractive `project switch`
command refuses unresolved divergences; use the panel to resolve them. XSwap
does not copy authentication, config, cache, database, or unrelated sessions.
Open sessions started outside the XSwap wrapper cannot be restarted automatically.
XSwap detects their active writer locks, labels them for manual resume, and
requires the old process to be closed before the destination copy is used. Once
resumed through the wrapper, future handoffs can restart them automatically.

An interactive `codex resume` command does not reveal the selected conversation
ID to its wrapper before Codex opens the picker. XSwap records the project's
active writer locks before launch and immediately persists the one newly active
conversation against that supervisor. Conversations opened later therefore do
not make an already identified process ambiguous. If more than one conversation
could match before identification, XSwap does not guess. A selected candidate is
copied safely and labeled **supervised · manual resume if transferred**; close
the old process and run `codex resume` with the destination account.

The noninteractive `xswap project switch NAME` command selects every conversation
in the project because it has no interactive session picker.

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

### Settings

XSwap stores settings in `~/.codex-swap/settings.json` (or inside
`CODEX_SWAP_HOME` when set). Prefer the CLI commands for changes. Hand-edited
files support these keys:

| Key | Default | Accepted values |
| --- | --- | --- |
| `disabled` | `[]` | Array of account names excluded from auto-switch |
| `displayNames` | `{}` | Object mapping account names to display labels |
| `auto.enabled` | `false` | Boolean |
| `auto.threshold` | `90` | Integer from 1 to 100, percent used |
| `auto.interval` | `60` | Integer of at least 10 seconds |
| `auto.cooldown` | `300` | Nonnegative integer, seconds |
| `auto.margin` | `5` | Nonnegative integer, percentage points |

Missing fields retain their defaults, including files written by older XSwap
versions. Existing case-insensitive field names remain accepted. Unknown keys
are ignored with a warning naming their path, such as `auto.threshhold`, on
standard error at startup and in the panel. Valid fields still apply; a
misspelled field leaves the supported field at its default. `xswap doctor`
also reports unknown keys as a warning (exit code 1). Warnings never print
setting values.

Correct or remove unknown keys before making changes. Reading settings does
not rewrite the file; saving through XSwap writes supported fields only, so
back up a file from a newer version before using an older XSwap to edit it.
Invalid JSON, incorrect field types, and out-of-range auto-switch values remain
errors and prevent settings changes until repaired.

### Supervised background service

On macOS and Linux, an optional user service can start the enabled monitor at
login and restart it after a failure:

```sh
xswap service install
xswap auto on
xswap service status
xswap service logs
xswap service uninstall
```

The service is **not** installed by default, and installing it does not enable
auto-switch. On macOS it is a LaunchAgent at
`~/Library/LaunchAgents/com.bryanpinheiro77.xswap.auto.plist`; on Linux it is a
systemd user unit at `~/.config/systemd/user/xswap-auto.service`. Both files are
private and contain only the XSwap executable path and manager directory, never
account credentials. Existing unrecognized files at those paths are left alone.
When `xswap` is available in `PATH` as a link to the running executable, the
service uses that stable path so package upgrades do not strand an old versioned
binary path.

An enabled monitor already running in detached mode stops before the service
takes over. The existing singleton lock prevents two monitors from rotating
accounts at once. `xswap auto off` stops the managed process cleanly; turning it
back on starts the service. macOS launchd waits at least 30 seconds between
restarts after failures. Linux systemd uses a 30-second restart delay with a
five-start limit in five minutes. A clean stop is not restarted. If the service
is removed, the auto-switch setting remains unchanged; the detached monitor
can start again the next time `xswap` or `codex` opens.

`xswap service logs` shows the last 100 lines from the private macOS log at
`~/.codex-swap/logs/service.log`, or the systemd user journal on Linux. If
`service status` says the service is installed but the monitor is stopped while
auto-switch is on, run `xswap service install` to reload its registration, then
check the logs. On Linux, `systemctl --user reset-failed xswap-auto.service`
clears a restart limit reached after repeated crashes. Run `xswap service
uninstall` before removing the XSwap package; `xswap uninstall` removes the
service as part of its normal cleanup. Windows does not provide this optional
service yet.

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
unused valid name. Archival is not permanent credential deletion. Runtime-only
directories recreated by an already-running Codex process are ignored; their
contents are preserved in place and can be archived if you explicitly add the
same account name again.

## Configuration and upgrades

Manager state defaults to `~/.codex-swap`; `CODEX_SWAP_HOME` changes that location.
Explicit `CODEX_HOME` takes priority for the `codex` wrapper. Run
`unset CODEX_HOME` to return to normal manager selection.

Profiles copy `config.toml` only on creation. Subsequent config updates and plugin
installations are not synchronized. Skills are shared by symlink. Each account
has separate Codex history and runtime state.

On macOS and Linux, `xswap install` places a durable wrapper under
`~/.codex-swap/bin` and adds that directory before package-manager commands in
the active shell configuration. Updating Codex through npm or another package
manager still updates the official CLI that XSwap launches, without replacing
the active wrapper. Open a new terminal after installation or repair. If the
shell block or wrapper is missing, the panel shows **Repair Codex
integration…**; selecting it is equivalent to running `xswap install` again.
On Windows, the installer similarly keeps `%LOCALAPPDATA%\XSwap\bin` first in
the user `PATH`. `xswap uninstall` stops auto-switch and removes this integration
without deleting account data or the official CLI.

## Diagnose installation problems

Run `xswap doctor` for an offline check of the XSwap version and platform,
saved official Codex CLI path, command wrappers, `CODEX_HOME` precedence, manager
directory access, pending named-account logins, settings validity, and
auto-switch monitor health. Warnings and failures include a suggested next
step. The command does not change account selection or settings and does not
read or print credential contents, conversation history, or quota responses.

The exit code is `0` when all checks pass, `1` when the report contains
warnings, and `2` when a required component fails. For example, a missing Codex
CLI suggests installing it and running `xswap install`; a pending profile
suggests `xswap login NAME`.

## Updating

Use `xswap version` for the installed version, `xswap update --check` for a fresh
GitHub check, and `xswap update` to install with confirmation. Automation can use
`xswap update --yes` as explicit installation approval. The home menu displays
**Update version…** only when its background check detects a newer stable release.
Drafts and prereleases are excluded. Failed checks do not show an update.

Checks are cached for 15 minutes. The CLI check always refreshes that cache.
Release binaries embed their repository and source builds fall back to the
canonical `BryanPinheiro77/xswap`, so a `git clone` installation detects updates
without extra setup. Use `xswap update --repo OWNER/xswap --check` once to point
either at a fork; the stored choice takes precedence over the built-in default. Updates support macOS, Linux, and Windows on arm64 and
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
