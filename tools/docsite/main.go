package main

import (
	"flag"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// document is one Markdown file rendered as a page.
type document struct {
	// Source is the repository-relative path of the Markdown file.
	Source string
	// Slug is the site path under /docs, without ".html".
	Slug string
	// Title is the first level-one heading, or a readable file name.
	Title string
	// Group orders the page in the navigation.
	Group string
	Body  string
	// Headings are the section headings, used for the table of contents.
	Headings []Heading
}

type group struct {
	Name  string
	Title string
}

// groups fixes the navigation order and display names.
var groups = []group{
	{"overview", "Overview"},
	{"product", "Product"},
	{"architecture", "Architecture"},
	{"security", "Security"},
	{"decisions", "Decisions"},
	{"plan", "Planning"},
	{"api", "API"},
	{"operations", "Operations"},
	{"community", "Community"},
}

func groupTitle(name string) string {
	for _, known := range groups {
		if known.Name == name {
			return known.Title
		}
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// rootDocuments maps the repository's top-level Markdown files to a slug and
// group. Files not listed here are not published (progress.md, for example, is
// an engineering log that is better read in the repository).
var rootDocuments = []struct {
	Source string
	Slug   string
	Group  string
	Title  string
}{
	{"README.md", "overview", "overview", "Overview"},
	{"PRD.md", "prd", "product", "Product Requirements"},
	{"docs/product/alpha-scope.md", "product/alpha-scope", "product", "Alpha Scope"},
	{"docs/product/naming.md", "product/naming", "product", "Naming"},
	{"docs/architecture/system-overview.md", "architecture/system-overview", "architecture", "System Overview"},
	{"docs/architecture/domain-model.md", "architecture/domain-model", "architecture", "Domain Model"},
	{"docs/architecture/protocol.md", "architecture/protocol", "architecture", "Protocol"},
	{"docs/security/threat-model.md", "security/threat-model", "security", "Threat Model"},
	{"docs/security/risk-register.md", "security/risk-register", "security", "Risk Register"},
	{"docs/security/security-architecture.md", "security/security-architecture", "security", "Security Architecture"},
	{"docs/plans/00-full-implementation-plan.md", "plan/implementation-plan", "plan", "Implementation Plan"},
	{"docs/api/mcp-tools.md", "api/mcp-tools", "api", "MCP Tools"},
	{"docs/operations/runbooks.md", "operations/runbooks", "operations", "Runbooks"},
	{"docs/operations/deployment.md", "operations/deployment", "operations", "Deploying the Site"},
	{"SECURITY.md", "community/security-policy", "community", "Security Policy"},
	{"CONTRIBUTING.md", "community/contributing", "community", "Contributing"},
	{"CODE_OF_CONDUCT.md", "community/code-of-conduct", "community", "Code of Conduct"},
	{"CHANGELOG.md", "changelog", "overview", "Changelog"},
}

// defaultBaseURL is the production URL Vercel assigns to the project.
const defaultBaseURL = "https://scopra-six.vercel.app"

// siteOptions controls one build.
type siteOptions struct {
	Root    string
	Out     string
	BaseURL string
	Static  string
}

func main() {
	root := flag.String("root", ".", "repository root")
	out := flag.String("out", "apps/docs-site/dist", "output directory")
	baseURL := flag.String("base-url", defaultBaseURL, "canonical site URL")
	static := flag.String("static", "apps/docs-site/static", "static assets directory")
	flag.Parse()

	options := siteOptions{Root: *root, Out: *out, BaseURL: *baseURL, Static: *static}
	if err := buildSite(options); err != nil {
		fmt.Fprintln(os.Stderr, "docsite:", err)
		os.Exit(1)
	}
}

func buildSite(options siteOptions) error {
	documents, err := collectDocuments(options.Root)
	if err != nil {
		return err
	}
	bySlug := make(map[string]*document, len(documents))
	bySource := make(map[string]*document, len(documents))
	for index := range documents {
		bySlug[documents[index].Slug] = &documents[index]
		bySource[documents[index].Source] = &documents[index]
	}
	for index := range documents {
		source := filepath.Join(options.Root, documents[index].Source)
		content, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read %s: %w", documents[index].Source, err)
		}
		resolve := func(destination string) string {
			return resolveLink(documents[index].Source, destination, bySource)
		}
		body, headings, err := RenderMarkdown(content, resolve)
		if err != nil {
			return fmt.Errorf("render %s: %w", documents[index].Source, err)
		}
		documents[index].Body = body
		documents[index].Headings = headings
	}

	if err := resetOutput(options.Root, options.Out); err != nil {
		return err
	}
	if err := os.MkdirAll(options.Out, 0o755); err != nil {
		return err
	}
	for index := range documents {
		doc := &documents[index]
		page := renderDocumentPage(doc, documents, options)
		target := filepath.Join(options.Out, "docs", filepath.FromSlash(doc.Slug)+".html")
		if err := writeFile(target, page); err != nil {
			return err
		}
	}
	if err := writeFile(filepath.Join(options.Out, "index.html"), renderLanding(options)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(options.Out, "docs", "index.html"), renderDocsIndex(documents, options)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(options.Out, "404.html"), renderNotFound(options)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(options.Out, "robots.txt"), renderRobots(options)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(options.Out, "sitemap.xml"), renderSitemap(documents, options)); err != nil {
		return err
	}
	if err := copyStatic(options.Static, options.Out); err != nil {
		return err
	}
	return nil
}

// resetOutput clears the output directory so a page that is no longer
// generated cannot survive in the published site. It refuses to remove the
// repository itself.
func resetOutput(root, out string) error {
	cleaned := filepath.Clean(out)
	if cleaned == "." || cleaned == ".." || cleaned == string(filepath.Separator) {
		return fmt.Errorf("refusing to clear output directory %q", out)
	}
	absoluteOut, err := filepath.Abs(cleaned)
	if err != nil {
		return err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if absoluteOut == absoluteRoot || strings.HasPrefix(absoluteRoot+string(filepath.Separator), absoluteOut+string(filepath.Separator)) {
		return fmt.Errorf("refusing to clear output directory %q: it contains the repository", out)
	}
	return os.RemoveAll(absoluteOut)
}

// collectDocuments reads the configured sources and derives each title.
func collectDocuments(root string) ([]document, error) {
	var documents []document
	for _, entry := range rootDocuments {
		content, err := os.ReadFile(filepath.Join(root, entry.Source))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Source, err)
		}
		documents = append(documents, document{
			Source: entry.Source,
			Slug:   entry.Slug,
			Group:  entry.Group,
			Title:  entry.Title,
			Body:   "",
		})
		_ = content
	}

	// The decision index and every ADR, in numeric order.
	decisions, err := filepath.Glob(filepath.Join(root, "docs", "decisions", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(decisions)
	// The index goes first in the navigation; the template is not a decision.
	var index *document
	for _, decision := range decisions {
		name := strings.TrimSuffix(filepath.Base(decision), ".md")
		switch name {
		case "README":
			entry := document{
				Source: path.Join("docs", "decisions", "README.md"),
				Slug:   "decisions",
				Group:  "decisions",
				Title:  plainText(documentTitle(decision, "Decision Records")),
			}
			index = &entry
			continue
		case "000-template":
			continue
		}
		documents = append(documents, document{
			Source: path.Join("docs", "decisions", name+".md"),
			Slug:   path.Join("decisions", name),
			Group:  "decisions",
			Title:  plainText(documentTitle(decision, name)),
		})
	}
	if index != nil {
		// Prepend so the index heads the Decisions group and the docs index.
		documents = append([]document{*index}, documents...)
	}
	return documents, nil
}

// documentTitle reads a document's title line, falling back to a stable name.
func documentTitle(path, fallback string) string {
	content, err := os.ReadFile(path)
	if err == nil {
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "# ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "# "))
			}
		}
	}
	return fallback
}

func classify(source string) string {
	dir := path.Dir(source)
	if dir == "." {
		return "overview"
	}
	parts := strings.Split(dir, "/")
	if len(parts) >= 2 && parts[0] == "docs" {
		return parts[1]
	}
	return parts[0]
}

// resolveLink maps a Markdown link to its site URL, the repository on GitHub,
// or leaves it alone for external targets.
func resolveLink(source, destination string, bySource map[string]*document) string {
	if destination == "" || strings.HasPrefix(destination, "#") {
		return destination
	}
	lower := strings.ToLower(destination)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") {
		return destination
	}
	target, fragment := destination, ""
	if index := strings.IndexByte(destination, '#'); index >= 0 {
		target, fragment = destination[:index], destination[index:]
	}
	if target == "" {
		return fragment
	}
	resolved := path.Clean(path.Join(path.Dir(source), target))
	if doc, ok := bySource[resolved]; ok {
		return "/docs/" + doc.Slug + ".html" + fragment
	}
	// Anything that is not published as a page points at the repository, so a
	// reader still reaches it (LICENSE, progress.md, fixtures, and so on).
	return repositoryBlobURL + resolved + fragment
}

const repositoryBlobURL = "https://github.com/SynergyFlowLabs/scopra/blob/main/"

func writeFile(target, content string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

func copyStatic(source, destination string) error {
	return filepath.WalkDir(source, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		return writeFile(target, string(content))
	})
}

// pageShell wraps content in the shared layout.
func pageShell(options siteOptions, title, description, canonical, body string) string {
	var out strings.Builder
	out.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	out.WriteString("<meta charset=\"utf-8\">\n")
	out.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&out, "<title>%s</title>\n", html.EscapeString(title))
	fmt.Fprintf(&out, "<meta name=\"description\" content=\"%s\">\n", html.EscapeString(description))
	fmt.Fprintf(&out, "<link rel=\"canonical\" href=\"%s\">\n", html.EscapeString(canonical))
	out.WriteString("<meta name=\"color-scheme\" content=\"dark light\">\n")
	out.WriteString("<meta name=\"theme-color\" content=\"#0b0e14\">\n")
	out.WriteString("<meta property=\"og:type\" content=\"website\">\n")
	fmt.Fprintf(&out, "<meta property=\"og:title\" content=\"%s\">\n", html.EscapeString(title))
	fmt.Fprintf(&out, "<meta property=\"og:description\" content=\"%s\">\n", html.EscapeString(description))
	fmt.Fprintf(&out, "<meta property=\"og:url\" content=\"%s\">\n", html.EscapeString(canonical))
	out.WriteString("<meta property=\"og:site_name\" content=\"Scopra\">\n")
	out.WriteString("<meta name=\"twitter:card\" content=\"summary\">\n")
	fmt.Fprintf(&out, "<link rel=\"icon\" href=\"/favicon.svg\" type=\"image/svg+xml\">\n")
	out.WriteString("<link rel=\"stylesheet\" href=\"/styles.css\">\n")
	out.WriteString("</head>\n<body>\n")
	out.WriteString(headerNav())
	out.WriteString(body)
	out.WriteString(footer(options))
	out.WriteString("</body>\n</html>\n")
	return out.String()
}

func headerNav() string {
	return `<a class="skip" href="#main">Skip to content</a>
<header class="site-header">
  <a class="brand" href="/"><span class="mark" aria-hidden="true"></span>Scopra</a>
  <nav aria-label="Main">
    <a href="/docs/">Docs</a>
    <a href="/docs/overview.html">Overview</a>
    <a href="https://github.com/SynergyFlowLabs/scopra" rel="noopener noreferrer">GitHub</a>
  </nav>
</header>
`
}

func footer(options siteOptions) string {
	return fmt.Sprintf(`<footer class="site-footer">
  <div class="footer-inner">
    <p><strong>Scopra</strong> — controlled execution for AI agents. Free and open source under the <a href="%sLICENSE">MIT License</a>.</p>
    <p class="muted">No pricing model, no telemetry, no account. Generated from the Markdown in <a href="https://github.com/SynergyFlowLabs/scopra" rel="noopener noreferrer">the repository</a>.</p>
  </div>
</footer>
`, repositoryBlobURL)
}

func renderDocumentPage(doc *document, documents []document, options siteOptions) string {
	canonical := strings.TrimSuffix(options.BaseURL, "/") + "/docs/" + doc.Slug + ".html"
	var body strings.Builder
	body.WriteString(`<div class="layout">`)
	body.WriteString(sidebar(doc, documents))
	body.WriteString(`<main id="main" class="content">`)
	body.WriteString(`<nav class="breadcrumb" aria-label="Breadcrumb"><a href="/docs/">Docs</a> <span aria-hidden="true">/</span> ` +
		html.EscapeString(groupTitle(doc.Group)) + `</nav>`)
	body.WriteString(`<article class="prose">`)
	body.WriteString(doc.Body)
	body.WriteString(`</article>`)
	body.WriteString(fmt.Sprintf(`<p class="edit-link"><a href="%s%s" rel="noopener noreferrer">Edit this page on GitHub</a></p>`,
		repositoryBlobURL, html.EscapeString(doc.Source)))
	body.WriteString(`</main>`)
	if toc := tableOfContents(doc.Headings); toc != "" {
		body.WriteString(`<aside class="toc" aria-label="On this page">` + toc + `</aside>`)
	}
	body.WriteString(`</div>`)
	return pageShell(options, doc.Title+" — Scopra", descriptionFor(doc), canonical, body.String())
}

func descriptionFor(doc *document) string {
	return doc.Title + " — Scopra documentation."
}

func tableOfContents(headings []Heading) string {
	var out strings.Builder
	open := false
	for _, heading := range headings {
		if heading.Level < 2 || heading.Level > 3 {
			continue
		}
		if !open {
			out.WriteString(`<p class="toc-title">On this page</p><ul>`)
			open = true
		}
		class := ""
		if heading.Level == 3 {
			class = ` class="toc-sub"`
		}
		fmt.Fprintf(&out, `<li%s><a href="#%s">%s</a></li>`, class, heading.ID, html.EscapeString(heading.Text))
	}
	if !open {
		return ""
	}
	out.WriteString(`</ul>`)
	return out.String()
}

// sidebar renders the grouped navigation, marking the current page.
func sidebar(current *document, documents []document) string {
	var out strings.Builder
	out.WriteString(`<nav class="sidebar" aria-label="Documentation">`)
	for _, known := range groups {
		var groupDocs []*document
		for index := range documents {
			if documents[index].Group == known.Name {
				groupDocs = append(groupDocs, &documents[index])
			}
		}
		if len(groupDocs) == 0 {
			continue
		}
		fmt.Fprintf(&out, `<p class="group">%s</p><ul>`, html.EscapeString(known.Title))
		for _, doc := range groupDocs {
			class := ""
			if doc.Slug == current.Slug {
				class = ` class="current" aria-current="page"`
			}
			fmt.Fprintf(&out, `<li><a href="/docs/%s.html"%s>%s</a></li>`,
				doc.Slug, class, html.EscapeString(shortTitle(doc)))
		}
		out.WriteString(`</ul>`)
	}
	out.WriteString(`</nav>`)
	return out.String()
}

// shortTitle trims an ADR's verbose title for the sidebar.
func shortTitle(doc *document) string {
	title := doc.Title
	if strings.HasPrefix(title, "ADR-") {
		if index := strings.Index(title, ":"); index > 0 {
			return title // keep "ADR-013: ..." whole; it is the identifier
		}
	}
	return title
}

func renderDocsIndex(documents []document, options siteOptions) string {
	var body strings.Builder
	body.WriteString(`<main id="main" class="content wide">`)
	body.WriteString(`<h1>Documentation</h1><p class="lede">Everything about Scopra, generated from the repository's Markdown at build time. The source of truth is always <a href="https://github.com/SynergyFlowLabs/scopra" rel="noopener noreferrer">the repository</a>.</p>`)
	for _, known := range groups {
		var groupDocs []*document
		for index := range documents {
			if documents[index].Group == known.Name {
				groupDocs = append(groupDocs, &documents[index])
			}
		}
		if len(groupDocs) == 0 {
			continue
		}
		fmt.Fprintf(&body, `<h2 id="%s">%s</h2><div class="cards">`, known.Name, html.EscapeString(known.Title))
		for _, doc := range groupDocs {
			fmt.Fprintf(&body,
				`<a class="card" href="/docs/%s.html"><strong>%s</strong><span class="muted">%s</span></a>`,
				doc.Slug, html.EscapeString(doc.Title), html.EscapeString(doc.Source))
		}
		body.WriteString(`</div>`)
	}
	body.WriteString(`</main>`)
	canonical := strings.TrimSuffix(options.BaseURL, "/") + "/docs/"
	return pageShell(options, "Documentation — Scopra", "Scopra documentation: product, architecture, security, decisions, and contributing.", canonical, body.String())
}

func renderRobots(options siteOptions) string {
	return fmt.Sprintf("User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", strings.TrimSuffix(options.BaseURL, "/"))
}

func renderSitemap(documents []document, options siteOptions) string {
	base := strings.TrimSuffix(options.BaseURL, "/")
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	out.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, location := range []string{"/", "/docs/"} {
		fmt.Fprintf(&out, "  <url><loc>%s%s</loc></url>\n", base, location)
	}
	for _, doc := range documents {
		fmt.Fprintf(&out, "  <url><loc>%s/docs/%s.html</loc></url>\n", base, doc.Slug)
	}
	out.WriteString("</urlset>\n")
	return out.String()
}

func renderNotFound(options siteOptions) string {
	body := `<main id="main" class="content wide">
  <h1>Page not found</h1>
  <p class="lede">That page does not exist. Try the <a href="/docs/">documentation index</a>, or the <a href="/">home page</a>.</p>
</main>`
	return pageShell(options, "Page not found — Scopra", "The page you requested does not exist.", strings.TrimSuffix(options.BaseURL, "/")+"/404.html", body)
}
