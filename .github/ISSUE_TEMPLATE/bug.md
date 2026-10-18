---
name: Bug report
about: Something does not work as documented
title: "bug: "
labels: ["bug", "triage"]
---

## Summary

A clear, one-paragraph description of what is wrong.

## Steps to reproduce

1.
2.
3.

```sh
# exact commands, with secrets and personal paths removed
```

## Expected behavior

## Actual behavior

Include the error text and, if relevant, the exit status.

## Environment

- Scopra version or commit (`scopra version`):
- Distribution and kernel (`uname -a`):
- Podman version and mode (`podman info --format json`, rootless or rootful):
- Cgroup version (v1 or v2):
- State directory (`--state-dir`, or the default):

## Logs and journal

Paste only what is needed, and remove workspace paths you consider private.

- `scopra doctor` output:
- `scopra logs --json --limit 5` output (metadata only; it contains no command or output):

## Additional context

## Checklist

- [ ] I searched existing issues and found no duplicate.
- [ ] This is not a security vulnerability (those go to SECURITY.md privately).
- [ ] I removed secrets, tokens, and personal data from the report.
