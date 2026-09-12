# Releasing

## Version-file driven publishing

Every language SDK has **one** version source file:

| Language | File | Tag | Distribution |
|---|---|---|---|
| Go | `go/version.go` (`Version` const) | `go/vX.Y.Z` | Go module proxy (automatic) |
| Python | `python/pyproject.toml` (`[project] version`) | `python-vX.Y.Z` | PyPI |
| Example plugins | `examples/go/{name}/manifest.toml` (`[plugin] version`) | `{name}-vX.Y.Z` | plugin market (run by `release-plugin.yml` + `scripts/plugin-release.sh`) |

`scripts/plugin-pack.py` (the market tooling) is a versionless stdlib-only
Python script — no tag, consumed via raw download.

Publishing is a commit, not a command:

1. Bump the version file, push to `main`.
2. The release workflow detects a version that has no matching tag yet, runs
   the full verification (`make check`), then creates the tag and publishes
   (Go: GitHub release + module proxy; Python: PyPI).

Rules and guardrails:

- **Never push a version bump while `make check` fails** — the release is
  silently skipped (the workflow fails visibly) until verification passes.
- **Never create bare `vX.Y.Z` tags at the repo root.** The Go module lives
  in the `go/` subdirectory, so its tags must be prefixed `go/`. A bare
  semver tag would make `go get` fail with "no go.mod at repository root".
- Same version twice: the tag already exists, the workflow is a no-op.
  Fixing a bad release requires a new version (Go additionally supports
  `retract` directives; PyPI does not allow re-uploading).
- Pre-releases are allowed: `go/v0.2.0-rc.1` (`Version = "0.2.0-rc.1"`).
- `workflow_dispatch` on the release workflows re-runs the flow for the
  current version file (fallback when the GitHub release step failed after
  the tag was created; not a way to re-publish the same version on PyPI).

## Protocol regeneration

`proto/` is the shared contract source. `make regen` regenerates
`go/gen/` (and later `python/gen/`) with the pinned toolchain
(protoc 29.3 + protoc-gen-go v1.36.12 + protoc-gen-go-grpc v1.6.2, pinned
via `go/tools.go`). CI asserts committed stubs stay in sync (`make
regen-check`). Bump the toolchain pins in `proto/regen.sh` / `go/tools.go`
together, and commit the regenerated output in the same PR.

## Go publishing (proxy / sumdb / pkg.go.dev)

There is no registry to publish to: a pushed `go/vX.Y.Z` tag **is** the
release. The release workflow already warms the ecosystem:

1. `git push` of the tag → the module is resolvable by anyone:
   `go get github.com/hyuan280/Sonicore-PluginSDK/go@go/vX.Y.Z`
2. `go list -m` step in the workflow triggers the first fetch, so
   proxy.golang.org caches the module and sum.golang.org records its
   checksum immediately.
3. pkg.go.dev indexes the docs on first request — visit
   `https://pkg.go.dev/github.com/hyuan280/Sonicore-PluginSDK/go` after the
   first release to create the page.

Requirements:

- The GitHub repository **must be public**: the public proxy and sumdb
  cannot fetch private repositories. Consumers of a private repo must
  configure `GOPRIVATE` + git credentials and forego the public proxy.
- The repo must contain a LICENSE file (present) for pkg.go.dev to serve
  the documentation.

Consuming the SDK (host or plugin authors):

```
go get github.com/hyuan280/Sonicore-PluginSDK/go@go/v0.1.0
```

Local development against a checkout keeps the `replace` directive; remove
it when pinning a released version.
