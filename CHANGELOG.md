# Changelog

This project follows semantic versioning. Published entries correspond to Git
tags; unreleased work stays in this section until a release is prepared.

## Unreleased

## v0.4.0 — 2026-09-18

### Changed

- Rename the project handoff menu action to **Continue sessions with another
  account…** so its effect is clear before selecting a destination account.
- Add a session picker before account selection, with every project conversation
  selected initially and `Space` controls for selective continuation.
- Label conversations with their first real user request, skipping Codex
  environment metadata and falling back to the project folder and update time.
- Keep supervised Codex processes attached to the foreground terminal so
  interactive startup is not suspended by Unix job control.
- Let `a` select or clear every conversation in the handoff picker and accept
  either `Enter` or `y` on the final review with mode-specific footer guidance.
- Associate one selected conversation with the only unidentified managed Codex
  process in its directory, allowing picker-based `codex resume` sessions to restart.
- Safely fast-forward append-only session histories when moving a conversation
  back to an account, while rejecting histories changed in both accounts.
- Show the source and destination account after a handoff, mark the account
  effective for the current project as active, and identify a different global
  default separately.
- Open the current project's conversations directly, while offering a
  session-derived project picker when XSwap is opened from the user home.

### Added

- Add project-local account selection through `.xswap-account` and the
  `xswap project use`, `current`, and `clear` commands.
- Add a confirmed project account handoff that copies only matching Codex
  conversations and automatically resumes running XSwap-managed sessions in
  their original terminals.
- Discover project conversations across all registered accounts when the current
  project pin or global selection differs from the account that owns them.

### Fixed

- Register transferred conversations through Codex's app-server so inactive
  sessions appear in the destination account's resume picker.
- Preserve the selected resumed conversation when a supervised process has not
  yet published its session identifier.
- Detect open conversations outside XSwap supervision and stop before copying,
  naming the sessions that must be reopened with `codex resume`.
- Keep each project picker's conversation count aligned with the source account
  and the sessions shown on the following screen.

### Security

- Validate session metadata and containment, reject source and destination
  symlinks and conflicts, and never copy credentials, configuration, caches, or
  unrelated profile data during a project handoff.
- Build releases with Go 1.26.6 or newer so shipped binaries include the latest
  standard-library security fixes.
- Refuse persistent project pins at the user home and use an explicit global
  account switch for home-scoped handoffs; filesystem-root handoffs are blocked.
- Add `.xswap-account` to the repository's local `.git/info/exclude` before
  writing it, preventing accidental commits without changing project files.

## v0.3.4 — 2026-09-17

### Fixed

- Restore the terminal before removing an account, show removal progress, and
  accept a single lowercase or uppercase confirmation key in the panel.

### Changed

- Avoid duplicate CI matrices for feature-branch pushes while preserving full
  pull request, protected-branch, merge queue, and release validation.

## v0.3.3 — 2026-09-17

### Fixed

- Make draft release uploads resumable and retry-safe, reusing verified assets,
  replacing incomplete uploads, and rejecting duplicate drafts explicitly.

## v0.3.2 — 2026-09-17

### Fixed

- Keep update confirmation inside the terminal panel so one `y` confirms the
  selected update without input being consumed during terminal-mode changes.
- Stop the panel keyboard reader from reading ahead when handing input to login
  or a prompt, preventing the first Enter from being consumed after Add account.

## v0.3.1 — 2026-09-17

### Changed

- Show release updates in Homebrew-managed panels and route confirmed upgrades
  through `brew update` and `brew upgrade xswap` before restarting XSwap.

## v0.3.0 — 2026-09-17

### Added

- Homebrew distribution for macOS and Linux with stable command wrappers and a
  release-generated formula validated in CI.

### Changed

- Route package-managed upgrades through Homebrew and hide the standalone update
  action for those installations.

## v0.2.1 — 2026-09-16

### Fixed

- Preserve the last valid quota reading when Codex returns an incomplete response,
  mark retained data as stale, and exclude it from automatic account selection.

## v0.2.0 — 2026-09-16

### Added

- Native Windows support with user-PATH command wrappers, interactive terminal
  handling, portable process control, and Codex `.exe`, `.cmd`, or `.bat`
  launchers.
- Windows AMD64 and ARM64 release zip files, native CI integration tests, and
  versioned self-update activation that does not replace a running executable.

## v0.1.4 — 2026-09-16

### Changed

- Refresh release availability in an open panel every 15 minutes, while keeping
  cached checks within that interval.

## v0.1.3 — 2026-09-16

### Fixed

- Make account renaming a two-step panel flow and load the selected account's
  current display name into the editor.

## v0.1.2 — 2026-09-16

### Added

- Optional account display names with e-mail fallback for privacy-friendly
  terminal panels (`xswap add NAME --label LABEL` and `xswap rename`).

## v0.1.1 — 2026-09-15

### Fixed

- Forward the Update version action from the terminal panel to the confirmed
  updater instead of silently ignoring the selection.
- Show a checking-for-updates message before fetching release metadata.
- Display the running version in the home panel so the installed build is visible.

### Added

- A real terminal integration regression test for update selection and terminal
  restoration, using isolated state and simulated quota/release responses.

## v0.1.0 — 2026-09-15

- Check stable GitHub releases in the background and show Update version only
  when a newer version is available.
- Add confirmed, checksum-validated atomic updates for macOS and Linux.

### Changed

- Group Go source and tests under `cmd/xswap/` and community documents under
  `.github/`, keeping the repository root focused on project and build files.

### Added

- Go executable with an English terminal menu and thin quota bars.
- Behavioral coverage for isolated wrapper installation/restoration, simulated
  login, storage failures, CLI management, incomplete quotas, and auto-view status.
- Isolated Codex account login, manual selection, and parallel account runs.
- Live quota watching through the official Codex app-server protocol.
- Opt-in background auto-switch with a configurable threshold, five-minute
  cooldown, minimum improvement, and fresh-quota checks.
- Auto-switch view with monitor status, eligibility, and recent switches.
- Enable/disable controls for automatic rotation; manual selection stays available.
- Account removal with local archival and protection for the active/default account.
- Contributor documentation, CI checks, draft release automation, and repository
  protection configuration.

### Fixed

- Validate settings before archiving a profile, so corrupt settings leave the
  account registered and its files in place.
- Reject repository paths containing invalid owner names or dot-only repository
  names before changing updater configuration.

### Known limitations

- Auto-switch applies to newly started Codex processes, not running sessions.
- Only macOS and Linux are supported. Claude support is planned for the future.
- macOS artifacts are not Apple-notarized; release checksums are not signed.
