import {
  createHash,
  createPublicKey,
  timingSafeEqual,
  verify as verifySignature,
  type KeyObject,
} from "node:crypto";

export const DASHBOARD_ISSUER = "scopra-dashboard";
export const CONTROL_PLANE_AUDIENCE = "scopra-control-plane";
export const DASHBOARD_ASSERTION_MAX_SECONDS = 60;

const ED25519_SPKI_PREFIX = Buffer.from("302a300506032b6570032100", "hex");
const BASE64URL = /^[A-Za-z0-9_-]+$/;

export class SecurityError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SecurityError";
  }
}

export type DashboardPrincipal = {
  githubSubject: string;
  assertionId: string;
  issuedAt: number;
  expiresAt: number;
};

function decodeBase64url(value: string, expectedBytes?: number): Buffer {
  if (!BASE64URL.test(value)) throw new SecurityError("invalid base64url value");
  const decoded = Buffer.from(value, "base64url");
  if (decoded.toString("base64url") !== value) throw new SecurityError("non-canonical base64url value");
  if (expectedBytes !== undefined && decoded.byteLength !== expectedBytes) {
    throw new SecurityError("base64url value has an unexpected length");
  }
  return decoded;
}

function exactObject(value: unknown, keys: readonly string[]): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new SecurityError("signed value must be an object");
  }
  const record = value as Record<string, unknown>;
  const actual = Object.keys(record).sort();
  const expected = [...keys].sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new SecurityError("signed value has missing or unexpected fields");
  }
  return record;
}

function parseJsonSegment(segment: string): Record<string, unknown> {
  const bytes = decodeBase64url(segment);
  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch {
    throw new SecurityError("signed value contains malformed JSON");
  }
  if (bytes.toString("utf8") !== Buffer.from(JSON.stringify(parsed)).toString("utf8")) {
    // The dashboard signer emits compact JSON. Requiring the exact compact
    // representation rules out ambiguous byte encodings at this boundary.
    throw new SecurityError("signed value is not compact canonical JSON");
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new SecurityError("signed value must be a JSON object");
  }
  return parsed as Record<string, unknown>;
}

/** Verify the short-lived Ed25519 assertion minted server-side by the dashboard. */
export function verifyDashboardAssertion(
  encoded: string,
  publicKeyPEM: string,
  nowSeconds = Math.floor(Date.now() / 1000),
): DashboardPrincipal {
  if (encoded.length > 4096) throw new SecurityError("dashboard assertion is too large");
  const parts = encoded.split(".");
  if (parts.length !== 3) throw new SecurityError("malformed dashboard assertion");
  const [headerPart, payloadPart, signaturePart] = parts;
  const header = exactObject(parseJsonSegment(headerPart), ["alg", "typ"]);
  if (header.alg !== "EdDSA" || header.typ !== "JWT") {
    throw new SecurityError("unsupported dashboard assertion algorithm or type");
  }
  const payload = exactObject(parseJsonSegment(payloadPart), ["iss", "aud", "sub", "iat", "exp", "jti"]);

  let publicKey: KeyObject;
  try {
    publicKey = createPublicKey(publicKeyPEM);
  } catch {
    throw new SecurityError("dashboard verification key is invalid");
  }
  if (publicKey.asymmetricKeyType !== "ed25519") {
    throw new SecurityError("dashboard verification key must be Ed25519");
  }
  const signature = decodeBase64url(signaturePart, 64);
  const signedData = Buffer.from(`${headerPart}.${payloadPart}`, "ascii");
  if (!verifySignature(null, signedData, publicKey, signature)) {
    throw new SecurityError("dashboard assertion signature is invalid");
  }

  const { iss, aud, sub, iat, exp, jti } = payload;
  if (iss !== DASHBOARD_ISSUER || aud !== CONTROL_PLANE_AUDIENCE) {
    throw new SecurityError("dashboard assertion issuer or audience is invalid");
  }
  if (typeof sub !== "string" || !/^[0-9]{1,20}$/.test(sub)) {
    throw new SecurityError("dashboard subject must be an immutable GitHub account ID");
  }
  if (!Number.isSafeInteger(iat) || !Number.isSafeInteger(exp) || !Number.isSafeInteger(nowSeconds)) {
    throw new SecurityError("dashboard assertion timestamps are invalid");
  }
  const issuedAt = iat as number;
  const expiresAt = exp as number;
  if (issuedAt > nowSeconds + 10 || expiresAt <= nowSeconds || expiresAt <= issuedAt ||
      expiresAt - issuedAt > DASHBOARD_ASSERTION_MAX_SECONDS) {
    throw new SecurityError("dashboard assertion is expired or outside its time window");
  }
  if (typeof jti !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(jti)) {
    throw new SecurityError("dashboard assertion ID is invalid");
  }
  return { githubSubject: sub, assertionId: jti, issuedAt, expiresAt };
}

/** Construct an Ed25519 public key from the raw 32-byte key stored by Go. */
export function publicKeyFromRaw(raw: Uint8Array): KeyObject {
  if (raw.byteLength !== 32) throw new SecurityError("device public key must be 32 bytes");
  try {
    return createPublicKey({
      key: Buffer.concat([ED25519_SPKI_PREFIX, Buffer.from(raw)]),
      format: "der",
      type: "spki",
    });
  } catch {
    throw new SecurityError("device public key is invalid");
  }
}

/** Verify a raw Ed25519 signature against a device public key. */
export function verifyDeviceSignature(publicKeyRaw: Uint8Array, signatureRaw: Uint8Array, message: string | Uint8Array): boolean {
  if (signatureRaw.byteLength !== 64) return false;
  try {
    return verifySignature(null, Buffer.from(message), publicKeyFromRaw(publicKeyRaw), Buffer.from(signatureRaw));
  } catch {
    return false;
  }
}

export function deviceFingerprint(publicKeyRaw: Uint8Array): string {
  return createHash("sha256").update(publicKeyRaw).digest("hex").slice(0, 24);
}

export function hashPairingCode(code: string): Buffer {
  return createHash("sha256").update(code, "ascii").digest();
}

export function equalHash(left: Uint8Array, right: Uint8Array): boolean {
  return left.byteLength === right.byteLength && timingSafeEqual(Buffer.from(left), Buffer.from(right));
}

export function decodeDevicePublicKey(encoded: string): Buffer {
  return decodeBase64url(encoded, 32);
}

export function decodeDeviceSignature(encoded: string): Buffer {
  return decodeBase64url(encoded, 64);
}
