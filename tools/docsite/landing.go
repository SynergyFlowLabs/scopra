package main

import (
	"fmt"
	"strings"
)

// renderLanding builds the home page. It is hand-authored rather than derived
// from the README: the README is the repository's front door and this is the
// project's, and the two answer different questions.
func renderLanding(options siteOptions) string {
	var body strings.Builder
	body.WriteString(`<main id="main">
<section class="hero">
  <div class="hero-inner">
    <p class="eyebrow">Pre-alpha · Linux · MIT</p>
    <h1>Controlled execution<br>for AI agents</h1>
    <p class="lede">Give agents enough access to complete the task — never unlimited access to the machine. Scopra runs commands in an approved workspace, inside a rootless container, with limits, and never stores what ran.</p>
    <pre class="terminal install"><code>curl -fsSL https://scopra-six.vercel.app/install.sh | sh</code></pre>
    <p class="install-note">Linux · rootless Podman 4.0+ · systemd user session — the script verifies the release's SHA-256, then opens an interactive installer. Nothing changes until you confirm.</p>
    <div class="actions">
      <a class="button primary" href="/docs/operations/installation.html">Install guide</a>
      <a class="button" href="/docs/overview.html">Read the docs</a>
      <a class="button" href="https://github.com/SynergyFlowLabs/scopra" rel="noopener noreferrer">GitHub</a>
    </div>
  </div>
</section>

<section class="strip" aria-label="Properties">
  <p><strong>Free and open source</strong> · MIT, no pricing model</p>
  <p><strong>No telemetry</strong> · no account, no phone-home</p>
  <p><strong>Local-first</strong> · decisions run on your machine</p>
</section>

<section class="section">
  <h2>Why Scopra</h2>
  <div class="grid">
    <article class="feature">
      <h3>Least authority by default</h3>
      <p>An agent gets a workspace, not a shell. Network is off, the mount is read-only unless asked otherwise, the environment is empty, and every run is bounded.</p>
    </article>
    <article class="feature">
      <h3>Local enforcement</h3>
      <p>The decision and the sandbox live on the machine that runs the command. A cloud service can narrow authority; it cannot broaden it.</p>
    </article>
    <article class="feature">
      <h3>Structured, not stringly</h3>
      <p>A request is an executable plus arguments. No shell strings, no PTY, no native fallback, and a canonical hash that binds to the exact action.</p>
    </article>
    <article class="feature">
      <h3>Honest about data</h3>
      <p>The run journal records metadata only: which workspace, which state, how many bytes. Never the command line, never the output.</p>
    </article>
    <article class="feature">
      <h3>Fail closed</h3>
      <p>An unusable registry, journal, socket, or containment setup refuses the operation. Scopra never degrades into something less safe.</p>
    </article>
    <article class="feature">
      <h3>Small by construction</h3>
      <p>One Go module, no external dependencies, two binaries, and a static documentation site with no JavaScript at all.</p>
    </article>
  </div>
</section>

<section class="section alt">
  <h2>How it works</h2>
  <div class="split">
    <pre class="diagram"><code>  you / an agent
        │  scopra run …
        ▼
  ┌─────────────┐   authenticated    ┌─────────────┐
  │   scopra    │   Unix socket      │   scoprad   │
  │  (Mariner)  │ ─────────────────▶ │    (Mio)    │
  └─────────────┘                    └──────┬──────┘
                                            │ rootless Podman
                                            ▼
                                     ┌─────────────┐
                                     │   Hollow    │
                                     │   sandbox   │
                                     └─────────────┘</code></pre>
    <div>
      <p><strong>Mariner</strong> is the CLI: it resolves a workspace, builds the request, streams output, and returns the command's own exit status.</p>
      <p><strong>Mio</strong> is the daemon: it revalidates the workspace immediately before use, runs exactly one container, and records metadata about the result.</p>
      <p><strong>Hollow</strong> is the boundary: rootless Podman, no network, no capabilities, no-new-privileges, a read-only root, one bind mount, and explicit limits.</p>
    </div>
  </div>
</section>

<section class="section">
  <h2>Quick start</h2>
  <ol class="steps">
    <li>
      <h3>Check the host</h3>
      <pre><code>./bin/scopra doctor --sandbox-image alpine@sha256:&lt;digest&gt;</code></pre>
      <p>Doctor is read-only. It reports the platform, state directory, workspace registry, registered workspaces, Mio's endpoint, and whether rootless Podman meets the requirements.</p>
    </li>
    <li>
      <h3>Start Mio</h3>
      <pre><code>./bin/scoprad --sandbox-image alpine@sha256:&lt;digest&gt;</code></pre>
      <p>The image must already be in local storage and pinned by digest. Mio never pulls.</p>
    </li>
    <li>
      <h3>Run something</h3>
      <pre><code>./bin/scopra workspace add ~/src/project
./bin/scopra run --workspace &lt;id&gt; -- uname -a
./bin/scopra logs</code></pre>
    </li>
  </ol>
</section>

<section class="section alt">
  <h2>Status</h2>
  <p class="lede">Scopra is pre-alpha. This table is the same one the README keeps, and it is deliberately unflattering.</p>
  <div class="table-wrap"><table>
    <thead><tr><th>Area</th><th>State</th></tr></thead>
    <tbody>
      <tr><td>Protocol, IPC, workspace registry, journal, CLI</td><td><span class="pill ok">tested</span></td></tr>
      <tr><td>Hollow runner and <code>scopra run</code></td><td><span class="pill ok">tested against a fake Podman</span></td></tr>
      <tr><td>Real rootless Podman container execution</td><td><span class="pill warn">not verified</span></td></tr>
      <tr><td>Real systemd user session</td><td><span class="pill warn">not verified</span></td></tr>
      <tr><td>Policy, grants, approvals, audit chain</td><td><span class="pill muted">not implemented</span></td></tr>
      <tr><td>Enrollment, relay, cloud control plane, dashboard</td><td><span class="pill muted">not implemented</span></td></tr>
    </tbody>
  </table></div>
</section>

<section class="section">
  <h2>Security, stated plainly</h2>
  <div class="split">
    <ul class="checks">
      <li>Any process running as you can use the socket — exactly as it can already run commands.</li>
      <li>A local run is your own action. It grants nothing and approves nothing.</li>
      <li>Containment is Hollow's container, not systemd's sandbox.</li>
      <li>The journal is not an audit trail: it is owner-only, bounded, and you can delete it.</li>
      <li>No telemetry, no account, no license key, no feature gate.</li>
    </ul>
    <div>
      <p>Those limits are documented rather than hidden. The <a href="/docs/security/threat-model.html">threat model</a>, <a href="/docs/security/risk-register.html">risk register</a>, and <a href="/docs/security/security-architecture.html">security architecture</a> are part of the repository, and every design decision has an <a href="/docs/decisions.html">ADR</a>.</p>
      <p>To report a vulnerability, follow the <a href="/docs/community/security-policy.html">security policy</a>. Please do not open a public issue.</p>
    </div>
  </div>
</section>

<section class="section alt" id="install">
  <h2>Install, every way</h2>
  <p class="lede">The one-liner above is the short version. These are the same installer with the choices spelled out; every flag reaches <code>scopra install</code> unchanged.</p>
  <ol class="steps">
    <li>
      <h3>Interactive</h3>
      <pre><code>curl -fsSL https://scopra-six.vercel.app/install.sh | sh</code></pre>
      <p>The default: a live preflight, a directory field, an optional digest-pinned image, a systemd user-service question, progress, and a summary. If rootless Podman is missing it can install it for you, showing every command first. The install itself never runs <code>sudo</code> and never edits your shell profile.</p>
    </li>
    <li>
      <h3>Scripted, no interface</h3>
      <pre><code>curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --yes</code></pre>
      <p>The same install with one line per step, for configuration management and CI. The flags are the answers, so nothing has to be typed.</p>
    </li>
    <li>
      <h3>Pin a version</h3>
      <pre><code>curl -fsSL https://scopra-six.vercel.app/install.sh | SCOPRA_VERSION=v0.1.0-alpha.7 sh</code></pre>
      <p><a href="https://scopra-six.vercel.app/releases/latest.txt">/releases/latest.txt</a> names the current tag. Set <code>SCOPRA_VERSION</code> to repeat an exact install; every tag keeps its own directory of archives.</p>
    </li>
    <li>
      <h3>Choose the directory, skip the service</h3>
      <pre><code>curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --dir ~/.local/bin --no-service</code></pre>
      <p><code>--dir</code> defaults to <code>$XDG_BIN_HOME</code> when it is absolute, otherwise <code>~/.local/bin</code>. <code>--no-service</code> skips the systemd unit, then the installer checks Podman in the current process context where you will start <code>scoprad</code>.</p>
    </li>
    <li>
      <h3>Pin the sandbox image</h3>
      <pre><code>curl -fsSL https://scopra-six.vercel.app/install.sh | sh -s -- --sandbox-image debian@sha256:&lt;digest&gt;</code></pre>
      <p>Mio never pulls an image, so the digest must already be in local storage. Without one, the binaries are installed and every run is refused with <code>containment_unavailable</code>.</p>
    </li>
    <li>
      <h3>Verify the download yourself</h3>
      <pre><code>version=$(curl -fsSL https://scopra-six.vercel.app/releases/latest.txt)
base="https://scopra-six.vercel.app/releases/$version"
curl -fsSLO "$base/scopra_linux_amd64.tar.gz" -O "$base/checksums.txt"
grep " scopra_linux_amd64.tar.gz$" checksums.txt | sha256sum -c -
tar xzf scopra_linux_amd64.tar.gz &amp;&amp; ./scopra install</code></pre>
      <p>Same bytes, same check, without the bootstrap script. Use <code>arm64</code> on ARM. The checksum proves the archive is the published one; it does not prove who published it.</p>
    </li>
    <li>
      <h3>Build from source</h3>
      <pre><code>git clone https://github.com/SynergyFlowLabs/scopra
cd scopra &amp;&amp; make bootstrap &amp;&amp; make test
go build -o bin/scopra ./cmd/scopra
go build -o bin/scoprad ./cmd/scoprad</code></pre>
      <p>Go 1.26.9, one module, no external dependencies. The <a href="/docs/operations/installation.html">install guide</a> has the requirements, the exact files an install writes, and troubleshooting.</p>
    </li>
  </ol>
</section>

<section class="section">
  <h2>Start reading</h2>
  <div class="cards">
    <a class="card" href="/docs/overview.html"><strong>Overview</strong><span class="muted">What exists, what does not, and what is unverified</span></a>
    <a class="card" href="/docs/architecture/system-overview.html"><strong>Architecture</strong><span class="muted">How Mariner, Mio, and Hollow fit together</span></a>
    <a class="card" href="/docs/security/threat-model.html"><strong>Threat model</strong><span class="muted">Assets, boundaries, and what is out of scope</span></a>
    <a class="card" href="/docs/decisions.html"><strong>Decisions</strong><span class="muted">Every design decision, from module layout to licensing</span></a>
    <a class="card" href="/docs/community/contributing.html"><strong>Contributing</strong><span class="muted">Setup, conventions, and the review process</span></a>
    <a class="card" href="/docs/changelog.html"><strong>Changelog</strong><span class="muted">What changed, in Keep a Changelog form</span></a>
  </div>
</section>
</main>`)

	canonical := strings.TrimSuffix(options.BaseURL, "/") + "/"
	return pageShell(options,
		"Scopra — controlled execution for AI agents",
		"Scopra runs commands from AI agents in approved workspaces inside rootless containers, with limits and a metadata-only journal. Free and open source under the MIT License.",
		canonical, body.String())
}

// landingTitle is used by tests to assert the hero copy stays accurate.
const landingTitle = "Controlled execution for AI agents"

var _ = fmt.Sprintf
