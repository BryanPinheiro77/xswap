# Security policy

Security fixes target the newest release. Before 1.0, compatibility may change
between minor versions; check release notes before upgrading.

## Reporting a vulnerability

After the repository is published, use GitHub's **Security → Report a
vulnerability** feature for private reports. Maintainers must enable private
vulnerability reporting during repository setup. If that feature is unavailable,
open an issue requesting a private contact without including exploit details,
credentials, or private account data.

Include the affected version and platform, expected and actual behavior, and a
minimal reproduction using temporary profiles. Use fake credentials throughout.
Coordinate public disclosure after maintainers have assessed the report and
prepared a fix. Response times are best effort for this volunteer project.

## Credential handling

The manager invokes the official Codex CLI for login, token refresh, and quota
queries. It does not implement OAuth or send credentials to its own servers.
Profile directories are created with mode 700; manager state, copied config,
lock files, and named-account auth files use private permissions.

`~/.codex-swap` contains credentials and account metadata. Do not commit, upload,
or share it. Removed profiles are archived under `~/.codex-swap/removed`, so
removal is not credential erasure. Delete an archive yourself if permanent
erasure is necessary, and revoke sessions using your account provider when needed.

The original `default` account retains its existing credential-store setting.
Named profiles force file-based storage in their isolated Codex home. Quota
queries and account removal use per-account lock directories; config and selection
updates use a shared state lock. These locks coordinate manager processes, not
arbitrary external programs that edit account files.

Release archives include SHA-256 checksums. Checksums detect accidental corruption;
they are not independently signed publisher attestations. macOS binaries are not
Apple-notarized, and Windows binaries are not code-signed.
