package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is the checkout this package lives in.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Fatalf("repository root %s has no README.md: %v", root, err)
	}
	return root
}

func buildRealSite(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	out := t.TempDir()
	options := siteOptions{
		Root:    root,
		Out:     out,
		BaseURL: "https://scopra.example",
		Static:  filepath.Join(root, "apps", "docs-site", "static"),
	}
	if err := buildSite(options); err != nil {
		t.Fatalf("buildSite: %v", err)
	}
	return out
}

func TestBuildSitePublishesTheWholeDocumentationSet(t *testing.T) {
	out := buildRealSite(t)
	for _, file := range []string{
		"index.html",
		"404.html",
		"robots.txt",
		"sitemap.xml",
		"styles.css",
		"favicon.svg",
		"docs/index.html",
		"docs/overview.html",
		"docs/decisions.html",
		"docs/decisions/014-open-source-no-pricing.html",
		"docs/community/contributing.html",
		"docs/community/security-policy.html",
		"docs/security/threat-model.html",
	} {
		if _, err := os.Stat(filepath.Join(out, file)); err != nil {
			t.Errorf("missing %s: %v", file, err)
		}
	}
}

func TestBuildSiteSkipsTheEngineeringLog(t *testing.T) {
	out := buildRealSite(t)
	if _, err := os.Stat(filepath.Join(out, "docs", "progress.html")); err == nil {
		t.Fatal("progress.md should not be published")
	}
}

func TestBuildSiteHasNoBrokenInternalLinks(t *testing.T) {
	out := buildRealSite(t)
	pages, err := filepath.Glob(filepath.Join(out, "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	deep, err := filepath.Glob(filepath.Join(out, "docs", "**", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	pages = append(pages, deep...)
	if len(pages) < 30 {
		t.Fatalf("only %d pages built; the glob is probably wrong", len(pages))
	}

	ancestors := regexp.MustCompile(`(?:href|src)="([^"]+)"`)
	identifiers := regexp.MustCompile(`id="([^"]+)"`)
	ids := make(map[string]map[string]bool, len(pages))
	links := make(map[string][]string, len(pages))
	for _, page := range pages {
		content, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		ids[page] = map[string]bool{}
		for _, match := range identifiers.FindAllStringSubmatch(string(content), -1) {
			ids[page][match[1]] = true
		}
		for _, match := range ancestors.FindAllStringSubmatch(string(content), -1) {
			links[page] = append(links[page], match[1])
		}
	}

	for page, targets := range links {
		for _, target := range targets {
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "data:") {
				continue
			}
			path, fragment, _ := strings.Cut(target, "#")
			if path == "" {
				if fragment != "" && !ids[page][fragment] {
					t.Errorf("%s links to missing anchor #%s", shortName(out, page), fragment)
				}
				continue
			}
			resolved := path
			if strings.HasPrefix(resolved, "/") {
				resolved = filepath.Join(out, resolved)
			} else {
				resolved = filepath.Join(filepath.Dir(page), resolved)
			}
			if strings.HasSuffix(path, "/") {
				resolved = filepath.Join(resolved, "index.html")
			}
			info, err := os.Stat(resolved)
			if err != nil || info.IsDir() {
				t.Errorf("%s links to %s, which was not generated", shortName(out, page), target)
				continue
			}
			if fragment != "" && strings.HasSuffix(resolved, ".html") && !ids[resolved][fragment] {
				t.Errorf("%s links to %s, which has no anchor #%s", shortName(out, page), target, fragment)
			}
		}
	}
}

func TestBuildSiteLeavesNoUnrenderedMarkdown(t *testing.T) {
	out := buildRealSite(t)
	pages, err := filepath.Glob(filepath.Join(out, "docs", "*.html"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no pages to inspect: %v", err)
	}
	for _, page := range pages {
		content, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		if strings.Contains(text, "```") {
			t.Errorf("%s still contains a code fence marker", shortName(out, page))
		}
		if strings.Contains(text, "](/") || strings.Contains(text, "](https://") {
			t.Errorf("%s still contains an unrendered Markdown link", shortName(out, page))
		}
	}
}

// TestCommittedSiteIsUpToDate keeps apps/docs-site/public honest: the deployed
// artifact is committed, so a documentation change without a rebuild would
// otherwise ship stale pages.
func TestCommittedSiteIsUpToDate(t *testing.T) {
	root := repoRoot(t)
	committed := filepath.Join(root, "apps", "docs-site", "public")
	if _, err := os.Stat(filepath.Join(committed, "index.html")); err != nil {
		t.Fatalf("apps/docs-site/public is missing; run `make site` and commit it: %v", err)
	}
	fresh := t.TempDir()
	options := siteOptions{
		Root:    root,
		Out:     fresh,
		BaseURL: defaultBaseURL,
		Static:  filepath.Join(root, "apps", "docs-site", "static"),
	}
	if err := buildSite(options); err != nil {
		t.Fatalf("buildSite: %v", err)
	}
	generated, err := relativeFiles(fresh)
	if err != nil {
		t.Fatal(err)
	}
	published, err := relativeFiles(committed)
	if err != nil {
		t.Fatal(err)
	}
	for name := range generated {
		if _, ok := published[name]; !ok {
			t.Errorf("%s is generated but not committed", name)
		}
	}
	for name := range published {
		if _, ok := generated[name]; !ok {
			t.Errorf("%s is committed but no longer generated", name)
		}
	}
	for name := range generated {
		if _, ok := published[name]; !ok {
			continue
		}
		want, err := os.ReadFile(filepath.Join(fresh, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(committed, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got) {
			t.Errorf("%s is stale; run `make site` and commit the result", name)
		}
	}
}

// relativeFiles lists every regular file below root, keyed by slash path.
func relativeFiles(root string) (map[string]bool, error) {
	files := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = true
		return nil
	})
	return files, err
}

func TestBuildSiteUsesTheConfiguredCanonicalURL(t *testing.T) {
	out := buildRealSite(t)
	content, err := os.ReadFile(filepath.Join(out, "docs", "overview.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `<link rel="canonical" href="https://scopra.example/docs/overview.html">`) {
		t.Fatalf("canonical URL is wrong:\n%s", firstLines(string(content), 20))
	}
	if !strings.Contains(string(content), `<meta property="og:title"`) {
		t.Fatal("social metadata is missing")
	}
}

func TestBuildSiteRobotsAndSitemap(t *testing.T) {
	out := buildRealSite(t)
	robots, err := os.ReadFile(filepath.Join(out, "robots.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(robots), "Sitemap: https://scopra.example/sitemap.xml") {
		t.Fatalf("robots.txt: %s", robots)
	}
	sitemap, err := os.ReadFile(filepath.Join(out, "sitemap.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sitemap), "<loc>https://scopra.example/docs/decisions.html</loc>") {
		t.Fatalf("sitemap.xml does not list the decision index")
	}
}

func TestResolveLinkFallsBackToTheRepository(t *testing.T) {
	documents := []document{{Source: "README.md", Slug: "overview"}}
	bySource := map[string]*document{"README.md": &documents[0]}
	got := resolveLink("README.md", "LICENSE", bySource)
	want := "https://github.com/SynergyFlowLabs/scopra/blob/main/LICENSE"
	if got != want {
		t.Fatalf("resolveLink(LICENSE) = %q, want %q", got, want)
	}
	if got := resolveLink("README.md", "https://example.com", bySource); got != "https://example.com" {
		t.Fatalf("external link was rewritten to %q", got)
	}
	if got := resolveLink("README.md", "#status", bySource); got != "#status" {
		t.Fatalf("fragment was rewritten to %q", got)
	}
}

func shortName(root, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return name
}

func firstLines(text string, count int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > count {
		lines = lines[:count]
	}
	return strings.Join(lines, "\n")
}

func TestResetOutputRefusesDangerousPaths(t *testing.T) {
	root := t.TempDir()
	for _, out := range []string{"", ".", "..", "/", root} {
		if err := resetOutput(root, out); err == nil {
			t.Errorf("resetOutput(%q) succeeded, want a refusal", out)
		}
	}
	out := filepath.Join(root, "public")
	if err := resetOutput(root, out); err != nil {
		t.Fatalf("resetOutput(%q): %v", out, err)
	}
}

func TestPageTitlesArePlainText(t *testing.T) {
	out := buildRealSite(t)
	pages, err := filepath.Glob(filepath.Join(out, "docs", "*", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	titles := regexp.MustCompile(`<title>([^<]*)</title>`)
	for _, page := range pages {
		content, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		match := titles.FindStringSubmatch(string(content))
		if match == nil {
			t.Fatalf("%s has no title", shortName(out, page))
		}
		for _, marker := range []string{"`", "**", "]("} {
			if strings.Contains(match[1], marker) {
				t.Errorf("%s title %q contains Markdown %q", shortName(out, page), match[1], marker)
			}
		}
	}
}
