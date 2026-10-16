# System Overview

```text
AI agent / host
  -> MCP or HTTPS
  -> cloud edge
  -> identity, grants, policy, sessions, approvals, audit
  -> authenticated outbound machine channel
  -> local daemon
  -> rootless worker
  -> approved workspace
```

The local daemon is the final authorization and containment authority. Cloud services may narrow but cannot broaden local policy.
