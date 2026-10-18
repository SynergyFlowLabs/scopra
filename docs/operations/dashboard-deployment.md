# Deploying the Scopra dashboard and Caloris

This runbook deploys the **private-alpha** enrollment and presence service:

- the Next.js dashboard runs as a separate Vercel project;
- Caloris runs as one long-lived Docker Compose service behind Caddy-managed TLS;
- PostgreSQL is a separate managed service with TLS enabled;
- Mio connects outbound to Caloris and sends signed presence heartbeats.

The existing Vercel project `scopra-six.vercel.app` remains the static docs site. Do not change its root directory or `vercel.json`. This is not a production-security approval: remote command execution, grants, revocation UI, audit, multi-replica rate limiting, and live operational restore tests are not part of this increment. Do not use it for sensitive machines.

## 1. Prerequisites

- A Docker host with Docker Compose v2, a public DNS name for the API, inbound TCP 80/443, outbound access to the managed PostgreSQL endpoint, and a stable outbound IP if the database uses an IP allowlist.
- A PostgreSQL database and least-privilege application user. Require TLS and certificate verification, and enable automated backups with a tested restore process.
- A stable dashboard hostname, for example `dashboard.example.com`, and an API hostname, for example `api.example.com`.
- A GitHub OAuth App. Set its callback URL exactly to `https://dashboard.example.com/api/auth/callback/github`.
- A Vercel project separate from the docs project.

DNS for the API hostname must point to the Docker host before Caddy requests its TLS certificate. Permit the host firewall to accept TCP 80/443 (and UDP 443 for HTTP/3). Do not expose PostgreSQL or the Caloris port directly to the public internet.

## 2. Generate the dashboard-to-Caloris keypair

On a trusted operator machine with OpenSSL 1.1.1 or newer:

```sh
./scripts/generate-dashboard-keypair.sh
```

The script creates a new owner-only directory (default `~/.config/scopra/dashboard-keys`) and two mode-0600 PEM files. It never prints the private key and refuses to overwrite an existing directory. Its final output includes a single-line, escaped **public** environment assignment that is safe to copy into Caloris' `.env.production`. Back up the private key in the organization's approved secret manager; do not commit it or paste it into chat.

Configure `dashboard-jwt-private.pem` as the Vercel production environment variable `SCOPRA_API_JWT_PRIVATE_KEY`. Configure the matching `dashboard-jwt-public.pem` as `SCOPRA_DASHBOARD_JWT_PUBLIC_KEY` for Caloris. Preserve the PEM newlines; Caloris and the dashboard also accept PEM text with literal `\\n` separators. Never swap the two keys or use a different key in the two services.

Generate the Auth.js secret separately, for example with `openssl rand -base64 32`. It is not the Ed25519 key and must be configured only as `AUTH_SECRET` in Vercel.

## 3. Deploy Caloris and TLS

Create the production environment file, secure it, and fill it using the managed database's TLS connection string and the **public** Ed25519 key:

```sh
cp deploy/control-plane/env.production.example deploy/control-plane/.env.production
chmod 600 deploy/control-plane/.env.production
$EDITOR deploy/control-plane/.env.production
```

Set `SCOPRA_API_DOMAIN` to the API's public hostname and `SCOPRA_DATABASE_URL` to the managed PostgreSQL URL. Omit `ssl`, `sslmode`, `sslcert`, `sslkey`, `sslrootcert`, `sslnegotiation`, and `uselibpqcompat` query parameters: Caloris configures TLS with certificate verification itself and rejects connection strings that try to override it. Set `SCOPRA_DASHBOARD_JWT_PUBLIC_KEY` to the public PEM; a single-line value with literal `\\n` separators is supported. Do not put the private key, GitHub secret, or Auth.js secret in this file. The `.env.production` path is ignored by Git.

Deploy from the repository root:

```sh
docker compose \
  --env-file deploy/control-plane/.env.production \
  -f deploy/control-plane/compose.yaml \
  up -d --build
```

The Compose stack builds the Caloris image, starts it as the image's non-root `node` user with a read-only root filesystem, drops Linux capabilities, applies memory/CPU/PID limits, and publishes it only to the internal Compose network. Caddy is the only public container; it obtains and renews HTTPS certificates and proxies to Caloris. Database migrations run under a PostgreSQL advisory lock when Caloris starts. A failed migration or database health check keeps Caloris unhealthy.

Verify deployment:

```sh
docker compose --env-file deploy/control-plane/.env.production \
  -f deploy/control-plane/compose.yaml ps
curl --fail --show-error https://api.example.com/healthz
```

`/healthz` should return `{"status":"ok"}` only when PostgreSQL is reachable. Inspect logs without enabling request-body logging:

```sh
docker compose --env-file deploy/control-plane/.env.production \
  -f deploy/control-plane/compose.yaml logs --tail=100 control-plane caddy
```

Use one Caloris replica for this alpha. Rate limits are in-memory and keyed by the socket peer; behind Caddy, that means they are instance/proxy scoped. Nonce replay checks are persisted in PostgreSQL. Horizontal scaling needs shared rate-limit state and an explicit trusted-proxy design first.

## 4. Configure the separate Vercel dashboard project

Create a new Vercel project connected to `SynergyFlowLabs/scopra`; **do not reuse the docs project**. Set:

- **Project name:** `scopra-dashboard` (or another distinct name)
- **Root Directory:** `apps/dashboard`
- **Framework:** Next.js
- **Node.js version:** 22.x
- **Include source files outside the Root Directory:** enabled (the npm workspace lockfile is at repository root)
- **Production branch:** the reviewed release branch merged to `main`

Keep the repository-root npm workspace lockfile available to the project build. Use Vercel's detected install/build commands unless the build log identifies a concrete workspace issue; the checked command from the repository root is `npm run build --workspace=@scopra/dashboard`. Do not add dashboard rewrites to the root docs project's `vercel.json`.

Add these **Production** environment variables in the Vercel project settings (mark secrets encrypted/sensitive):

| Name | Value |
|---|---|
| `AUTH_URL` | `https://dashboard.example.com` |
| `AUTH_SECRET` | A new random value from `openssl rand -base64 32` |
| `AUTH_GITHUB_ID` | GitHub OAuth App client ID |
| `AUTH_GITHUB_SECRET` | GitHub OAuth App client secret |
| `SCOPRA_CONTROL_PLANE_URL` | `https://api.example.com` |
| `SCOPRA_API_JWT_PRIVATE_KEY` | Contents of `dashboard-jwt-private.pem` |

Set the dashboard's stable domain in Vercel and confirm the OAuth callback URL matches it exactly. Do not enable production pairing until `/healthz` is healthy and all six variables are present. Preview deployments need their own OAuth callback strategy; do not copy production secrets to untrusted preview branches. A practical alpha setup is to configure OAuth and secrets only for Production and keep Preview limited to build checks.

After configuration, deploy the reviewed branch from the Vercel dashboard or the connected Git integration. Confirm the dashboard responds over HTTPS and that GitHub sign-in completes before inviting users. The docs project remains independently deployed at `https://scopra-six.vercel.app`.

## 5. Enroll and verify a machine

On a Linux machine with Mio running, configure the CLI with the deployed origins and enroll one workspace:

```sh
export SCOPRA_API_URL=https://api.example.com
export SCOPRA_DASHBOARD_URL=https://dashboard.example.com
scopra init /absolute/path/to/workspace
```

Sign in to the dashboard with the intended GitHub account, paste the one-time code, verify the exact machine fingerprint and workspace path, and explicitly approve. The CLI should report successful pairing. Mio sends its first signed heartbeat immediately and then every 30 seconds; the dashboard should show the machine online within 90 seconds. Repeat `scopra init` for another workspace on the same machine to add it to the existing machine identity.

This release has no remote command endpoint. Pairing and presence do not authorize execution.

## 6. Operations and rollback

- Keep PostgreSQL backups, TLS, Vercel secrets, the private assertion key, and Docker-host access under the organization's normal secret/access controls.
- Rotate the assertion key only as a coordinated change: update the Caloris public key and Vercel private key together, then restart/redeploy both services. The current version has no overlapping-key rotation window.
- Monitor `/healthz`, container restarts, disk space, database availability, and certificate renewal. Never log pairing request bodies or full workspace paths.
- Before changing the migration or container image, take a database backup. Roll back to the previously tested API image and Vercel deployment; do not roll back a schema migration without a reviewed database recovery procedure.
- The control-plane tests use a fake store. Exercise a staging deployment with a real PostgreSQL instance, real OAuth app, TLS, and a restore before treating any production deployment as ready for sensitive use.
