# execution

`execution.Service` is Mio's executor for local runs (ADR-011, increment 2). It
looks the workspace up in the registry, revalidates it with ADR-004's validator
immediately before use, and runs the request through Scopra Hollow (ADR-010),
passing only the record's canonical path to the sandbox. [ADR-011](../../docs/decisions/011-local-execution.md)
records the behaviour and this package's tests use a fake runner; the end-to-end
wiring in `cmd/scoprad` is tested with a fake Podman.
