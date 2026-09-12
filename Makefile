GOIMPORTS := $(shell command -v goimports 2>/dev/null || { gopath=$$(go env GOPATH 2>/dev/null); if [ -n "$$gopath" ]; then echo "$$gopath/bin/goimports"; fi; })
LOCAL_PKG := github.com/hyuan280/Sonicore-PluginSDK

.PHONY: fmt fmt-check go-vet go-test go-build py-syntax regen regen-check check

## Formatting (Go SDK)
fmt:
	@if [ -z "$(GOIMPORTS)" ]; then echo "goimports not found: go install golang.org/x/tools/cmd/goimports@latest" >&2; exit 1; fi
	$(GOIMPORTS) -w -local $(LOCAL_PKG) go

fmt-check:
	@if [ -z "$(GOIMPORTS)" ]; then echo "goimports not found: go install golang.org/x/tools/cmd/goimports@latest" >&2; exit 1; fi
	@diff=$$($(GOIMPORTS) -l -local $(LOCAL_PKG) go); \
	if [ -n "$$diff" ]; then \
		echo "Files need formatting (run 'make fmt'):"; \
		echo "$$diff"; \
		exit 1; \
	fi

## Go SDK verification
go-vet:
	cd go && go vet ./...

go-test:
	cd go && go test ./...

go-build:
	mkdir -p out
	cd examples/go/demo && go build -o ../../../out/demo .
	cd examples/go/notifydemo && go build -o ../../../out/notifydemo .

## Tool verification (scripts/plugin-pack.py is a standalone Python script)
py-syntax:
	python3 -m py_compile scripts/plugin-pack.py

## Code generation (proto -> go/gen, python/gen)
regen:
	./proto/regen.sh

regen-check:
	./proto/regen.sh
	@git add -N -- go/gen proto 2>/dev/null || true
	@if [ -n "$$(git diff -- go/gen proto)" ]; then \
		echo "Generated stubs are out of sync with proto/ (run 'make regen' and commit)"; \
		git diff --stat -- go/gen proto; \
		exit 1; \
	fi

## Aggregate entry point used by CI
check: fmt-check go-vet go-test py-syntax regen-check go-build

## Publishing (CI-driven; see docs/releasing.md)
# Bump go/version.go (and later python/pyproject.toml) and push to main —
# the release-go / release-python workflows verify, tag and publish.
# No local release targets on purpose.
