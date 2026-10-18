import { auth, signIn, signOut } from "../auth";
import { MachineInventory } from "../components/machine-inventory";
import { ControlPlaneError, listMachines, type MachineView } from "../lib/control-plane";

export const dynamic = "force-dynamic";

const githubConfigured = Boolean(
  process.env.AUTH_GITHUB_ID && process.env.AUTH_GITHUB_SECRET && process.env.AUTH_SECRET,
);

function Brand() {
  return (
    <a className="brand" href="/" aria-label="Scopra Dashboard home">
      <span className="brand-mark" aria-hidden="true"><span /></span>
      <span>scopra<span className="brand-light">/</span></span>
    </a>
  );
}

function GithubMark() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" className="github-mark">
      <path fill="currentColor" d="M12 .9a11.1 11.1 0 0 0-3.51 21.63c.55.1.76-.24.76-.53v-2.08c-3.1.68-3.76-1.32-3.76-1.32-.5-1.3-1.24-1.65-1.24-1.65-1.02-.7.08-.69.08-.69 1.13.08 1.73 1.17 1.73 1.17 1 .1.77 2.56 3.57 1.82v-1.32c-2.52-.28-5.17-1.26-5.17-5.6 0-1.24.44-2.25 1.17-3.04-.12-.29-.5-1.44.11-3 0 0 .96-.31 3.13 1.16a10.9 10.9 0 0 1 5.7 0c2.17-1.47 3.13-1.16 3.13-1.16.62 1.56.23 2.71.11 3 .73.79 1.17 1.8 1.17 3.04 0 4.35-2.65 5.31-5.18 5.59v2.03c0 .29.2.63.77.52A11.1 11.1 0 0 0 12 .9Z" />
    </svg>
  );
}

function SignIn() {
  if (!githubConfigured) {
    return (
      <div className="setup-notice" role="status">
        <span className="notice-dot" />
        <span>GitHub sign-in is not configured for this deployment.</span>
      </div>
    );
  }
  return (
    <form action={async () => {
      "use server";
      await signIn("github", { redirectTo: "/" });
    }}>
      <button className="button button-primary button-wide" type="submit">
        <GithubMark /> Continue with GitHub <span className="button-arrow" aria-hidden="true">↗</span>
      </button>
    </form>
  );
}

function SignOut() {
  return (
    <form action={async () => {
      "use server";
      await signOut({ redirectTo: "/" });
    }}>
      <button className="avatar-button" type="submit" aria-label="Sign out" title="Sign out"><span className="avatar-fallback">↗</span></button>
    </form>
  );
}

function LoginPage() {
  return (
    <main className="login-layout">
      <div className="login-top"><Brand /><span className="top-label">SECURE WORKSPACE ACCESS</span></div>
      <section className="login-main" aria-labelledby="welcome-title">
        <div className="eyebrow"><span className="eyebrow-line" /> YOUR MACHINES, CONNECTED</div>
        <h1 id="welcome-title">Your terminal.<br /><span>On your terms.</span></h1>
        <p className="login-copy">Pair your Scopra machine and workspaces. Sign in to see live presence and approve each workspace from one secure place.</p>
        <SignIn />
        <p className="login-privacy"><span aria-hidden="true">◈</span> Your workspace stays on your machine. Scopra never opens an inbound port.</p>
      </section>
      <aside className="login-aside" aria-label="One-time pairing flow preview">
        <div className="orbit orbit-one" /><div className="orbit orbit-two" />
        <div className="aside-badge"><span className="pulse" /> ONE-TIME PAIRING FLOW</div>
        <div className="terminal-card">
          <div className="terminal-bar"><div className="traffic"><i /><i /><i /></div><span>scopra · mio</span><span className="terminal-lock">⌑</span></div>
          <div className="terminal-body">
            <div className="terminal-line"><span className="prompt">$</span> scopra init</div>
            <div className="terminal-spacer" />
            <div className="terminal-status"><span className="status-check">✓</span><span>Workspace registered</span><span className="status-done">done</span></div>
            <div className="terminal-status"><span className="status-check">✓</span><span>Machine key created</span><span className="status-done">done</span></div>
            <div className="terminal-status terminal-pending"><span className="status-wait">◌</span><span>Waiting for approval</span><span className="status-live">live</span></div>
            <div className="terminal-rule" />
            <div className="terminal-footer"><span className="terminal-code-label">PAIRING CODE</span><span className="terminal-code">•••• •••• ••••</span></div>
          </div>
        </div>
        <p className="aside-caption">A one-time code. A deliberate confirmation.<br />Nothing is trusted by default.</p>
        <div className="aside-bottom"><span>SCOPRA CALORIS</span><span>01 — 03</span></div>
      </aside>
      <footer className="login-footer"><span>© {new Date().getFullYear()} Scopra</span><span>Open source · MIT licensed</span><a href="https://scopra-six.vercel.app/security/" rel="noreferrer">Security</a></footer>
    </main>
  );
}

export default async function Home() {
  const session = githubConfigured ? await auth() : null;
  if (!session?.user) return <LoginPage />;

  const displayName = session.user.name?.trim() || session.user.email || "Scopra user";
  const initial = displayName.slice(0, 1).toUpperCase();
  let initialMachines: MachineView[] = [];
  let controlPlaneError: string | null = null;
  try {
    initialMachines = await listMachines(session.user.id);
  } catch (error) {
    controlPlaneError = error instanceof ControlPlaneError ? error.message : "Control plane is unavailable.";
  }
  const controlPlaneAvailable = controlPlaneError === null;
  const workspaceCount = initialMachines.reduce((count, machine) => count + machine.workspaces.length, 0);
  const pairingEnabled = controlPlaneAvailable && Boolean(process.env.SCOPRA_API_JWT_PRIVATE_KEY);

  return (
    <main className="dashboard-layout">
      <aside className="sidebar">
        <Brand />
        <div className="sidebar-section-label">WORKSPACE</div>
        <nav className="sidebar-nav" aria-label="Main navigation">
          <a className="nav-item nav-item-active" href="#overview"><span className="nav-icon">◫</span> Overview</a>
          <a className="nav-item" href="#machines"><span className="nav-icon">⌘</span> Machines</a>
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-version"><span className="version-dot" /> PRIVATE ALPHA <span>0.1</span></div>
          <div className="sidebar-user"><span className="user-avatar">{initial}</span><span className="user-name">{displayName}</span><SignOut /></div>
        </div>
      </aside>
      <section className="dashboard-content" id="overview">
        <header className="dashboard-header">
          <div><span className="breadcrumb-muted">Workspace</span><span className="breadcrumb-separator">/</span><span>Overview</span></div>
          <div className="header-meta"><span className={`cloud-state ${controlPlaneAvailable ? "cloud-state-online" : "cloud-state-offline"}`}><span className="state-dot" /> {controlPlaneAvailable ? "Control plane online" : controlPlaneError}</span><span className="header-avatar">{initial}</span></div>
        </header>
        <div className="page-heading">
          <div><div className="eyebrow eyebrow-small">OVERVIEW <span className="heading-sep">/</span> PERSONAL SPACE</div><h1>Good to see you, {displayName.split(" ")[0]}.</h1><p>Pair a machine to bring your workspace into view.</p></div>
          <div className="heading-date">YOUR SCOPRA SPACE</div>
        </div>
        <div className="summary-grid">
          <article className="summary-card"><div className="summary-label">MACHINES</div><div className="summary-value">{controlPlaneAvailable ? initialMachines.length : "—"}</div><div className="summary-note">{controlPlaneAvailable ? "Paired to this account" : "Control plane unavailable"}</div><span className="summary-icon">⌘</span></article>
          <article className="summary-card"><div className="summary-label">WORKSPACES</div><div className="summary-value">{controlPlaneAvailable ? workspaceCount : "—"}</div><div className="summary-note">{controlPlaneAvailable ? "Across your machines" : "Presence not loaded"}</div><span className="summary-icon">▧</span></article>
          <article className="summary-card summary-card-accent"><div className="summary-label">SECURITY POSTURE</div><div className="summary-value summary-value-small">Default deny</div><div className="summary-note"><span className="good-dot" /> No remote execution enabled</div><span className="summary-icon">◈</span></article>
        </div>
        <div className="section-heading" id="machines"><div><h2>Your machines</h2><p>Machines connected to this account</p></div><span className={`live-pill ${controlPlaneAvailable ? "live-pill-online" : "live-pill-offline"}`}><span className="state-dot" /> {controlPlaneAvailable ? "LIVE PRESENCE" : "PRESENCE UNAVAILABLE"}</span></div>
        <MachineInventory initialMachines={initialMachines} initialError={controlPlaneError} pairingEnabled={pairingEnabled} />
        <footer className="dashboard-footer"><span>SCOPRA · SECURE EXECUTION, ON YOUR TERMS</span><span><a href="https://scopra-six.vercel.app/" rel="noreferrer">Documentation</a><i /> <a href="https://github.com/SynergyFlowLabs/scopra" rel="noreferrer">GitHub</a></span></footer>
      </section>
    </main>
  );
}
