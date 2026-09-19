# Bubble Tea v2 evaluation

- Status: Accepted
- Date: 2026-09-18
- Decision owner: XSwap maintainer
- Scope: terminal user interface only

## Context

XSwap's panel currently owns terminal raw mode, rendering, keyboard decoding,
resize handling, prompts, and subprocess handoff in `cmd/xswap/tui.go`. That
implementation is dependency-light and tailored to the current interface, but
the same file has grown to 1,203 lines. Recent defects around Enter, `y`, login,
and update prompts show that terminal ownership is a product concern rather than
incidental display code.

Issue #47 asks whether Bubble Tea v2 should become the panel foundation without
moving account, quota, project, session, update, or storage rules into the UI
framework.

## Evaluation

An isolated prototype exercised the behaviors that have caused regressions in
XSwap before the production migration:

- arrow-key navigation and single-key confirmation;
- editable Unicode text with save and cancellation;
- resize events and asynchronous refresh messages;
- alternate-screen setup and restoration;
- release of terminal input to an interactive child process and recovery after
  the child exits.

The last behavior is covered by an integration test that creates an 80x24
pseudo-terminal with the standard Unix `script` command. The test opens the
child prompt through `tea.ExecProcess`, sends one line of input, and verifies
that Bubble Tea redraws the panel after receiving the child result. The explicit
PTY size matters: a headless `script` process otherwise starts at 0x0 and has no
drawable viewport.

The prototype and production panel use Bubble Tea v2.0.9 through its canonical
module path, `charm.land/bubbletea/v2`.

### Measured impact

Measurements were taken on macOS arm64 with Go 1.26.6 and stripped binaries.
The final measurement uses the complete migrated panel. An earlier link-only
probe reported a smaller result because the Go linker removed renderer paths
that the probe did not call; the production measurement below supersedes it.

| Measure | XSwap v0.4.2 | Migrated panel | Change |
| --- | ---: | ---: | ---: |
| Stripped executable | 6,780,082 bytes | 8,012,162 bytes | +1,232,080 bytes (+18.17%) |
| Modules in build list | 3 | 23 | +20 |
| Panel source files | 1,203 lines | 1,312 lines | +109 lines (+9.06%) |

The standalone prototype executable is 3,588,722 bytes. It is not comparable to
the full XSwap binary and is recorded only to make the experiment reproducible.

`go mod verify` succeeds for the prototype. Unit tests pass with the race
detector on macOS. The interactive pseudo-terminal test passes natively on
macOS and in a Linux container, and the module builds for macOS arm64, Linux
amd64, and Windows amd64. Cross-compilation checks API and build portability; it
does not replace a native Windows terminal smoke test.

### Decision matrix

Scores use the priorities of XSwap's current terminal defects and roadmap. Each
row is capped by its weight.

| Criterion | Weight | Current loop | Bubble Tea v2 |
| --- | ---: | ---: | ---: |
| Terminal and subprocess ownership | 30 | 17 | 27 |
| Maintainability and testability | 25 | 14 | 22 |
| Cross-platform terminal behavior | 15 | 8 | 12 |
| Navigation, input, resize, and async UI | 15 | 7 | 13 |
| Dependency and supply-chain simplicity | 10 | 10 | 5 |
| Executable footprint | 5 | 5 | 4 |
| **Total** | **100** | **61** | **83** |

Bubble Tea scores higher because its event loop and `tea.ExecProcess` give one
component responsibility for pausing input, restoring terminal state, and
redrawing the panel. The current implementation remains stronger in dependency
simplicity. The 20-module increase is the main ongoing cost.

## Recommendation

Adopt Bubble Tea v2 for the XSwap panel through an incremental migration. Keep
the domain and operating-system code independent of Bubble Tea. UI models should
call narrow application services and translate their results into messages;
profile storage, quota selection, updates, session copying, and process
supervision must remain usable and testable without a renderer.

Confidence is **medium-high (78%)**. The prototype directly validates the most
important Unix terminal lifecycle and the binary cost remains acceptable for a
single static executable. Confidence is
below high until the migrated XSwap flows pass on all three supported operating
systems, including a native Windows terminal smoke test.

## Migration sequence

1. Introduce a small panel application boundary and Bubble Tea shell while
   retaining the existing business functions.
2. Move the home menu, watch view, resize behavior, and shared navigation.
3. Move text entry and confirmations, preserving the one-Enter behavior in PTY
   integration tests.
4. Move login, update, and session continuation through `tea.ExecProcess` or an
   equivalent command boundary that releases terminal ownership.
5. Remove the legacy raw loop after macOS, Linux, and Windows checks pass.

Each stage must leave `xswap` usable and keep the previous renderer easy to
restore until the last stage. Do not combine provider work or new account
features with this migration.

## Migration outcome

The production migration completed without provider or account-domain changes.
Automated tests passed natively on macOS, Linux, and Windows, and the terminal
handoff passed pseudo-terminal integration tests on macOS and Linux. The
maintainer also completed a manual macOS panel test. A manual Windows console
smoke test was not available during this migration, so that remains a documented
validation gap rather than an inferred result from cross-platform CI.

## Validation gate

Before removing the legacy loop, require:

- existing `make check` coverage to remain green;
- model tests for every menu action, cancellation, Unicode input, and resize;
- Unix pseudo-terminal tests for login, update, removal, and session handoff;
- native Windows tests plus a manual terminal smoke test;
- a manual macOS/Linux pass for alternate-screen cleanup and interrupted child
  processes;
- a dependency vulnerability review and review of any new transitive dependency
  introduced after v2.0.9.

## Sources

- Bubble Tea releases: <https://github.com/charmbracelet/bubbletea/releases>
- Bubble Tea v2 upgrade guide: <https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md>
- `tea.ExecProcess` implementation and lifecycle: <https://github.com/charmbracelet/bubbletea/blob/main/exec.go>
