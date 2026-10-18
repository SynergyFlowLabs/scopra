# Scopra Caloris

Caloris is the containerized control-plane API for the first hosted enrollment
and presence increment. The Next.js dashboard runs separately on Vercel; Goldstone
remote command transport remains a later implementation step. This service
accepts device-signed enrollment/status/presence calls and short-lived Ed25519
assertions minted server-side by the dashboard. It does not accept browser
cookies, CORS requests, command execution, or arbitrary shell messages.

## Deployment

The private-alpha Docker Compose/Caddy deployment uses an external TLS-enabled
PostgreSQL database and keeps the API private behind HTTPS. Follow the runbook in
[`docs/operations/dashboard-deployment.md`](../../docs/operations/dashboard-deployment.md).
For Render, follow the Render-specific guide in
[`docs/operations/render-deployment.md`](../../docs/operations/render-deployment.md).
Do not expose port 8080 or PostgreSQL directly to the internet. Use one API
replica until rate limiting has shared state and trusted-proxy handling is added.

## Local development

1. Copy the repository `.env.example` to `.env`.
2. Run `scripts/generate-dashboard-keypair.sh`. Copy its public-key environment
   assignment into `.env`; put the private PEM only in `apps/dashboard/.env.local`.
   Configure GitHub OAuth credentials and a strong local `AUTH_SECRET` there.
   Do not commit either env file or paste secret values into chat.
3. Start PostgreSQL and Caloris with `docker compose up --build postgres control-plane`.
4. The health endpoint is `http://127.0.0.1:8080/healthz`; it is for local
   development only. The listener binds `0.0.0.0` inside the container, while
   Compose publishes it only on the host loopback interface.

The API binds to all interfaces inside its container and relies on the hosting
platform's TLS reverse proxy in production. Production rejects loopback database
addresses and SSL overrides in connection URLs. PostgreSQL TLS uses
certificate/hostname verification by default (`SCOPRA_DATABASE_TLS_MODE=verify-full`);
Render's private Postgres endpoint can opt into TLS without certificate
verification via the explicit `SCOPRA_DATABASE_TLS_MODE=require` setting.
Migrations are serialized with a PostgreSQL advisory lock and the API fails
closed if storage or signing-key configuration is missing.

## CLI enrollment and presence

For local development, run the Next.js dashboard at `http://127.0.0.1:3000`,
start Mio, then provide the CLI origins and enroll a workspace:

```sh
export SCOPRA_API_URL=http://127.0.0.1:8080
export SCOPRA_DASHBOARD_URL=http://127.0.0.1:3000
scopra init [PATH]
```

The CLI prints a five-minute, single-use code. After GitHub sign-in, paste it
into the dashboard, review the machine fingerprint and exact workspace path,
and explicitly confirm. The CLI polls device-signed enrollment status and
saves only owner-only pairing metadata; the code is not persisted. Mio detects
that metadata while running and sends signed presence heartbeats outbound every
30 seconds. On a real hosted deployment, set the CLI origins to the deployed
HTTPS API and Vercel dashboard URLs. `scopra init` does not execute remote
commands; remote relay remains deferred.

## Initial routes

- `POST /v1/enrollments` — device proof-of-possession; issues a 128-bit,
  five-minute pairing code once and persists only its SHA-256 hash.
- `POST /v1/enrollments/inspect` — authenticated dashboard inspection; returns
  machine fingerprint and exact workspace path for explicit confirmation.
- `POST /v1/enrollments/claim` — authenticated, atomic, single-use pairing.
- `POST /v1/enrollments/status` — fresh, nonce-protected device-signed status.
- `POST /v1/presence` — fresh, nonce-protected device heartbeat.
- `GET /v1/machines` — lists only machines owned by the authenticated GitHub ID.

Pairing codes are accepted only in request bodies and never in URLs. The API
never logs request bodies, pairing codes, workspace paths, or assertion tokens.
All responses are `no-store`; browser access is intended to go through
same-origin dashboard server routes.
