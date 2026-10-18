import "server-only";
import { readDashboardConfig } from "./config";
import { BackendTokenError, createBackendAssertion } from "./backend-token";

const API_TIMEOUT_MS = 5_000;
const RESPONSE_LIMIT_BYTES = 64 * 1024;
const CONTROL_CHARACTERS = /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u;

export type WorkspaceView = {
  id: string;
  localWorkspaceId: string;
  path: string;
};

export type MachineView = {
  id: string;
  machineName: string;
  fingerprint: string;
  createdAt: string;
  lastSeenAt: string | null;
  presence: "online" | "offline";
  workspaces: WorkspaceView[];
};

export type PairingInspection = {
  enrollmentId: string;
  machineName: string;
  fingerprint: string;
  workspacePath: string;
  expiresAt: string;
};

export class ControlPlaneError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
  ) {
    super(message);
    this.name = "ControlPlaneError";
  }
}

export async function listMachines(githubSubject: string): Promise<MachineView[]> {
  const payload = await requestControlPlane("/v1/machines", githubSubject, "GET");
  if (!isRecord(payload) || !hasExactKeys(payload, ["machines"]) || !Array.isArray(payload.machines) || payload.machines.length > 100) {
    throw new ControlPlaneError("Control plane returned an invalid machine list", 502, "invalid_response");
  }
  return payload.machines.map(parseMachine);
}

export async function inspectPairing(githubSubject: string, pairingCode: string): Promise<PairingInspection> {
  const payload = await requestControlPlane("/v1/enrollments/inspect", githubSubject, "POST", { pairingCode });
  if (!isRecord(payload) || !hasExactKeys(payload, ["enrollmentId", "machineName", "fingerprint", "workspacePath", "expiresAt"]) ||
      typeof payload.enrollmentId !== "string" || !isUuid(payload.enrollmentId) ||
      typeof payload.machineName !== "string" || !safeText(payload.machineName, 128) ||
      typeof payload.fingerprint !== "string" || !/^[0-9a-f]{24}$/.test(payload.fingerprint) ||
      typeof payload.workspacePath !== "string" || !safePath(payload.workspacePath) ||
      typeof payload.expiresAt !== "string" || !Number.isFinite(Date.parse(payload.expiresAt))) {
    throw new ControlPlaneError("Control plane returned invalid enrollment details", 502, "invalid_response");
  }
  return payload as PairingInspection;
}

export async function claimPairing(
  githubSubject: string,
  input: { pairingCode: string; fingerprint: string; workspacePath: string },
): Promise<{ machineId: string; workspaceId: string }> {
  const payload = await requestControlPlane("/v1/enrollments/claim", githubSubject, "POST", input);
  if (!isRecord(payload) || !hasExactKeys(payload, ["machineId", "workspaceId"]) ||
      typeof payload.machineId !== "string" || !isUuid(payload.machineId) ||
      typeof payload.workspaceId !== "string" || !isUuid(payload.workspaceId)) {
    throw new ControlPlaneError("Control plane returned an invalid pairing result", 502, "invalid_response");
  }
  return { machineId: payload.machineId, workspaceId: payload.workspaceId };
}

async function requestControlPlane(
  path: "/v1/machines" | "/v1/enrollments/inspect" | "/v1/enrollments/claim",
  githubSubject: string,
  method: "GET" | "POST",
  body?: unknown,
): Promise<unknown> {
  let endpoint: URL;
  try {
    const config = readDashboardConfig();
    if (!config.controlPlaneUrl) throw new ControlPlaneError("Control plane is not configured", 503, "not_configured");
    endpoint = new URL(path, config.controlPlaneUrl);
  } catch (error) {
    if (error instanceof ControlPlaneError) throw error;
    throw new ControlPlaneError("Control plane configuration is invalid", 503, "not_configured");
  }

  let assertion: string;
  try { assertion = createBackendAssertion(githubSubject); }
  catch (error) {
    if (error instanceof BackendTokenError) throw new ControlPlaneError("Dashboard service authentication is not configured", 503, "not_configured");
    throw new ControlPlaneError("Dashboard service authentication is unavailable", 503, "not_configured");
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), API_TIMEOUT_MS);
  timeout.unref?.();
  let response: Response;
  try {
    response = await fetch(endpoint, {
      method,
      cache: "no-store",
      redirect: "error",
      signal: controller.signal,
      headers: {
        accept: "application/json",
        authorization: `Bearer ${assertion}`,
        ...(method === "POST" ? { "content-type": "application/json" } : {}),
      },
      ...(method === "POST" ? { body: JSON.stringify(body) } : {}),
    });
  } catch {
    throw new ControlPlaneError("Control plane is unavailable", 503, "upstream_unavailable");
  } finally {
    clearTimeout(timeout);
  }

  const encoded = await readLimited(response);
  if (!response.ok) {
    const code = readSafeErrorCode(encoded);
    const status = response.status;
    const message = status === 404 || status === 409 ? "Pairing code is invalid, expired, or already used." :
      status === 429 ? "Too many requests. Wait briefly and try again." :
      status === 401 || status === 403 ? "Sign in again before continuing." :
      status === 400 ? "The pairing request was not accepted." :
      "Control plane is unavailable.";
    throw new ControlPlaneError(message, status >= 400 && status < 600 ? status : 502, code);
  }
  if (response.status === 204) return null;
  try { return JSON.parse(encoded); }
  catch { throw new ControlPlaneError("Control plane returned an invalid response", 502, "invalid_response"); }
}

async function readLimited(response: Response): Promise<string> {
  const length = response.headers.get("content-length");
  if (length && (!/^\d+$/.test(length) || Number(length) > RESPONSE_LIMIT_BYTES)) {
    throw new ControlPlaneError("Control plane response exceeded the limit", 502, "invalid_response");
  }
  if (!response.body) return "";
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > RESPONSE_LIMIT_BYTES) {
        await reader.cancel();
        throw new ControlPlaneError("Control plane response exceeded the limit", 502, "invalid_response");
      }
      chunks.push(value);
    }
  } catch (error) {
    if (error instanceof ControlPlaneError) throw error;
    throw new ControlPlaneError("Control plane response could not be read", 502, "invalid_response");
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
  try { return new TextDecoder("utf-8", { fatal: true }).decode(bytes); }
  catch { throw new ControlPlaneError("Control plane returned invalid text", 502, "invalid_response"); }
}

function readSafeErrorCode(encoded: string): string {
  try {
    const value = JSON.parse(encoded) as unknown;
    if (!isRecord(value) || !isRecord(value.error) || typeof value.error.code !== "string") return "upstream_error";
    return /^[a-z_]{1,40}$/.test(value.error.code) ? value.error.code : "upstream_error";
  } catch { return "upstream_error"; }
}

function parseMachine(value: unknown): MachineView {
  if (!isRecord(value) || !hasExactKeys(value, ["id", "machineName", "fingerprint", "createdAt", "lastSeenAt", "presence", "workspaces"]) ||
      typeof value.id !== "string" || !isUuid(value.id) ||
      typeof value.machineName !== "string" || !safeText(value.machineName, 128) ||
      typeof value.fingerprint !== "string" || !/^[0-9a-f]{24}$/.test(value.fingerprint) ||
      typeof value.createdAt !== "string" || !Number.isFinite(Date.parse(value.createdAt)) ||
      !(value.lastSeenAt === null || (typeof value.lastSeenAt === "string" && Number.isFinite(Date.parse(value.lastSeenAt)))) ||
      (value.presence !== "online" && value.presence !== "offline") ||
      !Array.isArray(value.workspaces) || value.workspaces.length > 256) {
    throw new ControlPlaneError("Control plane returned invalid machine data", 502, "invalid_response");
  }
  const workspaces = value.workspaces.map((workspace) => {
    if (!isRecord(workspace) || !hasExactKeys(workspace, ["id", "localWorkspaceId", "path"]) ||
        typeof workspace.id !== "string" || !isUuid(workspace.id) ||
        typeof workspace.localWorkspaceId !== "string" || !/^[0-9a-f]{32}$/.test(workspace.localWorkspaceId) ||
        typeof workspace.path !== "string" || !safePath(workspace.path)) {
      throw new ControlPlaneError("Control plane returned invalid workspace data", 502, "invalid_response");
    }
    return { id: workspace.id, localWorkspaceId: workspace.localWorkspaceId, path: workspace.path };
  });
  return {
    id: value.id,
    machineName: value.machineName,
    fingerprint: value.fingerprint,
    createdAt: value.createdAt,
    lastSeenAt: value.lastSeenAt,
    presence: value.presence,
    workspaces,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, keys: readonly string[]): boolean {
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  return actual.length === expected.length && actual.every((key, index) => key === expected[index]);
}

function safeText(value: string, maxLength: number): boolean {
  return value.length > 0 && Buffer.byteLength(value, "utf8") <= maxLength && !CONTROL_CHARACTERS.test(value);
}

function safePath(value: string): boolean {
  return value.startsWith("/") && Buffer.byteLength(value, "utf8") <= 4095 && !CONTROL_CHARACTERS.test(value);
}

function isUuid(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}
