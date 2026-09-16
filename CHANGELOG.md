# Changelog

This project follows semantic versioning. Published entries correspond to Git
tags; unreleased work stays in this section until a release is prepared.

## Unreleased

### Changed

- Refresh release availability in an open panel every 15 minutes, while keeping
  cached checks within that interval.

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
