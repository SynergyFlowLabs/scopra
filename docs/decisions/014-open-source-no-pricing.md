# ADR-014: MIT License and No Pricing Model

**Status:** Accepted (maintainer decision, 2026-10-17)
**Date:** 2026-10-17

## Context

The repository shipped without a license: `LICENSE` said only that all rights were reserved until one was selected. Without a license, nobody may legally copy, modify, or redistribute the code, so Scopra was not open source in practice, whatever its intent. The PRD and plan had also carried a future "billing, and plan limits" item, which implied a commercial tier gated by price.

The maintainer's instruction is explicit: Scopra stays **open source and free for everyone**, with no pricing model.

## Decision

1. **Scopra is licensed under the MIT License.** `LICENSE` holds the standard MIT text with the copyright line "Copyright (c) 2026 Scopra contributors". The license is permissive and includes the usual warranty disclaimer.
2. **There is no pricing model.** No paid tiers, subscriptions, usage-based billing, plan limits, feature gates, trials, or account requirements exist or are planned. Every capability documented in this repository is available to everyone under the same license. The plan's Milestone 6 "billing, and plan limits" item was removed, and the PRD records the same rule as a product principle and as an alpha non-goal.
3. **Contributions are inbound under the same license.** `CONTRIBUTING.md` states that a contribution is licensed under the MIT terms. No separate CLA is required.
4. **Third-party material keeps its own license.** `NOTICE` records the Podman documentation test fixture (Apache-2.0, provenance in `internal/hollow/testdata/README.md`) and the third-party development skills under `.agents/skills/` (their licenses are recorded in each `SKILL.md`). They are not relicensed by this decision.

## Consequences

- Anyone can use, modify, and redistribute Scopra, including commercially, provided the copyright and permission notice are kept. The MIT license does not require derivative works to be open source.
- No pricing machinery may be added to the product without reversing this ADR. That includes payment code, plan/quota tables that gate features, license keys, and "upgrade" prompts.
- Hosting or support services around Scopra remain possible for anyone, including the maintainers, but they cannot be required to use the software, because the software itself is free and complete.
- The project has no revenue mechanism by design; infrastructure costs (for example a hosted relay) are a separate operational question, not a product tier.
- MIT has no explicit patent grant and no copyleft. If either becomes important, a future ADR can relicense with the consent of the contributors whose code is affected; changing the license of existing code is not unilateral.

## Security Considerations

- Licensing does not change the threat model. The MIT warranty disclaimer does not reduce the project's own security requirements, which stay in the PRD, threat model, and ADRs.
- Because redistribution is permitted, security fixes must be assumed to reach downstream copies slowly. The project should therefore keep changes small, documented, and easy to backport.
- No license keys, activation servers, or telemetry-based entitlement checks will be added. Those would create new attack surface and new data collection for no product benefit.

## Alternatives

- **No license (status quo).** Rejected: it is not open source, forbids contribution and reuse, and contradicts the maintainer's intent.
- **Apache-2.0.** A reasonable choice with an explicit patent grant, but the maintainer selected MIT. It can be revisited in a future ADR.
- **AGPL-3.0.** Would keep hosted derivatives open, but it is more restrictive than requested and can deter adoption for a local developer tool.
- **Open core with paid tiers.** Rejected: explicitly out of scope. It would contradict the PRD principle and require billing, entitlement, and gating code.
