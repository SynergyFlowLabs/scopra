import assert from "node:assert/strict";
import test from "node:test";
import { createPublicKey, generateKeyPairSync, verify } from "node:crypto";
import { BackendTokenError, createBackendAssertion } from "./backend-token";

function decode(segment: string): unknown {
  const bytes = Buffer.from(segment, "base64url");
  assert.equal(bytes.toString("base64url"), segment);
  return JSON.parse(bytes.toString("utf8")) as unknown;
}

test("mints a one-minute Ed25519 assertion for a stable GitHub subject", () => {
  const keys = generateKeyPairSync("ed25519");
  const now = 1_800_000_000;
  const privatePEM = keys.privateKey.export({ type: "pkcs8", format: "pem" }).toString();
  const token = createBackendAssertion("123456789", { NODE_ENV: "test", SCOPRA_API_JWT_PRIVATE_KEY: privatePEM }, now);
  const [headerPart, payloadPart, signaturePart] = token.split(".");
  assert.deepEqual(decode(headerPart), { alg: "EdDSA", typ: "JWT" });
  const payload = decode(payloadPart) as Record<string, unknown>;
  assert.equal(payload.iss, "scopra-dashboard");
  assert.equal(payload.aud, "scopra-control-plane");
  assert.equal(payload.sub, "123456789");
  assert.equal(payload.iat, now);
  assert.equal(payload.exp, now + 60);
  assert.match(String(payload.jti), /^[A-Za-z0-9_-]{22}$/);
  const publicKey = createPublicKey(keys.publicKey.export({ type: "spki", format: "pem" }));
  assert.equal(verify(null, Buffer.from(`${headerPart}.${payloadPart}`, "ascii"), publicKey, Buffer.from(signaturePart, "base64url")), true);
});

test("refuses missing signing keys, non-Ed25519 keys, and mutable usernames", () => {
  assert.throws(() => createBackendAssertion("42", { NODE_ENV: "test" }), BackendTokenError);
  const rsa = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const rsaPEM = rsa.privateKey.export({ type: "pkcs8", format: "pem" }).toString();
  assert.throws(() => createBackendAssertion("42", { NODE_ENV: "test", SCOPRA_API_JWT_PRIVATE_KEY: rsaPEM }), BackendTokenError);
  const keys = generateKeyPairSync("ed25519");
  const privatePEM = keys.privateKey.export({ type: "pkcs8", format: "pem" }).toString();
  assert.throws(() => createBackendAssertion("gulzaib", { NODE_ENV: "test", SCOPRA_API_JWT_PRIVATE_KEY: privatePEM }), BackendTokenError);
});
