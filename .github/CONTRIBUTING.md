# Contributing

XSwap welcomes bug fixes, documentation improvements, and focused features.
The current release supports the official Codex CLI on macOS, Linux, and Windows. Claude
support is a future project; discuss its design before implementing a backend.

## Local setup

Install Go 1.26.6 or newer. The panel uses Bubble Tea v2, and terminal support
uses `golang.org/x/term` and `golang.org/x/sys`; Go modules resolve their
transitive dependencies. Release binaries are statically linked and do not
require those libraries to be installed separately.
Real quota queries also require the official Codex CLI and ChatGPT login.
Tests use temporary profiles and a fake app server; no personal account is needed.

```sh
make check
make build
./bin/xswap --help
```

Tests must not read real tokens, use real accounts, or modify the contributor's
Codex installation. Set `CODEX_SWAP_HOME` to a temporary directory for manual
experiments with manager state. That setting alone does not isolate `default`:
the original account still uses `~/.codex`. Use named temporary profiles if you
need fully isolated authentication.

## Pull requests

1. Fork the repository and branch from `main`.
2. Use a focused branch such as `feat/auto-switch` or `fix/quota-timeout`.
3. Add behavioral tests for logic that changes selection, account eligibility,
   authentication boundaries, process cleanup, or file handling.
4. Run `make check` and update user-facing documentation.
5. Add an entry under `Unreleased` in `CHANGELOG.md`.
6. Open a pull request against `main` and complete the PR template.

Maintainers may use `release/*` branches for patches to older versions. Other PR
base branches fail the branch policy check. Do not push directly to protected
branches or rewrite published release tags.

Use descriptive commits. Conventional Commit prefixes (`feat:`, `fix:`, `docs:`,
`test:`, `ci:`) help scan changes but are not mandatory. Maintainers squash merges.

## Review expectations

Explain the concrete problem, resulting behavior, and validation. Keep changes
small enough to review. Review comments should address code and behavior, with
specific examples. Credential snapshots, tokens, or screenshots containing
private account information do not belong in PRs or issue reports.

The protected-branch configuration normally requires one approving review.
Solo-maintainer repositories can apply the documented `--solo` configuration;
tests, branch policy, conversation resolution, and PR-only merges still apply.

## Documentation and language

The interface, source comments, issues, PR templates, and primary documentation
use English. Brazilian Portuguese documentation will be added after publication.
Do not introduce partial translations into the primary documents.

See [architecture](../docs/architecture.md), [releasing](../docs/releasing.md),
[testing](../docs/testing.md), [repository setup](../docs/github-setup.md), and
[security policy](SECURITY.md).
