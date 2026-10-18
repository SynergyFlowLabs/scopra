import "server-only";
import { createPrivateKey, randomBytes, sign, type KeyObject } from "node:crypto";

const ISSUER = "scopra-dashboard";
const AUDIENCE = "scopra-control-plane";
const ASSERTION_SECONDS = 60;

export class BackendTokenError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "BackendTokenError";
  }
}

function loadPrivateKey(encodedPEM: string): KeyObject {
  let key: KeyObject;
  try {
    key = createPrivateKey(encodedPEM.replace(/\\n/g, "\n"));
  } catch {
    throw new BackendTokenError("dashboard service signing key is not configured correctly");
  }
  if (key.asymmetricKeyType !== "ed25519") {
    throw new BackendTokenError("dashboard service signing key must be Ed25519");
  }
  return key;
}

/** Mint a minimal, one-minute service assertion. This function is server-only. */
export function createBackendAssertion(githubSubject: string, env: NodeJS.ProcessEnv = process.env, nowSeconds = Math.floor(Date.now() / 1000)): string {
  if (!/^[0-9]{1,20}$/.test(githubSubject)) {
    throw new BackendTokenError("authenticated GitHub subject is invalid");
  }
  const encodedPEM = env.SCOPRA_API_JWT_PRIVATE_KEY;
  if (!encodedPEM) throw new BackendTokenError("dashboard service signing key is not configured");
  const key = loadPrivateKey(encodedPEM);
  const header = base64url(JSON.stringify({ alg: "EdDSA", typ: "JWT" }));
  const payload = base64url(JSON.stringify({
    iss: ISSUER,
    aud: AUDIENCE,
    sub: githubSubject,
    iat: nowSeconds,
    exp: nowSeconds + ASSERTION_SECONDS,
    jti: randomBytes(16).toString("base64url"),
  }));
  const signingInput = `${header}.${payload}`;
  const signature = sign(null, Buffer.from(signingInput, "ascii"), key).toString("base64url");
  return `${signingInput}.${signature}`;
}

function base64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}
