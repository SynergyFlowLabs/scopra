# Scopra hosted dashboard

This is the separate Next.js application for the hosted dashboard. The current
Vercel project at the repository root is the static documentation site and must
remain unchanged; configure a separate Vercel project with `apps/dashboard` as
its root.

The dashboard uses GitHub OAuth for human sign-in. It calls Caloris through
same-origin, authenticated server routes, and never sends browser requests to a
user's `localhost`, exposes a reusable backend token, or puts pairing codes in
URLs. Caloris runs as a long-lived container service backed by PostgreSQL; it
is not a Vercel serverless function (ADR-018). Remote execution remains deferred.

For the deployment runbook—including the separate Vercel project settings,
GitHub OAuth callback, assertion-key setup, health checks, and machine-pairing
smoke test—see
[`docs/operations/dashboard-deployment.md`](../../docs/operations/dashboard-deployment.md).
For the Render API/PostgreSQL path, see
[`docs/operations/render-deployment.md`](../../docs/operations/render-deployment.md).
The current Vercel project is the docs site and must not be repointed to this app.

## Local development

From `apps/dashboard`:

```sh
cp .env.example .env.local
npm run dev
```

Run `scripts/generate-dashboard-keypair.sh` once. Copy the private PEM into
`SCOPRA_API_JWT_PRIVATE_KEY` in `.env.local`; copy only the printed public-key
environment assignment into the root `.env` as
`SCOPRA_DASHBOARD_JWT_PUBLIC_KEY`. Set a strong `AUTH_SECRET` (for example,
`openssl rand -base64 32`) and your GitHub OAuth client ID/secret in `.env.local`.
The callback URL is `http://localhost:3000/api/auth/callback/github` locally
and `https://<dashboard-host>/api/auth/callback/github` in the deployed app.
Configure `SCOPRA_CONTROL_PLANE_URL` as the Caloris origin. Keep all secret
values server-side, out of Git, and out of chat.

When Caloris is reachable, the signed-in page lists the user's machines and
workspaces and refreshes presence every 20 seconds. Pairing requires a one-time
code produced by `scopra init`; the dashboard inspects the code, displays the
machine fingerprint and exact workspace path, and requires explicit approval.

For the CLI's local development flow, export `SCOPRA_API_URL` and
`SCOPRA_DASHBOARD_URL` in the shell where `scopra init` runs. The Go CLI does
not read `.env` files. The API and dashboard origins can also be passed as
`--api-url` and `--dashboard-url` flags. Local HTTP is accepted only for
loopback development; deployed origins must use HTTPS.
