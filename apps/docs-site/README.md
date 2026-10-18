# docs-site

The public documentation site. It is a static site generated from the Markdown
already in this repository, so the website can never drift from the docs.

- **Generator:** `tools/docsite` (Go standard library only)
- **Styles:** `static/styles.css` (hand-written, no framework)
- **Published output:** `public/` (generated, committed, served as-is)
- **Host:** Vercel, project `scopra`, configured by `vercel.json` at the repository root

## Build locally

```console
$ make site
$ python3 -m http.server -d apps/docs-site/public 8080
```

Then open <http://localhost:8080>. There is nothing to install: the generator is
part of the module.

## Why the output is committed

Vercel's build image does not ship a Go toolchain, and this project will not add
a second language or download a compiler during a deploy. `public/` is therefore
built locally, reviewed like any other change, and served as a pure static
artifact: no build step, no framework detection, nothing to fail in production.

The output is a pure function of the Markdown, so the committed copy can be
verified. `TestCommittedSiteIsUpToDate` rebuilds the site and fails `go test ./...`
when `public/` no longer matches the docs. If you change any published Markdown,
run `make site` and commit the result.

## What is published

Every page in `docs/` except `progress.md` (an engineering log), plus the
top-level `README.md`, `PRD.md`, `CONTRIBUTING.md`, `CHANGELOG.md`,
`CODE_OF_CONDUCT.md`, and `SECURITY.md`. `docs/decisions/README.md` becomes the
decision index at `/docs/decisions.html`.

Additions are made in `rootDocuments` in `tools/docsite/main.go`; ADRs are picked
up automatically from `docs/decisions/*.md`.

## Conventions

- No JavaScript, no web fonts, no third-party assets. The only external request a
  page can make is a badge image from `img.shields.io`.
- Raw HTML in Markdown is escaped, never passed through.
- Every link is rewritten at build time: a published page links to its site URL,
  anything else links to the file on GitHub. If a link cannot be resolved, the
  build fails rather than shipping a dead end.
- Accessibility and security headers live in `vercel.json` (CSP with
  `script-src 'none'`, HSTS, `X-Frame-Options: DENY`, and so on).

See [`docs/operations/deployment.md`](../../docs/operations/deployment.md) for the
deployment and rollback procedure.
