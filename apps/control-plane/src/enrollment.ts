import { randomBytes } from "node:crypto";
import { isAbsolute } from "node:path";
import {
  decodeDevicePublicKey,
  decodeDeviceSignature,
  deviceFingerprint,
  verifyDeviceSignature,
} from "./security.js";

const WORKSPACE_ID = /^[0-9a-f]{32}$/;
const REQUEST_ID = /^[A-Za-z0-9_-]{22}$/;
const PAIRING_CODE = /^[A-Za-z0-9_-]{22}$/;
const MAX_PATH_BYTES = 4095;
const MAX_MACHINE_NAME_BYTES = 128;
const CONTROL_CHARACTERS = /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u;

export class EnrollmentInputError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "EnrollmentInputError";
  }
}

export type BeginEnrollmentRequest = {
  protocolVersion: 1;
  requestId: string;
  publicKey: string;
  machineName: string;
  workspaceId: string;
  workspacePath: string;
  signature: string;
};

export type VerifiedBeginEnrollment = Omit<BeginEnrollmentRequest, "signature"> & {
  publicKeyBytes: Buffer;
  fingerprint: string;
};

function exactKeys(value: unknown, expected: readonly string[]): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new EnrollmentInputError("request body must be an object");
  }
  const record = value as Record<string, unknown>;
  const actualKeys = Object.keys(record).sort();
  const expectedKeys = [...expected].sort();
  if (actualKeys.length !== expectedKeys.length || actualKeys.some((key, index) => key !== expectedKeys[index])) {
    throw new EnrollmentInputError("request body has missing or unexpected fields");
  }
  return record;
}

function isUTF8LengthWithin(value: string, maximum: number): boolean {
  return Buffer.byteLength(value, "utf8") <= maximum;
}

/** Decode and validate an enrollment request, then verify proof of possession. */
export function validateBeginEnrollment(value: unknown): VerifiedBeginEnrollment {
  const body = exactKeys(value, [
    "protocolVersion", "requestId", "publicKey", "machineName", "workspaceId", "workspacePath", "signature",
  ]);
  if (body.protocolVersion !== 1) throw new EnrollmentInputError("unsupported enrollment protocol version");
  if (typeof body.requestId !== "string" || !REQUEST_ID.test(body.requestId) ||
      Buffer.from(body.requestId, "base64url").byteLength !== 16 ||
      Buffer.from(body.requestId, "base64url").toString("base64url") !== body.requestId) {
    throw new EnrollmentInputError("requestId must be canonical base64url for 16 random bytes");
  }
  if (typeof body.publicKey !== "string" || typeof body.signature !== "string") {
    throw new EnrollmentInputError("device key and signature must be strings");
  }
  if (typeof body.machineName !== "string" || !body.machineName.trim() ||
      !isUTF8LengthWithin(body.machineName, MAX_MACHINE_NAME_BYTES) || CONTROL_CHARACTERS.test(body.machineName)) {
    throw new EnrollmentInputError("machineName is empty, too long, or contains control characters");
  }
  if (typeof body.workspaceId !== "string" || !WORKSPACE_ID.test(body.workspaceId)) {
    throw new EnrollmentInputError("workspaceId is invalid");
  }
  if (typeof body.workspacePath !== "string" || !isAbsolute(body.workspacePath) ||
      !isUTF8LengthWithin(body.workspacePath, MAX_PATH_BYTES) || CONTROL_CHARACTERS.test(body.workspacePath)) {
    throw new EnrollmentInputError("workspacePath must be a bounded absolute path without control characters");
  }

  let publicKeyBytes: Buffer;
  let signatureBytes: Buffer;
  try {
    publicKeyBytes = decodeDevicePublicKey(body.publicKey);
    signatureBytes = decodeDeviceSignature(body.signature);
  } catch {
    throw new EnrollmentInputError("device key or signature encoding is invalid");
  }
  const request = body as unknown as BeginEnrollmentRequest;
  const signedMessage = beginEnrollmentSigningMessage(request);
  if (!verifyDeviceSignature(publicKeyBytes, signatureBytes, signedMessage)) {
    throw new EnrollmentInputError("device key proof is invalid");
  }
  return {
    ...request,
    publicKeyBytes,
    fingerprint: deviceFingerprint(publicKeyBytes),
  };
}

/** Stable newline-delimited signing form shared with the Go CLI. */
export function beginEnrollmentSigningMessage(request: Omit<BeginEnrollmentRequest, "signature"> | BeginEnrollmentRequest): string {
  return [
    "scopra/device-enrollment-begin/v1",
    request.requestId,
    request.machineName,
    request.workspaceId,
    request.publicKey,
    request.workspacePath,
  ].join("\n");
}

/** 128-bit one-time secret; persist only hashPairingCode(code). */
export function newPairingCode(): string {
  return randomBytes(16).toString("base64url");
}

export function normalizePairingCode(value: unknown): string {
  if (typeof value !== "string") throw new EnrollmentInputError("pairing code is required");
  const code = value.trim();
  if (!PAIRING_CODE.test(code)) throw new EnrollmentInputError("pairing code is invalid or expired");
  return code;
}

export function validateEnrollmentId(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

export const ENROLLMENT_TTL_SECONDS = 5 * 60;
export const PRESENCE_TTL_SECONDS = 90;
