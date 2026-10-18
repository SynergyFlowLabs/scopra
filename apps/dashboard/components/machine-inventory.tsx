"use client";

import { useEffect, useState } from "react";
import type { MachineView, PairingInspection } from "../lib/control-plane";

type InventoryState = {
  machines: MachineView[];
  connected: boolean;
  message: string;
};

export function MachineInventory({
  initialMachines,
  initialError,
  pairingEnabled,
}: {
  initialMachines: MachineView[];
  initialError: string | null;
  pairingEnabled: boolean;
}) {
  const [inventory, setInventory] = useState<InventoryState>({
    machines: initialMachines,
    connected: initialError === null,
    message: initialError ?? "",
  });
  const [pairingOpen, setPairingOpen] = useState(false);
  const [pairingCode, setPairingCode] = useState("");
  const [inspection, setInspection] = useState<PairingInspection | null>(null);
  const [pairingMessage, setPairingMessage] = useState("");
  const [pairingError, setPairingError] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let active = true;
    let controller: AbortController | undefined;
    const refresh = async () => {
      controller?.abort();
      controller = new AbortController();
      try {
        const response = await fetch("/api/machines", {
          method: "GET",
          cache: "no-store",
          credentials: "same-origin",
          signal: controller.signal,
        });
        if (!response.ok) {
          const problem = await safeError(response);
          if (active) setInventory((current) => ({ ...current, connected: false, message: problem }));
          return;
        }
        const body = await response.json() as { machines?: unknown };
        if (!Array.isArray(body.machines)) throw new Error("invalid machine list");
        if (active) setInventory({ machines: body.machines as MachineView[], connected: true, message: "" });
      } catch (error) {
        if (active && !(error instanceof DOMException && error.name === "AbortError")) {
          setInventory((current) => ({ ...current, connected: false, message: "Could not refresh machine presence." }));
        }
      }
    };
    const timer = window.setInterval(() => { void refresh(); }, 20_000);
    return () => { active = false; controller?.abort(); window.clearInterval(timer); };
  }, []);

  async function inspect(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPairingError(false);
    setPairingMessage("");
    setInspection(null);
    const code = pairingCode.trim();
    if (!/^[A-Za-z0-9_-]{22}$/.test(code)) {
      setPairingError(true);
      setPairingMessage("Enter the 22-character code shown by Scopra.");
      return;
    }
    setBusy(true);
    try {
      const response = await fetch("/api/enrollments/inspect", {
        method: "POST",
        cache: "no-store",
        credentials: "same-origin",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ pairingCode: code }),
      });
      const payload = await response.json() as PairingInspection | { error?: { message?: string } };
      if (!response.ok || !("fingerprint" in payload)) throw new Error(errorMessage(payload, "Pairing code could not be inspected."));
      setInspection(payload);
    } catch (error) {
      setPairingError(true);
      setPairingMessage(error instanceof Error ? error.message : "Pairing code could not be inspected.");
    } finally {
      setBusy(false);
    }
  }

  async function confirmPairing() {
    if (!inspection) return;
    setBusy(true);
    setPairingError(false);
    setPairingMessage("");
    try {
      const response = await fetch("/api/enrollments/claim", {
        method: "POST",
        cache: "no-store",
        credentials: "same-origin",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          pairingCode: pairingCode.trim(),
          fingerprint: inspection.fingerprint,
          workspacePath: inspection.workspacePath,
        }),
      });
      const payload = await response.json() as { machineId?: string; error?: { message?: string } };
      if (!response.ok || typeof payload.machineId !== "string") throw new Error(errorMessage(payload, "Machine pairing could not be completed."));
      setPairingCode("");
      setInspection(null);
      setPairingMessage("Machine paired. Presence will update as Scopra Mio sends its next heartbeat.");
      setPairingError(false);
      const listResponse = await fetch("/api/machines", { cache: "no-store", credentials: "same-origin" });
      if (listResponse.ok) {
        const list = await listResponse.json() as { machines?: MachineView[] };
        if (Array.isArray(list.machines)) setInventory({ machines: list.machines, connected: true, message: "" });
      }
    } catch (error) {
      setPairingError(true);
      setPairingMessage(error instanceof Error ? error.message : "Machine pairing could not be completed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <section className="machine-panel" aria-label="Paired machines">
        {!inventory.connected ? (
          <div className="inventory-state" role="status">
            <span className="inventory-icon inventory-icon-warn">!</span>
            <div><h3>Machine presence unavailable</h3><p>{inventory.message || "The control plane is not connected."}</p></div>
          </div>
        ) : inventory.machines.length === 0 ? (
          <div className="inventory-state" role="status">
            <span className="inventory-icon">⌘</span>
            <div><h3>No machines paired</h3><p>Run <code>scopra init</code> in a workspace to get a one-time pairing code, then enter it here.</p></div>
          </div>
        ) : (
          <div className="machine-list">
            {inventory.machines.map((machine) => (
              <article className="machine-row" key={machine.id}>
                <div className="machine-symbol" aria-hidden="true">⌘</div>
                <div className="machine-details">
                  <div className="machine-name-line"><h3>{machine.machineName}</h3><span className={`presence-pill presence-${machine.presence}`}><i />{machine.presence}</span></div>
                  <p className="machine-fingerprint">Fingerprint <code>{machine.fingerprint}</code></p>
                  <div className="workspace-list">
                    {machine.workspaces.map((workspace) => (
                      <div className="workspace-row" key={workspace.id}><span className="workspace-glyph">▧</span><code title={workspace.path}>{workspace.path}</code></div>
                    ))}
                  </div>
                  <p className="machine-seen">{machine.lastSeenAt ? `Last seen ${formatRelative(machine.lastSeenAt)}` : "Waiting for first heartbeat"}</p>
                </div>
              </article>
            ))}
          </div>
        )}
        {inventory.connected && (
          <div className="inventory-footer"><span><i className="inventory-live-dot" /> Presence refreshes every 20 seconds</span><button className="text-button" onClick={() => setPairingOpen((open) => !open)} type="button">{pairingOpen ? "Close pairing" : "Pair a machine"}<span aria-hidden="true">{pairingOpen ? "−" : "+"}</span></button></div>
        )}
      </section>
      {pairingOpen && inventory.connected && (
        <section className="pairing-panel" aria-labelledby="pairing-title">
          <div className="pairing-heading"><div><span className="empty-kicker">ONE-TIME APPROVAL</span><h3 id="pairing-title">Pair a machine</h3><p>The code is single-use and expires after five minutes. It is never put in a URL.</p></div><span className="pairing-shield">◈</span></div>
          {!pairingEnabled && <div className="pairing-warning" role="status">The control-plane signing key is not configured for this dashboard deployment.</div>}
          <form className="pairing-form" onSubmit={inspect}>
            <label htmlFor="pairing-code">Pairing code</label>
            <div className="pairing-input-row"><input id="pairing-code" name="pairingCode" value={pairingCode} onChange={(event) => { setPairingCode(event.target.value); setInspection(null); setPairingMessage(""); }} autoComplete="off" autoCapitalize="off" spellCheck={false} inputMode="text" maxLength={22} placeholder="Paste the 22-character code" disabled={!pairingEnabled || busy} /><button className="button button-primary" type="submit" disabled={!pairingEnabled || busy}>{busy ? "Checking…" : "Inspect code"}</button></div>
          </form>
          {inspection && (
            <div className="pairing-inspection">
              <div className="inspection-heading"><span className="inspection-check">!</span><div><strong>Review before approving</strong><p>Pairing binds this machine and workspace to your signed-in GitHub account.</p></div></div>
              <dl><div><dt>Machine</dt><dd>{inspection.machineName}</dd></div><div><dt>Fingerprint</dt><dd><code>{inspection.fingerprint}</code></dd></div><div><dt>Workspace path</dt><dd><code className="path-value">{inspection.workspacePath}</code></dd></div><div><dt>Code expires</dt><dd>{new Date(inspection.expiresAt).toLocaleTimeString()}</dd></div></dl>
              <button className="button button-confirm" type="button" onClick={confirmPairing} disabled={busy}>Confirm pairing<span aria-hidden="true">→</span></button>
            </div>
          )}
          {pairingMessage && <p className={`pairing-message ${pairingError ? "pairing-message-error" : "pairing-message-ok"}`} role="status">{pairingMessage}</p>}
        </section>
      )}
    </>
  );
}

async function safeError(response: Response): Promise<string> {
  try {
    const value = await response.json() as { error?: { message?: unknown } };
    return typeof value.error?.message === "string" ? value.error.message : "Control plane is unavailable.";
  } catch { return "Control plane is unavailable."; }
}

function errorMessage(value: unknown, fallback: string): string {
  if (value && typeof value === "object" && "error" in value) {
    const error = (value as { error?: { message?: unknown } }).error;
    if (typeof error?.message === "string") return error.message;
  }
  return fallback;
}

function formatRelative(value: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - Date.parse(value)) / 1000));
  if (seconds < 60) return "just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86_400)}d ago`;
}
