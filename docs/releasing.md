# Releasing

Use semantic version tags such as `v0.1.0`. Before 1.0, minor releases may contain
documented breaking changes; patch releases should preserve compatibility.

## Prepare a PR

1. Start from `main` or a maintained release branch.
2. Run `make check` and review the changelog.
3. Move relevant `Unreleased` entries into a dated version section.
4. Document compatibility changes and limitations.
5. Merge after checks and reviews pass.

The initial pipeline requires release commits to be reachable from `main`.
For older maintained branches, merge the release commit back into `main` before
tagging, or revise that policy through a reviewed PR.

## Create the tag

```sh
git switch main
git pull --ff-only
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

Only repository administrators can create version tags under the prepared rule.
Never move or reuse published tags. Tag immutability is separate from creation
privileges so maintainers do not automatically bypass it.

## Review the draft

The Release workflow reruns CI on macOS, Linux, and Windows, verifies an annotated
semantic tag on `main`, builds six archives, computes SHA-256 checksums, and
creates a **draft GitHub release** with generated notes. Targets are macOS,
Linux, and Windows, each for arm64 and amd64.

Unix archives contain `xswap`; Windows zip files contain `xswap.exe`. Every
archive also includes the README and license. Version, source commit, and UTC
build date are embedded; inspect them with `xswap version`. macOS binaries are
not Apple-notarized. Checksums are not independent signed attestations.

Review notes, verify checksums, and smoke-test installation on accessible
platforms. Publish the draft through GitHub when ready. Failed jobs and draft
creation do not automatically publish a public release.

Local artifact verification:

```sh
make release VERSION=v0.1.0
cd dist
shasum -a 256 -c checksums.txt
```

Actions are pinned to commits; Dependabot proposes weekly updates. The release
token can write only in the draft job. CI needs no OpenAI key or personal account.

## Installed updates

Release builds embed `github.repository` for the updater. Publish the stable
draft before clients can discover it. Keep all six platform archives and
`checksums.txt` attached; archives contain flat regular files plus the platform
executable. The updater requires the matching archive and SHA-256 checksum.
Checksums protect download integrity; they do not replace trust in release
maintainers or protect against a compromised repository.

Local builds can embed the repository with
`make release VERSION=v0.1.0 REPOSITORY=OWNER/xswap`. Clients check periodically
while the panel runs and install only after explicit confirmation.
