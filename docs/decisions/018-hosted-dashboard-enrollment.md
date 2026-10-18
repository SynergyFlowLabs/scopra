# ADR-018: Hosted Dashboard, Machine Enrollment, and Presence

**Status:** Accepted
**Date:** 2026-10-18

## Context

The repository's Vercel project is the static documentation site. A hosted dashboard cannot read a user's local files or terminal, and the existing local Unix socket is intentionally same-UID-only. Remote access therefore needs an authenticated control plane and an outbound machine channel; the browser must never connect to `localhost` or an inbound listener on the user's machine.

The first remote increment is limited to human sign-in, one-time machine/workspace enrollment, and presence. It does not authorize or execute commands. Remote execution remains behind later grants, authorization, approval, audit, and Hollow enforcement work.

## Decision

1. Keep the current static docs project and its root `vercel.json` unchanged. The dashboard is a separate Next.js project rooted at `apps/dashboard`, deployed to Vercel.
2. Use GitHub OAuth for human sign-in. The immutable GitHub account ID, not a mutable login name, is the dashboard subject. Auth.js owns the browser session; session cookies are secure, HTTP-only, same-site, and never exposed to client code as API bearer tokens.
3. Keep Caloris and Goldstone outside Vercel's serverless request handlers. The development/initial deployment is provider-neutral Docker: a long-running service for the control-plane/relay and a persistent PostgreSQL system of record. Redis is deferred until a concrete coordination need exists. Production deployment requires a host with persistent storage, TLS, and support for long-lived outbound relay connections.
4. The dashboard server acts as a same-origin proxy. It verifies the Auth.js session and signs a short-lived, audience-restricted service assertion for Caloris. The browser never receives the service signing key or a reusable backend token.
5. `scopra init` canonicalizes the selected directory and idempotently reuses or registers its local workspace. The machine generates an Ed25519 key locally and stores the private key only in the owner-only Scopra state directory. The key is bound to the pending enrollment and later machine identity.
6. Caloris issues a high-entropy, short-lived, single-use pairing code. Only a hash of the code is persisted. The CLI displays the dashboard URL and code separately; the code is never placed in a URL, query string, QR payload, or analytics event. The dashboard requires GitHub sign-in, shows the machine fingerprint and exact workspace path, and requires an explicit confirmation before consuming the code and binding the machine/workspace to that GitHub subject.
7. Mio establishes only outbound TLS connections. Goldstone authenticates the machine by its public-key-bound identity, maintains bounded presence leases, and routes only by authenticated machine identity. No local listener is exposed. Pairing does not create an execution grant.
8. The first dashboard surface shows paired machines/workspaces and online/offline presence. Terminal interaction is a later increment: only bounded, direct-executable commands with streamed output through Hollow, under explicit grants/authorization/approval/audit. Interactive PTY and unrestricted shell remain out of scope.

## Consequences

- Vercel serves the human-facing Next.js app; Caloris/Goldstone and PostgreSQL are a separate container deployment. The existing docs deployment is not converted to Next.js.
- GitHub OAuth client credentials and signing keys are deployment environment configuration, never committed or pasted into chat.
- The code can be developed and tested against Docker/PostgreSQL without selecting a cloud vendor. A live pairing URL is not considered ready until the API/relay host, OAuth app, TLS endpoints, backups, and secrets are configured.
- Enrollment, presence, authorization, and execution remain separate state transitions. A registered workspace or paired machine grants no command access.

## Security Considerations

- Pairing is proof-of-possession-bound, expires quickly, is consumed atomically, and is stored only as a hash. The code does not authenticate a browser session; GitHub sign-in does.
- Enrollment details are visible only to the authenticated account that presents the code. The user must explicitly confirm the displayed machine fingerprint and workspace path.
- Device private keys never leave the machine. Service assertions are short-lived and server-to-server; device requests are signed and replay-bounded.
- Presence is not authorization. A stale or revoked machine is offline/denied; absence of a fresh authenticated heartbeat is never treated as online.
- Workspace paths are sensitive metadata and are not sent to analytics. Commands and raw output are not part of this increment.
- Vercel's static docs security policy and deployment remain isolated from the dashboard's dynamic application and OAuth policy.

## Alternatives

- Put all services in Vercel functions: rejected because the relay requires persistent connections and the current Vercel project is a static docs deployment.
- Put a reusable access token in the dashboard URL: rejected because URLs leak through browser history, logs, referrers, and support screenshots.
- Accept pairing without an authenticated human account: rejected because it does not establish an attributable owner.
- Add remote commands in the enrollment increment: rejected because pairing is not a grant, and authorization, approvals, local enforcement, and audit are not implemented yet.
- Add Redis at the outset: deferred; PostgreSQL is the source of truth, and the first single-region service can use database-backed state until a measured coordination requirement exists.
