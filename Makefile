GO ?= go
GOFMT ?= gofmt

.PHONY: help bootstrap test lint security-test

help:
	@printf '%s\n' 'Scopra development targets:' '  bootstrap     Check the Go toolchain and prepare modules' '  test          Run Go tests with the race detector' '  lint          Check gofmt and run go vet' '  security-test Run protocol, local IPC, workspace, and doctor security tests'

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
	$(GO) test -race ./packages/protocol ./internal/localipc ./internal/workspace ./cmd/scopra -run '^TestSecurity_'
