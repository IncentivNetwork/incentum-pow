# Release process

How a mainnet release is cut, and the one rule that keeps `geth version` honest.

## The rule

**The version bump lands before the tag, in its own commit, and the tag is cut
from a commit that contains it.**

`params/version.go` is the only place the version is written down. A tag is just
a label on a commit — it is not compiled into the binary and nothing reconciles
the two. If the tag is cut from a commit whose `VersionPatch` still holds the
previous value, the release builds and ships, the tarball is named after the
tag, and the binary inside reports the old number. Nothing fails; the two simply
disagree, and an operator running `geth version` is told the wrong thing.

So, ahead of each mainnet release:

1. Open a one-commit PR bumping `params/version.go` — `VersionPatch` for an
   ordinary release, the appropriate component otherwise. `VersionMajor`,
   `VersionMinor` and `VersionMeta` stay put unless the release genuinely
   changes them.
2. Merge it.
3. Cut the tag from a commit that contains that bump.

Verify before tagging, on the commit you are about to tag:

```bash
make geth && ./build/bin/geth version | grep '^Version:'
```

The number it prints is what operators will see. It must match the version part
of the tag you are about to create.

## Where the version comes from

`params/version.go` defines four constants and everything else derives from
them:

| Producer | Value | Example |
| --- | --- | --- |
| `params.Version` | `Major.Minor.Patch` | `1.11.10` |
| `params.VersionWithMeta` | plus `VersionMeta` | `1.11.10-stable` |
| `params.VersionWithCommit` | plus the short commit — this is what `geth version` and the RPC client string report | `1.11.10-stable-3125695d` |
| `params.ArchiveVersion` | archive naming | `1.11.10-3125695d` |

Nothing in the build hard-codes a version. `build/ci.go` passes
`params.VersionWithMeta` into the Docker build args, and the release job in
`.github/workflows/build.yml` names its tarball from `${GITHUB_REF_NAME}` — the
tag it was triggered by. That is exactly why the tag and the binary can drift:
the tarball is named from one source and its contents from the other.

## Releasing

CI publishes a GitHub Release for each pushed `v*` tag: a static
`linux/amd64` tarball plus its SHA-256 checksum. See
`docs/dpow/DPOW_NODE_OPERATOR_GUIDE.md` §4.3 for the operator side — download,
verify, install.

That section tells operators to run `geth version` and "confirm the embedded
version and Git commit match the released tag". Cutting a tag without the bump
is what makes that instruction impossible to satisfy: the operator does the
right check, sees a mismatch, and has no way to tell a stale tag from a wrong
download.

Note that `.github/workflows/build.yml` runs tests on pull requests into `main`
and on `v*` tags only. A version-bump PR opened against `develop` gets no CI, so
run the suite locally before merging it:

```bash
go run build/ci.go test ./params/... ./cmd/geth/...
```

## Historical record

Kept because the tags are published and are not being rewritten.

| Tag | `VersionPatch` at that commit | Binary reports | Agrees |
| --- | --- | --- | --- |
| `v1.11.7-dpow-mainnet` | 7 | `1.11.7-stable` | yes |
| `v1.11.8-dpow-mainnet` | 8 | `1.11.8-stable` | yes |
| `v1.11.9-dmbf-mainnet` | 8 | `1.11.8-stable` | **no** |

Only `v1.11.9-dmbf-mainnet` drifted: it was cut without bumping the file, so
nodes running that release report `1.11.8`. There has never been a build that
reports `1.11.9`, which is why the next release goes to `1.11.10` — it resumes
the sequence from the highest published tag rather than filling the gap.

Re-tagging or rebuilding that release is deliberately out of scope: the commit
hash in `geth version` identifies the build unambiguously, and a rolling rebuild
of the fleet is not worth the churn.
