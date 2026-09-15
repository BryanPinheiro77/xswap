# Contributor notes for coding agents

XSwap is a Go terminal account manager. The first provider is Codex; Claude is
future scope. Keep the interface, comments, and primary documentation in English.

Read `.github/CONTRIBUTING.md` and `docs/architecture.md` before changing behavior.
Run `make check` for Go changes. Keep the Go runtime free of third-party
dependencies unless a concrete need is agreed with maintainers.

Tests must use temporary manager directories and fake app-server responses.
Never read, copy, print, or commit real account tokens. Do not alter the user's
Codex installation or selection while running tests. Do not turn on automatic
rotation in a real account environment as a side effect of development.

Auto-switch applies to new Codex processes only. Preserve the recheck under the
state lock, cooldown, candidate eligibility, and fail-safe handling of stale or
missing quotas. Account removal archives data and protects default/active accounts.

Update documentation and `CHANGELOG.md` for user-facing changes. Use feature
branches and pull requests targeting `main`. Publishing releases and applying
GitHub administration changes are maintainer actions described in the docs.
