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
| Installation | Durable Unix links, shell blocks, Windows wrappers, repair detection, account routing, PATH priority, Codex package updates, and clean removal | Unrelated commands and shell content preserved; missing original CLI, unsafe shell files, incomplete blocks, and foreign wrappers rejected |
| Login | Simulated browser/device invocation inside a named temporary profile | Failed login reported; original account and selection preserved |
| Execution | Arguments and explicit account homes retained | Explicit `CODEX_HOME` takes priority only for the Codex wrapper |
| Projects | Nearest account pin, global fallback, full project conversation transfer, eager interactive-resume writer association before later sessions start, idempotent copy, explicit divergence resolution with either source, private conflict archival, and panel confirmation | Missing/disabled accounts, ambiguous supervised sessions falling back to manual resume without guessed process ownership, unresolved or cancelled divergence, archive failure, active conflicting destinations, symlinks, traversal, and failed indexing |
| Auto-switch | Lowest eligible quota wins; committed selection | Disabled accounts, cooldown, stale/error/expired quotas, API keys, insufficient improvement, monitor off, changed active selection |
| Monitor | Background operation and clean stop | Duplicate monitor exits without taking over |
| Quota protocol | Handshake, notifications, account and quota responses | Cancellation terminates and waits for the server process |
| Panel | Bubble Tea navigation, resize, Unicode input, watch/back, enable/disable, removal, thin bars | Zero-sized pseudo-terminals, cancelled removal, and update item hidden without a newer release |
| Updates | Stable release discovery, caching, confirmed installation, Homebrew routing, Unix atomic replacement, Windows version activation, backup | Homebrew refresh failure; draft/prerelease/invalid tags, invalid repositories, offline/HTTP/JSON errors, oversized responses, checksum mismatch, unsafe/duplicate tar or zip entries, foreign asset URLs, unconfirmed noninteractive install |
| Release publishing | New and resumed drafts, verified asset reuse, bounded upload retry | Divergent/starter assets replaced; duplicate drafts rejected |

## Limits and release validation

The Unix terminal integration tests use the standard `script` utility to open
an 80x24 or larger pseudo-terminal. They navigate and confirm actions with real
keyboard input, verify that the alternate screen is restored, and exercise a
child prompt through Bubble Tea's terminal handoff. The child receives exactly
one input line before Bubble Tea recaptures and redraws the panel. These tests
use simulated release metadata and never install an update or perform an
account login.

The Windows CI job builds the executable and runs native integration tests for
installation, wrappers, Codex routing, batch launchers, and safe update
activation. Automated tests do not exercise an actual browser OAuth login or
every terminal size and key sequence.

## CI event policy

Pull requests targeting any branch run the complete Linux, macOS, and Windows
matrix. This keeps checks available to fork contributions and lets the branch
policy job reject unsupported targets. Feature-branch pushes do not run a
second matrix for the same commit.

Pushes to `main` and `release/**` run the complete matrix again after integration
so protected branches are independently validated. Merge queue commits also run
through `merge_group`. Tag releases call the same workflow before building a
draft, preserving release validation without broadening ordinary push triggers.

Before publishing a release, smoke-test the interactive panel and official
login on accessible target platforms. Verify the draft artifacts and checksums.
`scripts/test-release-publisher.sh` uses a fake GitHub CLI to test retries,
resumption, digest comparison, replacement, and duplicate-draft rejection
without calling GitHub or creating a release.
After the first stable release is published, validate discovery and installation
from that real GitHub release in an isolated installation. Until then, updater
tests verify the protocol and file-handling behavior with simulated downloads.
