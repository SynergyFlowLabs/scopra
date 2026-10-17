GO ?= go
GOFMT ?= gofmt
SITE_OUT ?= apps/docs-site/dist
SITE_STATIC ?= apps/docs-site/static
SITE_BASE_URL ?= https://scopra.vercel.app
SITE_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown')

.PHONY: help bootstrap test lint security-test site

help:
	@printf '%s\n' 'Scopra development targets:' '  bootstrap     Check the Go toolchain and prepare modules' '  test          Run Go tests with the race detector' '  lint          Check gofmt and run go vet' '  security-test Run protocol, journal, local IPC, workspace, registry, doctor, and daemon startup security tests' '  site          Render the documentation site into $(SITE_OUT)'

bootstrap:
	@./scripts/bootstrap.sh

test:
	$(GO) test -race ./...

lint:
	@unformatted="$$(find . -type f -name '*.go' -not -path './.git/*' -print0 | xargs -0 -r $(GOFMT) -l)"; \
	if [ -n "$$unformatted" ]; then \
		printf '%s\n' 'Run gofmt on these files:' "$$unformatted" >&2; \
		exit 1; \
	fi
	$(GO) vet ./...

security-test:
	$(GO) test -race ./packages/protocol ./internal/journal ./internal/localipc ./internal/workspace/... ./internal/hollow ./internal/execution ./cmd/scopra ./cmd/scoprad -run '^TestSecurity_'

# Render the static documentation site that Vercel publishes. The generator is
# part of this repository and has no dependencies beyond the Go standard
# library, so the deployed artifact is always the Markdown in this checkout.
site:
	$(GO) run ./tools/docsite -root . -out $(SITE_OUT) -static $(SITE_STATIC) -base-url $(SITE_BASE_URL) -commit $(SITE_COMMIT)
