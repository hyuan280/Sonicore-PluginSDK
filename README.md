# Sonicore PluginSDK

Multi-language SDKs for writing Sonicore plugins.

```
proto/          shared contract (source of truth for every language)
go/             Go SDK       (import github.com/hyuan280/Sonicore-PluginSDK/go)
python/         Python SDK   (PyPI sonicore-plugin-sdk, coming soon)
examples/       reference plugins + market catalog (repo.json)
scripts/        plugin-pack.py (packaging tool) + plugin-release.sh (CI pipeline)
docs/           releasing process
```

## Go SDK

```go
// go.mod of your plugin
require github.com/hyuan280/Sonicore-PluginSDK/go v0.1.0
```

```go
func main() {
    pluginsdk.Run("my-plugin", &MyPlugin{})
}

func (p *MyPlugin) Init(ctx context.Context) error {
    pluginsdk.ProvideNotifier(p) // MyPlugin implements Notifier
    return nil
}
```

See `examples/go/` for complete plugins and the package docs in
`go/pluginsdk.go` (lifecycle, config, data, logging, schedules, stdout/
stderr contract).

## Development

- `make check` — format + vet + test + tool syntax check + stub-sync check
  + examples build
- `make regen` — regenerate stubs from `proto/`
- Publishing is version-file driven, see `docs/releasing.md`

## Releasing a plugin

`scripts/plugin-pack.py` (Python 3.11+, stdlib only) builds/assembles the
release tarball of a plugin — Go binaries by default, any language via
`-files` — computes its sha256 and derives the download URL. Its
`check`/`add` subcommands validate the manifest version against the market
catalog (`examples/repo.json`) and upsert the entry.

`scripts/plugin-release.sh` is the CI orchestration the
`release-plugin.yml` workflow uses: version bump in a plugin's
`manifest.toml` → verify → pack → commit the catalog → push commit + tag in
one push → GitHub release + asset upload.
