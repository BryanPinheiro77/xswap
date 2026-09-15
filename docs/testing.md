# Testing

Run `make check` for formatting, static analysis, behavioral tests with Go's
race detector, and a local build. Run `go test -cover ./...` to measure statement
coverage. Coverage is a diagnostic, not proof that every behavior is tested.

Tests use temporary manager directories, fake credentials, a fake official-CLI
process, and an in-memory HTTP transport. They do not log in to real accounts,
call GitHub, or update the contributor's installed executable.

| Area | Successful behavior | Failure and boundary behavior |
| --- | --- | --- |
| Profiles | Numbered creation, private config, skills link | Invalid names; credentials and sessions are not copied |
| Management | Manual selection, enable/disable, removal archival | Active/default removal blocked; pending login rejected; corrupt settings and blocked archival preserve profiles; symlink profiles rejected |
| Storage | Private atomic replacement, lock release, legacy config defaults | Invalid JSON/config, file-parent write errors, lock contention and cancellation |
| Installation | Wrapper creation, repair and original-command restoration in a temporary home | Unrelated executables and symlinks preserved; missing original CLI rejected |
| Login | Simulated browser/device invocation inside a named temporary profile | Failed login reported; original account and selection preserved |
| Execution | Arguments and explicit account homes retained | Explicit `CODEX_HOME` takes priority only for the Codex wrapper |
| Auto-switch | Lowest eligible quota wins; committed selection | Disabled accounts, cooldown, stale/error/expired quotas, API keys, insufficient improvement, monitor off, changed active selection |
| Monitor | Background operation and clean stop | Duplicate monitor exits without taking over |
| Quota protocol | Handshake, notifications, account and quota responses | Cancellation terminates and waits for the server process |
| Panel | Menu, watch/back, enable/disable, removal, thin bars | Cancelled removal; update item hidden without a newer release |
| Updates | Stable release discovery, caching, confirmed command installation, atomic replacement, backup | Draft/prerelease/invalid tags, invalid repositories, offline/HTTP/JSON errors, oversized responses, checksum mismatch, unsafe/duplicate archive entries, foreign asset URLs, unconfirmed noninteractive install |

## Limits and release validation

Automated tests do not exercise an actual browser OAuth login or every terminal
size and key sequence. Cross-compilation checks build compatibility; it is not
the same as running the application on every supported operating system.

Before publishing a release, smoke-test the interactive panel and official
login on accessible target platforms. Verify the draft artifacts and checksums.
After the first stable release is published, validate discovery and installation
from that real GitHub release in an isolated installation. Until then, updater
tests verify the protocol and file-handling behavior with simulated downloads.
