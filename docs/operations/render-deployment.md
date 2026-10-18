# Deploying Caloris to Render

This guide deploys the **Caloris API and PostgreSQL** to Render. The browser
Dashboard remains a separate Next.js project on Vercel; the repository's
existing Vercel project is the static documentation site. Do not repoint that
project. Remote command execution is not implemented.

## Before you create the service

Render builds from a GitHub commit, not from this working directory. The current
Arena changes must first be committed and pushed to
`arena/fe74ddf7-scopra`, or merged to `main`; otherwise Render will build older
source without the enrollment API. Do not select a branch until the pushed
commit contains `apps/control-plane`, `apps/dashboard`, `cmd/scopra/enroll.go`,
and `internal/presence`.

For an always-available presence API, choose a paid web-service plan. Render's
Free web services sleep after 15 minutes without requests, and the first request
after sleep can take about a minute to wake the service. Free Render Postgres
expires after 30 days. Free plans are suitable only for a temporary demo, not
for persistent machine enrollment.

## 1. Create PostgreSQL

1. In Render, choose **New → PostgreSQL**.
2. Name it `scopra-caloris-db`.
3. Choose **Oregon (US West)**, the same region as the API service.
4. Choose a non-expiring paid database plan for ongoing use; set the database
   name and user or accept Render's generated values.
5. Create it and wait until its status is **Available**.
6. From the database's **Connect** menu, copy the **Internal Database URL**.
   Use it only when the API and database are in the same Render account and
   region. Render's internal Postgres endpoint uses a self-signed certificate;
   Caloris therefore requires the explicit `SCOPRA_DATABASE_TLS_MODE=require`
   setting for this private-network connection. This keeps TLS mandatory and
   fails if the server refuses TLS, but it does not authenticate the
   certificate. The trust boundary is Render's private network plus the
   database credentials. Do not use this mode with a public database URL.
7. Under the database's **Info → Networking** settings, disable external
   access (clear its public IP allowlist) if you don't need to connect from
   outside Render. Internal same-region connections continue to work. Keep the
   database password private.

For an external Postgres URL instead, leave `SCOPRA_DATABASE_TLS_MODE` unset
(or set it to `verify-full`), use the Render-managed TLS hostname, and allowlist
the API service's outbound IP ranges under the database's **Info → Networking**.
Remove any `sslmode`/`ssl` query parameters from the URL: production rejects
URL-level TLS overrides and applies the explicit TLS mode itself. Never use
`sslmode=disable`.

## 2. Configure the Web Service form shown in the Render Dashboard

Create **New → Web Service** from the `SynergyFlowLabs/scopra` repository. On
the form in the screenshot, use these values:

| Field | Value |
|---|---|
| Name | `scopra-caloris` |
| Branch | `arena/fe74ddf7-scopra` after it is pushed, or `main` after merge |
| Region | `Oregon (US West)` — same as PostgreSQL |
| Language/runtime | **Node** (not Yarn; the project uses npm workspaces) |
| Root Directory | Leave blank (repository root; needed for root `package-lock.json`) |
| Build Command | `npm run control-plane:render-build` |
| Start Command | `npm run start --workspace=@scopra/control-plane` |
| Health Check Path | `/healthz` (under Advanced settings, if not on the first form) |
| Compute | Paid always-on plan for normal use; Free only for a short-lived demo |

Do **not** use the prefilled `yarn install; yarn build` or `yarn start` values.
Render receives `NODE_ENV=production` during the build, so plain `npm ci` omits
the workspace's development-only TypeScript and `@types/node` packages. The
`control-plane:render-build` script explicitly includes dev dependencies for
compilation, then prunes them from the runtime install. The root-level
`.node-version` pins Node 22.22.3. Render automatically sets `PORT`; the server
binds to that port and `0.0.0.0`. Do not set `SCOPRA_API_PORT`, because that
would override Render's assigned port.

## 3. Add Render environment variables

Before clicking **Deploy Web Service**, add these in the form's environment
variable section. For the signing key, run this on a trusted machine from the
repository:

```sh
./scripts/generate-dashboard-keypair.sh
```

The script writes an owner-only Ed25519 keypair. It prints a copy-ready public
key environment assignment but never prints the private key.

| Key | Value |
|---|---|
| `NODE_ENV` | `production` |
| `SCOPRA_API_HOST` | `0.0.0.0` |
| `SCOPRA_DATABASE_URL` | Render's **Internal Database URL** |
| `SCOPRA_DATABASE_TLS_MODE` | `require` (explicitly enables TLS for Render's self-signed private endpoint) |
| `SCOPRA_DASHBOARD_JWT_PUBLIC_KEY` | The public PEM value from the key-generation script |

Do not add `SCOPRA_API_PORT` or a manually chosen `PORT`. Render supplies the
port. Add these variables before the first deploy so startup migration and the
readiness check can succeed. The private PEM belongs only in Vercel as
`SCOPRA_API_JWT_PRIVATE_KEY`; never put it in Render or GitHub.

Now click **Deploy Web Service**. Watch **Events/Logs**. A healthy start ends
with `scopra-caloris: ready on 0.0.0.0:<port>`. Migrations run automatically.
If startup fails, check the logs for the generic configuration/database marker,
then verify the database URL, region/network allowlist, and PEM. Never paste a
connection string, database password, or private key into chat.

## 4. Verify the API

Copy the service's generated HTTPS URL from the Render service page, for
example `https://scopra-caloris-xxxx.onrender.com`. Visit:

```text
https://<render-service-host>/healthz
```

It must return HTTP 200 with `{"status":"ok"}`. A 503 means Caloris cannot
reach PostgreSQL. Check the Render logs and database allowlist; do not expose
the database or port 8080 publicly.

## 5. Deploy the dashboard separately to Vercel

1. Create a **new** Vercel project for `SynergyFlowLabs/scopra`; name it
   `scopra-dashboard`.
2. Set **Root Directory** to `apps/dashboard`, framework to Next.js, and Node
   to 22. Keep source files outside the root directory available so Vercel can
   read the root npm workspace lockfile. Leave the existing docs Vercel project
   untouched.
3. Use the new project’s stable `*.vercel.app` URL (or custom domain) as the
   dashboard URL. In GitHub **Settings → Developer settings → OAuth Apps**,
   create an OAuth App with **Homepage URL** `https://<dashboard-host>` and
   callback URL `https://<dashboard-host>/api/auth/callback/github`. Use the
   same production host for both Vercel's `AUTH_URL` and this OAuth app.
4. In Vercel's **Production** environment, add:

   | Key | Value |
   |---|---|
   | `AUTH_URL` | `https://<dashboard-host>` |
   | `AUTH_SECRET` | Fresh random secret (e.g. `openssl rand -base64 32`) |
   | `AUTH_GITHUB_ID` | GitHub OAuth App client ID |
   | `AUTH_GITHUB_SECRET` | GitHub OAuth App client secret |
   | `SCOPRA_CONTROL_PLANE_URL` | Render service origin, e.g. `https://scopra-caloris-xxxx.onrender.com` |
   | `SCOPRA_API_JWT_PRIVATE_KEY` | Contents of `dashboard-jwt-private.pem` from the key-generation script |

   Store secrets in Vercel's encrypted environment settings, never in Git. Use
   a separate OAuth app and non-production secrets for Preview environments;
   do not copy production secrets into untrusted branch previews.
5. Deploy the Vercel project and verify its HTTPS URL loads and GitHub sign-in
   succeeds.

## 6. Pair a machine and verify presence

The current public installer (`v0.1.0-alpha.8`) predates `scopra init`; it does
not contain the new enrollment/presence Go changes. Use a build of the updated
branch made with the repository's required Go 1.26.9 toolchain, or wait for a
new tested release, before attempting this step.

On a Linux machine with the updated CLI and Mio running:

```sh
export SCOPRA_API_URL=https://<render-service-host>
export SCOPRA_DASHBOARD_URL=https://<dashboard-host>
scopra init /absolute/path/to/workspace
```

Sign in to the Vercel dashboard, paste the one-time code, verify the displayed
fingerprint and exact workspace path, and confirm. Mio should report the machine
online in the dashboard within 90 seconds. This increment supports enrollment
and presence only; it does not execute remote commands.

## Render free-tier warning

Render Free web services spin down after 15 minutes without inbound requests;
the first subsequent request can take about a minute to wake them. This can
exceed the CLI's request timeout during enrollment. Free Render Postgres expires
after 30 days, after which the database must be upgraded within Render's grace
period or its data is deleted. Use paid, always-on compute and a persistent
Postgres plan for an ongoing alpha deployment.
