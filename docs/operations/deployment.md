# Deploying the Documentation Site

The public documentation site is deployed to Vercel as a **pure static
artifact**. There is no build step on Vercel, no framework detection, and no
Node dependency graph: the HTML in `apps/docs-site/public` is generated locally
from the repository's Markdown and committed.

- Production URL: <https://scopra-six.vercel.app>
- Vercel project: `scopra` (`prj_8IUlxMQr92jfileaMhwprRyR6daH`), team `Gulzaib's projects`
- Repository root directory: the repository root (`vercel.json` lives there)
- Output directory: `apps/docs-site/public`
- Framework preset: none (`null`)

## Why the artifact is committed

Vercel's build image does not include a Go toolchain, and Scopra will not add a
second language, a Node dependency tree, or a compiler download to a deploy. The
site is therefore a build-time artifact of this repository, produced by
`tools/docsite` (standard library only) and served byte-for-byte as reviewed.

The published HTML is a pure function of the Markdown: no timestamps, no commit
hash, no environment input. That makes the committed copy verifiable.
`TestCommittedSiteIsUpToDate` rebuilds the site into a temporary directory and
fails `go test ./...` when `apps/docs-site/public` no longer matches the docs.

## Deploy a change

```console
$ make site                 # regenerate apps/docs-site/public
$ go test ./...             # includes the up-to-date check
$ git add apps/docs-site/public
$ git commit -m "docs: rebrand the threat-model section"
$ git push
```

Vercel builds nothing: it serves the committed directory. A deployment for the
branch appears in the project dashboard and on the pull request.

Anything that changes a published Markdown file changes the site — including
`README.md`, `PRD.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `SECURITY.md`,
`CODE_OF_CONDUCT.md`, the ADRs, and `docs/**` except `progress.md`. Run
`make site` in the same commit.

## Configuration

`vercel.json` at the repository root sets:

| Setting | Value |
|---|---|
| `buildCommand` | a no-op `echo`: the output is prebuilt |
| `installCommand` | a no-op `echo`: there are no dependencies |
| `outputDirectory` | `apps/docs-site/public` |
| `cleanUrls` | `false`: pages keep their `.html` URLs, so links never redirect |
| `trailingSlash` | `false` |

Every response carries `Content-Security-Policy` (`script-src 'none'`,
`frame-ancestors 'none'`, `base-uri 'none'`), `Strict-Transport-Security`,
`X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: strict-origin-when-cross-origin`,
`Cross-Origin-Opener-Policy: same-origin`, and a `Permissions-Policy` that
disables every feature the site does not use. `styles.css` is always
revalidated; the favicon is cached for a day.

The site itself loads no JavaScript, no web fonts, and no third-party
stylesheets. The only external request is the badge images on the rendered
README, served from `img.shields.io` and allowed explicitly by the CSP.

## Verify a deployment

```console
$ curl -sSI https://scopra-six.vercel.app/docs/overview.html | head -20
$ curl -sS https://scopra-six.vercel.app/ | head -12
```

Check that `/`, `/docs/` and `/docs/decisions.html` return `200`, that a nested
page such as `/docs/decisions/014-open-source-no-pricing.html` renders, and that
the response carries the headers above. `/docs/` resolves to
`apps/docs-site/public/docs/index.html`.

## Roll back

Vercel keeps the ten most recent deployments. Open the project's deployment
list, choose the last known-good one, and promote it. Because the artifact is
committed, `git revert` of the offending commit and a fresh deploy produce the
same result.

## Changing the URL

If a custom domain replaces `scopra-six.vercel.app`, update `defaultBaseURL` in
`tools/docsite/main.go` and `SITE_BASE_URL` in the `Makefile`, then run
`make site` and commit the regenerated output. Canonical URLs, `sitemap.xml`,
and `robots.txt` all derive from that one value.
