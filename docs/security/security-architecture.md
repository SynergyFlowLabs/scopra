# Security Architecture

Scopra separates identity, policy, human approval, and local enforcement authorities.

```text
Execute = valid user
  AND valid agent
  AND valid client
  AND valid machine
  AND unexpired scoped grant
  AND cloud policy allows
  AND local policy allows
  AND containment is available
  AND exact approval exists when required
  AND request is fresh
```

Any false or unavailable condition results in denial.
