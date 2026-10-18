import assert from "node:assert/strict";
import test from "node:test";
import { generateKeyPairSync, sign } from "node:crypto";
import {
  CONTROL_PLANE_AUDIENCE,
  DASHBOARD_ISSUER,
  deviceFingerprint,
  verifyDashboardAssertion,
  verifyDeviceSignature,
} from "./security.js";
import { beginEnrollmentSigningMessage, newPairingCode, validateBeginEnrollment } from "./enrollment.js";

function base64url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64url");
}

function assertion(keys: ReturnType<typeof generateKeyPairSync>, claims: Record<string, unknown>): string {
  const header = base64url(Buffer.from(JSON.stringify({ alg: "EdDSA", typ: "JWT" })));
  const payload = base64url(Buffer.from(JSON.stringify(claims)));
  const input = `${header}.${payload}`;
  const signature = sign(null, Buffer.from(input), keys.privateKey);
  return `${input}.${base64url(signature)}`;
}

test("verifies short-lived dashboard assertions from an Ed25519 key", () => {
  const keys = generateKeyPairSync("ed25519");
  const now = 1_800_000_000;
  const token = assertion(keys, {
    iss: DASHBOARD_ISSUER,
    aud: CONTROL_PLANE_AUDIENCE,
    sub: "123456789",
    iat: now,
    exp: now + 60,
    jti: base64url(Buffer.alloc(16, 7)),
  });
  const principal = verifyDashboardAssertion(token, keys.publicKey.export({ type: "spki", format: "pem" }).toString(), now);
  assert.equal(principal.githubSubject, "123456789");
  assert.equal(principal.expiresAt, now + 60);
});

test("rejects expired, overlong, wrong-audience, and non-GitHub assertions", () => {
  const keys = generateKeyPairSync("ed25519");
  const pem = keys.publicKey.export({ type: "spki", format: "pem" }).toString();
  const now = 1_800_000_000;
  const base = { iss: DASHBOARD_ISSUER, aud: CONTROL_PLANE_AUDIENCE, sub: "42", iat: now, exp: now + 60, jti: base64url(Buffer.alloc(16, 1)) };
  for (const claims of [
    { ...base, exp: now },
    { ...base, exp: now + 61 },
    { ...base, aud: "another-service" },
    { ...base, sub: "gulzaib" },
  ]) {
    assert.throws(() => verifyDashboardAssertion(assertion(keys, claims), pem, now));
  }
});

test("rejects assertion tampering and non-Ed25519 verification keys", () => {
  const keys = generateKeyPairSync("ed25519");
  const now = 1_800_000_000;
  const token = assertion(keys, {
    iss: DASHBOARD_ISSUER,
    aud: CONTROL_PLANE_AUDIENCE,
    sub: "42",
    iat: now,
    exp: now + 30,
    jti: base64url(Buffer.alloc(16, 2)),
  });
  const [header, payload, signature] = token.split(".");
  const changed = `${header}.${payload.slice(0, -1)}A.${signature}`;
  assert.throws(() => verifyDashboardAssertion(changed, keys.publicKey.export({ type: "spki", format: "pem" }).toString(), now));
  const rsa = generateKeyPairSync("rsa", { modulusLength: 2048 });
  assert.throws(() => verifyDashboardAssertion(token, rsa.publicKey.export({ type: "spki", format: "pem" }).toString(), now));
});

test("verifies proof-of-possession-bound enrollment and rejects modified workspace fields", () => {
  const keys = generateKeyPairSync("ed25519");
  const spki = keys.publicKey.export({ type: "spki", format: "der" });
  const rawPublicKey = spki.subarray(spki.byteLength - 32);
  const partial = {
    protocolVersion: 1 as const,
    requestId: base64url(Buffer.alloc(16, 3)),
    publicKey: base64url(rawPublicKey),
    machineName: "dev-machine",
    workspaceId: "4cfef09b057155cd8fc9826e40bd3f25",
    workspacePath: "/home/user/code/project",
  };
  const signature = sign(null, Buffer.from(beginEnrollmentSigningMessage(partial)), keys.privateKey);
  const body = { ...partial, signature: base64url(signature) };
  const verified = validateBeginEnrollment(body);
  assert.equal(verified.workspacePath, partial.workspacePath);
  assert.equal(verified.fingerprint, deviceFingerprint(rawPublicKey));
  assert.throws(() => validateBeginEnrollment({ ...body, workspacePath: "/home/user/other" }));
});

test("rejects extra fields, malformed key encodings, and unsafe paths", () => {
  const keys = generateKeyPairSync("ed25519");
  const spki = keys.publicKey.export({ type: "spki", format: "der" });
  const partial = {
    protocolVersion: 1 as const,
    requestId: base64url(Buffer.alloc(16, 3)),
    publicKey: base64url(spki.subarray(spki.byteLength - 32)),
    machineName: "machine",
    workspaceId: "4cfef09b057155cd8fc9826e40bd3f25",
    workspacePath: "/tmp/workspace",
  };
  const signature = base64url(sign(null, Buffer.from(beginEnrollmentSigningMessage(partial)), keys.privateKey));
  const body = { ...partial, signature };
  assert.throws(() => validateBeginEnrollment({ ...body, ignored: true }));
  assert.throws(() => validateBeginEnrollment({ ...body, publicKey: "not-base64url" }));
  assert.throws(() => validateBeginEnrollment({ ...body, workspacePath: "relative/path" }));
  assert.throws(() => validateBeginEnrollment({ ...body, machineName: "unsafe\nname" }));
});

test("pairing codes carry 128 bits of entropy and use a strict canonical form", () => {
  const first = newPairingCode();
  const second = newPairingCode();
  assert.match(first, /^[A-Za-z0-9_-]{22}$/);
  assert.notEqual(first, second);
  assert.throws(() => validateBeginEnrollment(null));
});

test("device fingerprints are stable and compact", () => {
  const key = Buffer.alloc(32, 9);
  assert.equal(deviceFingerprint(key), deviceFingerprint(Buffer.from(key)));
  assert.match(deviceFingerprint(key), /^[0-9a-f]{24}$/);
  assert.equal(verifyDeviceSignature(key, Buffer.alloc(64), "invalid"), false);
});
