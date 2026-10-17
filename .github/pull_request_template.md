## Summary

<!-- What changes, and why. Link the issue: Fixes #123. -->

## Scope

- [ ] Bug fix
- [ ] Feature (issue linked above; ADR proposed if the decision is durable)
- [ ] Documentation
- [ ] Test-only
- [ ] Refactor with no behavior change

## Security impact

- [ ] Threat model reviewed where relevant
- [ ] Negative authorization and failure-path tests added
- [ ] Audit events added or updated (or: Catena is not implemented)
- [ ] No secrets, command output, or personal data added to logs or the journal
- [ ] Fail-closed behavior preserved (no native fallback, shell, PTY, or bypass)
- [ ] New inputs, files, frames, and helper calls are bounded
- [ ] No pricing, entitlement, telemetry, or account machinery (ADR-014)

Describe the trust boundary this touches, if any:

## Testing

<!-- Commands run, and what they prove. Paste the failing test that now passes. -->

```sh
make lint && make test && make security-test
```

- [ ] New behavior is covered by a test that fails without the change
- [ ] Cross-platform build/vet checked when platform code changed
  (`GOOS=darwin`, `GOOS=windows`, `GOARCH=arm64`)

## Documentation

- [ ] Code comments explain non-obvious decisions
- [ ] README or a command README updated for user-visible changes
- [ ] ADR added or amended in `docs/decisions/` (with the index)
- [ ] `progress.md` entry added with the acceptance checklist and verification
- [ ] `CHANGELOG.md` `[Unreleased]` updated for user-visible changes
- [ ] `docs/repository-tree.txt` regenerated when files were added or removed

## Reviewer notes

<!-- Anything you want a second pair of eyes on. -->
