# Changelog

This project follows semantic versioning. Published entries correspond to Git
tags; unreleased work stays in this section until a release is prepared.

## Unreleased

- Check stable GitHub releases in the background and show Update version only
  when a newer version is available.
- Add confirmed, checksum-validated atomic updates for macOS and Linux.

### Added

- Go executable with an English terminal menu and thin quota bars.
- Isolated Codex account login, manual selection, and parallel account runs.
- Live quota watching through the official Codex app-server protocol.
- Opt-in background auto-switch with a configurable threshold, five-minute
  cooldown, minimum improvement, and fresh-quota checks.
- Auto-switch view with monitor status, eligibility, and recent switches.
- Enable/disable controls for automatic rotation; manual selection stays available.
- Account removal with local archival and protection for the active/default account.
- Contributor documentation, CI checks, draft release automation, and repository
  protection configuration.

### Known limitations

- Auto-switch applies to newly started Codex processes, not running sessions.
- Only macOS and Linux are supported. Claude support is planned for the future.
- macOS artifacts are not Apple-notarized; release checksums are not signed.
