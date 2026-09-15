# GitHub setup

These files prepare repository policy. **Committing them does not enable server
protections.** Apply the rules after creating the repository and running CI once.
See GitHub's [ruleset documentation](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/creating-rulesets-for-a-repository).

## Publish the initial repository

Review `git status` and `git log --oneline` before publishing. Build outputs, manager state, auth files,
and credentials stay outside Git. Primary documents are English; pt-BR docs
will be added in a later PR after publication.

```sh
git branch -M main
gh repo create OWNER/xswap --public --source . --remote origin --push
```

Replace `OWNER` with the intended account or organization. Set `main` as default.
The prepared checkout already has logical commits for project scaffolding,
the application and tests, documentation, and CI/release configuration. Preserve
that history; no additional initial commit is needed when the working tree is clean.
Wait for the first CI run to pass before enabling required status checks.

## Apply repository settings

Requirements: GitHub CLI, `jq`, and authenticated repository administration access.

```sh
sh scripts/configure-github.sh OWNER/xswap
```

For a solo maintainer, requiring an approval prevents merging your own PR. Use:

```sh
sh scripts/configure-github.sh OWNER/xswap --solo
```

The script creates or updates named rulesets, enables private vulnerability
reporting, sets squash-only merges, and creates issue/release labels.

| Rule | Result |
| --- | --- |
| Protected `main`, `release/**` | PR-only updates; no direct pushes |
| Required checks | `Branch policy`, `Quality / linux`, `Quality / macos` |
| Up-to-date branches | Checks must pass against current base changes |
| Reviews | One approval by default; zero with `--solo` |
| Conversation resolution | Review threads must be resolved |
| Linear history | Squash merges; no merge commits |
| Branch protection | No deletion or force pushes on integration branches |
| Release creation | Only repository administrators may create `v*` tags |
| Immutable tags | Version tags cannot be moved or deleted without changing policy |

The branch policy check accepts PRs against `main` or maintained `release/*`
branches. It fails on other targets; server enforcement on additional branches
requires the owner to extend protection rules to those branches.

Add maintainer handles to `.github/CODEOWNERS` after publication.
Publish a private conduct-reporting contact in `CODE_OF_CONDUCT.md` before
inviting community participation.
Enable required code-owner reviews once it is accurate. Turn on dependency alerts and secret
scanning where available. Fork PRs use hosted runners, `pull_request`, read-only
permissions, and no personal Codex credentials.

## Verify protection

Open a documentation PR and confirm the three required contexts appear.
Verify failed checks, unresolved discussions, and direct pushes block merging.
The local preparation does not create a remote repository, upload files, or
apply administration changes automatically.
