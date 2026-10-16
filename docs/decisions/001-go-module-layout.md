# ADR-001: Initial Go Module Layout

**Status:** Accepted  
**Date:** 2026-10-16

## Context

Scopra is a monorepo with a CLI, a local daemon, cloud services, and shared Go packages. At project start, it has no Go source or module metadata. The components are not independently versioned or released.

## Decision

Start with one Go module at the repository root: `github.com/SynergyFlowLabs/scopra`. Use Go 1.26.0 language/module semantics and pin the development/CI toolchain to Go 1.26.9. Keep the existing `go.work` as a workspace containing the root module. Do not add nested modules until a component needs independent versioning or release management and a follow-up ADR approves the split.

## Consequences

- `go test ./...` and `go vet ./...` cover the complete Go codebase from the repository root.
- Shared internal packages remain importable across Scopra binaries without local `replace` directives.
- Services and packages are not independently versioned at this stage.
- A future module split must update the workspace, CI, import paths, and release policy together.

## Security Considerations

The initial protocol package uses the Go standard library only. Third-party dependencies require review before introduction. Go toolchain patch updates must be applied promptly; CI uses pinned GitHub Actions and the declared Go toolchain.
