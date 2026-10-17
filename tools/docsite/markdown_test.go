package main

import (
	"strings"
	"testing"
)

func render(t *testing.T, source string) string {
	t.Helper()
	body, _, err := RenderMarkdown([]byte(source), nil)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	return body
}

func TestRenderHeadingsGetStableAnchors(t *testing.T) {
	body, headings, err := RenderMarkdown([]byte("# Title Here\n\n## Steps & Notes\n\n## Steps & Notes\n\n#### Deep\n"), nil)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	want := []Heading{
		{1, "Title Here", "title-here"},
		{2, "Steps & Notes", "steps-notes"},
		{2, "Steps & Notes", "steps-notes-1"},
		{4, "Deep", "deep"},
	}
	if len(headings) != len(want) {
		t.Fatalf("headings = %#v, want %d entries", headings, len(want))
	}
	for index := range want {
		if headings[index] != want[index] {
			t.Errorf("heading %d = %#v, want %#v", index, headings[index], want[index])
		}
	}
	if !strings.Contains(body, `<h2 id="steps-notes-1">Steps &amp; Notes</h2>`) {
		t.Errorf("duplicate heading did not get a unique anchor:\n%s", body)
	}
}

func TestRenderEscapesRawHTML(t *testing.T) {
	body := render(t, `<script>alert(1)</script> and <img src=x onerror=y>`)
	if strings.Contains(body, "<script") || strings.Contains(body, "<img") {
		t.Fatalf("raw HTML was not escaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag:\n%s", body)
	}
}

func TestRenderFencedCodeIsLiteral(t *testing.T) {
	source := "```console\n$ scopra run -- <b>true</b>\n```\n"
	body := render(t, source)
	if !strings.Contains(body, `<pre><code class="language-console">`) {
		t.Fatalf("missing language class:\n%s", body)
	}
	if !strings.Contains(body, "&lt;b&gt;") || !strings.Contains(body, "$ scopra run -- ") {
		t.Fatalf("code was not escaped:\n%s", body)
	}
	if strings.Contains(body, "<b>") {
		t.Fatalf("markup leaked out of a code fence:\n%s", body)
	}
}

func TestRenderTableWithAlignmentAndCodePipes(t *testing.T) {
	source := "| Flag | Meaning |\n|:-----|--------:|\n| `a|b` | pipe |\n| plain | cell |\n"
	body := render(t, source)
	for _, fragment := range []string{
		"<thead><tr><th class=\"align-left\">Flag</th>",
		"<th class=\"align-right\">Meaning</th>",
		"<code>a|b</code>",
		"<td class=\"align-right\">cell</td>",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("missing %q in:\n%s", fragment, body)
		}
	}
}

func TestRenderNestedListsAndTasks(t *testing.T) {
	source := "- parent\n  - child one\n  - child two\n- [x] done\n- [ ] todo\n\n1. first\n2. second\n"
	body := render(t, source)
	for _, fragment := range []string{
		"<li>parent<ul>\n<li>child one</li>",
		`<input type="checkbox" checked disabled> done`,
		`<input type="checkbox" disabled> todo`,
		"<ol>",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("missing %q in:\n%s", fragment, body)
		}
	}
}

func TestRenderOrderedListHonoursStart(t *testing.T) {
	body := render(t, "3. third\n4. fourth\n")
	if !strings.Contains(body, `<ol start="3">`) {
		t.Fatalf("expected a start attribute:\n%s", body)
	}
}

func TestRenderBlockquoteAndRule(t *testing.T) {
	body := render(t, "> quoted **text**\n\n---\n")
	if !strings.Contains(body, "<blockquote>\n<p>quoted <strong>text</strong></p>\n</blockquote>") {
		t.Fatalf("blockquote rendered incorrectly:\n%s", body)
	}
	if !strings.Contains(body, "<hr>") {
		t.Fatalf("thematic break missing:\n%s", body)
	}
}

func TestRenderInlineEscapesUnderscoresInFileNames(t *testing.T) {
	body := render(t, "see docs/run_journal.md or scopra_run_log and _emphasis_\n")
	if !strings.Contains(body, "docs/run_journal.md") || !strings.Contains(body, "scopra_run_log") {
		t.Fatalf("intraword underscores must survive untouched:\n%s", body)
	}
	if !strings.Contains(body, "<em>emphasis</em>") {
		t.Fatalf("real emphasis was lost:\n%s", body)
	}
}

func TestRenderInlineCodeWins(t *testing.T) {
	body := render(t, "use `--reason \"why *not*\"` here\n")
	if !strings.Contains(body, `<code>--reason &#34;why *not*&#34;</code>`) {
		t.Fatalf("inline code rendered incorrectly:\n%s", body)
	}
}

func TestRenderLinksResolveThroughCallback(t *testing.T) {
	body, _, err := RenderMarkdown([]byte("[a](../docs/x.md#frag) [b](https://example.com)"), func(d string) string {
		return "RESOLVED(" + d + ")"
	})
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(body, `href="RESOLVED(../docs/x.md#frag)"`) {
		t.Errorf("resolver was not applied:\n%s", body)
	}
	if !strings.Contains(body, `rel="noopener noreferrer"`) {
		t.Errorf("external link is missing rel:\n%s", body)
	}
}

func TestRenderBadgeIsAnImageInsideALink(t *testing.T) {
	body := render(t, "[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)\n")
	want := `<a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT" loading="lazy"></a>`
	if !strings.Contains(body, want) {
		t.Fatalf("badge rendered incorrectly:\n%s", body)
	}
}

func TestRenderAutolink(t *testing.T) {
	body := render(t, "see <https://example.com/a?b=c> now\n")
	if !strings.Contains(body, `<a href="https://example.com/a?b=c" rel="noopener noreferrer">https://example.com/a?b=c</a>`) {
		t.Fatalf("autolink failed:\n%s", body)
	}
}

func TestRenderEmphasisSpansASoftBreak(t *testing.T) {
	body := render(t, "deployed as a **pure static\nartifact**. Done.\n")
	if !strings.Contains(body, "<strong>pure static artifact</strong>") {
		t.Fatalf("emphasis across a line break was lost:\n%s", body)
	}
}

func TestRenderHardBreak(t *testing.T) {
	body := render(t, "one  \ntwo\n")
	if !strings.Contains(body, "one<br>two") {
		t.Fatalf("hard break missing:\n%s", body)
	}
}

func TestRenderEmptyDocument(t *testing.T) {
	body, headings, err := RenderMarkdown(nil, nil)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if body != "" || len(headings) != 0 {
		t.Fatalf("empty document rendered as %q with %d headings", body, len(headings))
	}
}

func TestSlugFallsBackAndTrims(t *testing.T) {
	if got := slug("!!!"); got != "" {
		t.Errorf("slug(!!!) = %q, want empty", got)
	}
	if got := slug("  Hello, World!  "); got != "hello-world" {
		t.Errorf("slug = %q, want hello-world", got)
	}
}
