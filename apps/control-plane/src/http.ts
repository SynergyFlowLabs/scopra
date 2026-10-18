import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import {
  CONTROL_PLANE_AUDIENCE,
  DASHBOARD_ISSUER,
  decodeDeviceSignature,
  deviceFingerprint,
  hashPairingCode,
  verifyDashboardAssertion,
  verifyDeviceSignature,
} from "./security.js";
import {
  ENROLLMENT_TTL_SECONDS,
  EnrollmentInputError,
  newPairingCode,
  normalizePairingCode,
  validateBeginEnrollment,
  validateEnrollmentId,
} from "./enrollment.js";
import { StoreConflictError, type ControlPlaneStore } from "./store.js";

const BODY_LIMIT_BYTES = 16 * 1024;
const DEVICE_CLOCK_SKEW_SECONDS = 60;
const DEVICE_NONCE_RETENTION_SECONDS = 180;
const RATE_WINDOW_MS = 60_000;
const RATE_LIMIT_PER_WINDOW = 60;
const PUBLIC_ENROLLMENT_RATE_LIMIT = 12;
const DEVICE_STATUS_RATE_LIMIT = 240;
const PRESENCE_RATE_LIMIT = 1200;

export type HttpOptions = {
  store: ControlPlaneStore;
  dashboardPublicKeyPEM: string;
  now?: () => Date;
};

export function createControlPlaneServer(options: HttpOptions): Server {
  const now = options.now ?? (() => new Date());
  const limiter = new RequestLimiter();
  return createServer((request, response) => {
    void handleRequest(request, response, options, now, limiter).catch(() => {
      if (!response.headersSent) sendError(response, 500, "internal_error", "request could not be completed");
      else response.destroy();
    });
  });
}

async function handleRequest(
  request: IncomingMessage,
  response: ServerResponse,
  options: HttpOptions,
  now: () => Date,
  limiter: RequestLimiter,
): Promise<void> {
  setSecurityHeaders(response);
  const method = request.method ?? "";
  let url: URL;
  try {
    url = new URL(request.url ?? "/", "http://control-plane.invalid");
  } catch {
    sendError(response, 400, "bad_request", "request URL is invalid");
    return;
  }
  if (url.search || url.hash) {
    sendError(response, 400, "bad_request", "query strings and fragments are not accepted");
    return;
  }

  if (method === "GET" && url.pathname === "/healthz") {
    try {
      await options.store.healthcheck();
      sendJson(response, 200, { status: "ok" });
    } catch {
      sendError(response, 503, "not_ready", "control plane storage is unavailable");
    }
    return;
  }

  const rateKey = request.socket.remoteAddress ?? "unknown";
  const limit = method === "POST" && url.pathname === "/v1/enrollments" ? PUBLIC_ENROLLMENT_RATE_LIMIT :
    method === "POST" && url.pathname === "/v1/enrollments/status" ? DEVICE_STATUS_RATE_LIMIT :
    method === "POST" && url.pathname === "/v1/presence" ? PRESENCE_RATE_LIMIT : RATE_LIMIT_PER_WINDOW;
  if (!limiter.allow(rateKey, limit, now().getTime())) {
    response.setHeader("Retry-After", "60");
    sendError(response, 429, "rate_limited", "too many requests");
    return;
  }

  if (method === "POST" && url.pathname === "/v1/enrollments") {
    const body = await readJsonObject(request, response);
    if (!body) return;
    try {
      const input = validateBeginEnrollment(body);
      const pairingCode = newPairingCode();
      const createdAt = now();
      const expiresAt = new Date(createdAt.getTime() + ENROLLMENT_TTL_SECONDS * 1000);
      const created = await options.store.beginEnrollment({
        requestId: input.requestId,
        publicKey: input.publicKeyBytes,
        fingerprint: input.fingerprint,
        machineName: input.machineName,
        localWorkspaceId: input.workspaceId,
        workspacePath: input.workspacePath,
        pairingCodeHash: hashPairingCode(pairingCode),
        createdAt,
        expiresAt,
      });
      sendJson(response, 201, { enrollmentId: created.enrollmentId, pairingCode, expiresAt: expiresAt.toISOString() });
    } catch (error) {
      if (error instanceof EnrollmentInputError) sendError(response, 400, "invalid_enrollment", error.message);
      else if (error instanceof StoreConflictError) sendError(response, 409, "enrollment_replayed", error.message);
      else sendError(response, 500, "internal_error", "enrollment could not be created");
    }
    return;
  }

  if (method === "POST" && (url.pathname === "/v1/enrollments/inspect" || url.pathname === "/v1/enrollments/claim")) {
    const principal = readDashboardPrincipal(request, response, options.dashboardPublicKeyPEM, now());
    if (!principal) return;
    const body = await readJsonObject(request, response);
    if (!body) return;
    if (url.pathname.endsWith("/inspect")) {
      if (!hasExactKeys(body, ["pairingCode"])) {
        sendError(response, 400, "invalid_request", "request fields are invalid");
        return;
      }
      try {
        const code = normalizePairingCode(body.pairingCode);
        const enrollment = await options.store.inspectEnrollment(hashPairingCode(code), now());
        if (!enrollment) {
          sendError(response, 404, "pairing_code_invalid", "pairing code is invalid or expired");
          return;
        }
        sendJson(response, 200, {
          enrollmentId: enrollment.enrollmentId,
          machineName: enrollment.machineName,
          fingerprint: enrollment.fingerprint,
          workspacePath: enrollment.workspacePath,
          expiresAt: enrollment.expiresAt.toISOString(),
        });
      } catch (error) {
        if (error instanceof EnrollmentInputError) sendError(response, 404, "pairing_code_invalid", "pairing code is invalid or expired");
        else sendError(response, 500, "internal_error", "enrollment could not be inspected");
      }
      return;
    }

    if (!hasExactKeys(body, ["pairingCode", "fingerprint", "workspacePath"])) {
      sendError(response, 400, "invalid_request", "request fields are invalid");
      return;
    }
    if (typeof body.fingerprint !== "string" || !/^[0-9a-f]{24}$/.test(body.fingerprint) ||
        typeof body.workspacePath !== "string") {
      sendError(response, 400, "invalid_request", "confirmation details are invalid");
      return;
    }
    try {
      const code = normalizePairingCode(body.pairingCode);
      const paired = await options.store.claimEnrollment({
        pairingCodeHash: hashPairingCode(code),
        githubSubject: principal.githubSubject,
        expectedFingerprint: body.fingerprint,
        expectedWorkspacePath: body.workspacePath,
        now: now(),
      });
      if (!paired) {
        sendError(response, 409, "pairing_changed", "pairing code is invalid, expired, already used, or the displayed details changed");
        return;
      }
      sendJson(response, 200, paired);
    } catch (error) {
      if (error instanceof EnrollmentInputError) sendError(response, 409, "pairing_changed", "pairing code is invalid or expired");
      else if (error instanceof StoreConflictError) sendError(response, 409, "machine_already_paired", "this machine is already paired");
      else sendError(response, 500, "internal_error", "machine could not be paired");
    }
    return;
  }

  if (method === "POST" && url.pathname === "/v1/enrollments/status") {
    const body = await readJsonObject(request, response);
    if (!body) return;
    if (!hasExactKeys(body, ["protocolVersion", "enrollmentId", "timestamp", "nonce", "signature"]) ||
        body.protocolVersion !== 1 || typeof body.enrollmentId !== "string" || !validateEnrollmentId(body.enrollmentId)) {
      sendError(response, 400, "invalid_request", "enrollment status request is invalid");
      return;
    }
    const signed = readDeviceProof(body, "enrollmentId", "scopra/device-enrollment-status/v1", now());
    if (!signed) {
      sendError(response, 401, "device_proof_invalid", "device proof is invalid");
      return;
    }
    const publicKey = await options.store.enrollmentPublicKey(body.enrollmentId);
    if (!publicKey || !verifyDeviceSignature(publicKey, signed.signature, signed.message)) {
      sendError(response, 401, "device_proof_invalid", "device proof is invalid");
      return;
    }
    const fingerprint = deviceFingerprint(publicKey);
    const requestNow = now();
    const replayAccepted = await options.store.consumeNonce(
      fingerprint,
      signed.nonce,
      new Date(requestNow.getTime() + DEVICE_NONCE_RETENTION_SECONDS * 1000),
      requestNow,
    );
    if (!replayAccepted) {
      sendError(response, 409, "device_request_replayed", "device request nonce was already used");
      return;
    }
    const status = await options.store.enrollmentStatus(body.enrollmentId, publicKey, requestNow);
    if (!status) {
      sendError(response, 404, "enrollment_not_found", "enrollment is no longer available");
      return;
    }
    sendJson(response, 200, status);
    return;
  }

  if (method === "POST" && url.pathname === "/v1/presence") {
    const body = await readJsonObject(request, response);
    if (!body) return;
    if (!hasExactKeys(body, ["protocolVersion", "machineId", "timestamp", "nonce", "signature"]) ||
        body.protocolVersion !== 1 || typeof body.machineId !== "string" || !validateEnrollmentId(body.machineId)) {
      sendError(response, 400, "invalid_request", "presence request is invalid");
      return;
    }
    const signed = readDeviceProof(body, "machineId", "scopra/device-presence/v1", now());
    if (!signed) {
      sendError(response, 401, "device_proof_invalid", "device proof is invalid");
      return;
    }
    const publicKey = await options.store.machinePublicKey(body.machineId);
    if (!publicKey || !verifyDeviceSignature(publicKey, signed.signature, signed.message)) {
      sendError(response, 401, "device_proof_invalid", "device proof is invalid");
      return;
    }
    const requestNow = now();
    const replayAccepted = await options.store.consumeNonce(
      deviceFingerprint(publicKey),
      signed.nonce,
      new Date(requestNow.getTime() + DEVICE_NONCE_RETENTION_SECONDS * 1000),
      requestNow,
    );
    if (!replayAccepted) {
      sendError(response, 409, "device_request_replayed", "device request nonce was already used");
      return;
    }
    if (!await options.store.heartbeat(body.machineId, publicKey, requestNow)) {
      sendError(response, 404, "machine_not_found", "machine is revoked or unavailable");
      return;
    }
    response.writeHead(204);
    response.end();
    return;
  }

  if (method === "GET" && url.pathname === "/v1/machines") {
    const principal = readDashboardPrincipal(request, response, options.dashboardPublicKeyPEM, now());
    if (!principal) return;
    const machines = await options.store.listMachines(principal.githubSubject, now());
    sendJson(response, 200, { machines });
    return;
  }

  sendError(response, 404, "not_found", "route not found");
}

function readDashboardPrincipal(
  request: IncomingMessage,
  response: ServerResponse,
  publicKeyPEM: string,
  now: Date,
) {
  const authorization = request.headers.authorization;
  if (typeof authorization !== "string" || !/^Bearer [A-Za-z0-9_.-]{1,4096}$/.test(authorization)) {
    sendError(response, 401, "authentication_required", "sign-in is required");
    return null;
  }
  try {
    const principal = verifyDashboardAssertion(authorization.slice("Bearer ".length), publicKeyPEM, Math.floor(now.getTime() / 1000));
    if (principal.githubSubject.length > 20) throw new Error("invalid subject");
    return principal;
  } catch {
    sendError(response, 401, "authentication_required", "sign-in is required");
    return null;
  }
}

function readDeviceProof(
  body: Record<string, unknown>,
  idField: "enrollmentId" | "machineId",
  domain: "scopra/device-enrollment-status/v1" | "scopra/device-presence/v1",
  currentTime: Date,
): { message: string; signature: Buffer; nonce: string } | null {
  const timestamp = body.timestamp;
  const nonce = body.nonce;
  const signatureText = body.signature;
  if (!Number.isSafeInteger(timestamp) || typeof nonce !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(nonce) ||
      Buffer.from(nonce, "base64url").byteLength !== 16 || Buffer.from(nonce, "base64url").toString("base64url") !== nonce ||
      typeof signatureText !== "string") return null;
  const timestampNumber = timestamp as number;
  const nowSeconds = Math.floor(currentTime.getTime() / 1000);
  if (Math.abs(timestampNumber - nowSeconds) > DEVICE_CLOCK_SKEW_SECONDS) return null;
  let signature: Buffer;
  try { signature = decodeDeviceSignature(signatureText); } catch { return null; }
  const message = [domain, String(body[idField]), String(timestampNumber), nonce].join("\n");
  return { message, signature, nonce };
}

async function readJsonObject(request: IncomingMessage, response: ServerResponse): Promise<Record<string, unknown> | null> {
  const contentType = request.headers["content-type"];
  if (typeof contentType !== "string" || !/^application\/json(?:\s*;\s*charset=utf-8)?$/i.test(contentType)) {
    sendError(response, 415, "unsupported_media_type", "application/json is required");
    request.resume();
    return null;
  }
  const lengthHeader = request.headers["content-length"];
  if (lengthHeader && (!/^\d+$/.test(lengthHeader) || Number(lengthHeader) > BODY_LIMIT_BYTES)) {
    sendError(response, 413, "body_too_large", "request body exceeds the limit");
    request.resume();
    return null;
  }
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    size += bytes.byteLength;
    if (size > BODY_LIMIT_BYTES) {
      sendError(response, 413, "body_too_large", "request body exceeds the limit");
      return null;
    }
    chunks.push(bytes);
  }
  const raw = Buffer.concat(chunks);
  let text: string;
  try { text = new TextDecoder("utf-8", { fatal: true }).decode(raw); }
  catch {
    sendError(response, 400, "invalid_json", "request body is not valid UTF-8");
    return null;
  }
  let value: unknown;
  try {
    value = JSON.parse(text);
    assertNoDuplicateJSONKeys(text);
  } catch {
    sendError(response, 400, "invalid_json", "request body is malformed or has duplicate object keys");
    return null;
  }
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    sendError(response, 400, "invalid_json", "request body must be an object");
    return null;
  }
  return value as Record<string, unknown>;
}

function assertNoDuplicateJSONKeys(text: string): void {
  let index = 0;
  const skipSpace = () => { while (/\s/.test(text[index] ?? "")) index++; };
  const readString = (): string => {
    const start = index;
    index++;
    while (index < text.length) {
      const char = text[index++];
      if (char === "\\") index++;
      else if (char === '"') return JSON.parse(text.slice(start, index)) as string;
    }
    throw new Error("unterminated string");
  };
  const scan = (depth: number): void => {
    if (depth > 16) throw new Error("JSON nesting too deep");
    skipSpace();
    const char = text[index];
    if (char === '"') { readString(); return; }
    if (char === "[") {
      index++; skipSpace();
      if (text[index] === "]") { index++; return; }
      while (true) {
        scan(depth + 1); skipSpace();
        if (text[index] === "]") { index++; return; }
        if (text[index++] !== ",") throw new Error("invalid array");
      }
    }
    if (char === "{") {
      index++; skipSpace();
      const keys = new Set<string>();
      if (text[index] === "}") { index++; return; }
      while (true) {
        skipSpace();
        if (text[index] !== '"') throw new Error("invalid object key");
        const key = readString();
        if (keys.has(key)) throw new Error("duplicate object key");
        keys.add(key); skipSpace();
        if (text[index++] !== ":") throw new Error("invalid object separator");
        scan(depth + 1); skipSpace();
        if (text[index] === "}") { index++; return; }
        if (text[index++] !== ",") throw new Error("invalid object");
      }
    }
    const start = index;
    while (index < text.length && !/[\s,\]}]/.test(text[index])) index++;
    if (index === start) throw new Error("invalid JSON value");
  };
  scan(0); skipSpace();
  if (index !== text.length) throw new Error("trailing JSON data");
}

function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const actualKeys = Object.keys(value).sort();
  const expectedKeys = [...expected].sort();
  return actualKeys.length === expectedKeys.length && actualKeys.every((key, index) => key === expectedKeys[index]);
}

function setSecurityHeaders(response: ServerResponse): void {
  response.setHeader("Cache-Control", "no-store, private");
  response.setHeader("Content-Type", "application/json; charset=utf-8");
  response.setHeader("X-Content-Type-Options", "nosniff");
  response.setHeader("Referrer-Policy", "no-referrer");
  response.setHeader("X-Frame-Options", "DENY");
  response.setHeader("Cross-Origin-Resource-Policy", "same-origin");
}

function sendJson(response: ServerResponse, status: number, value: unknown): void {
  const encoded = JSON.stringify(value);
  response.writeHead(status, { "Content-Length": Buffer.byteLength(encoded) });
  response.end(encoded);
}

function sendError(response: ServerResponse, status: number, code: string, message: string): void {
  sendJson(response, status, { error: { code, message } });
}

class RequestLimiter {
  private readonly windows = new Map<string, { start: number; count: number }>();

  allow(key: string, limit: number, now: number): boolean {
    if (this.windows.size > 5000) {
      for (const [candidate, window] of this.windows) {
        if (now - window.start > RATE_WINDOW_MS * 2) this.windows.delete(candidate);
      }
    }
    let window = this.windows.get(key);
    if (!window || now - window.start >= RATE_WINDOW_MS) {
      window = { start: now, count: 0 };
      this.windows.set(key, window);
    }
    window.count++;
    return window.count <= limit;
  }
}

export const deviceRequestClockSkewSeconds = DEVICE_CLOCK_SKEW_SECONDS;
export const controlPlaneJWTIssuer = DASHBOARD_ISSUER;
export const controlPlaneJWTAudience = CONTROL_PLANE_AUDIENCE;
