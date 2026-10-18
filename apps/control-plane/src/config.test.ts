import assert from "node:assert/strict";
import test from "node:test";
import { generateKeyPairSync } from "node:crypto";
import { createServer } from "node:net";
import { ConfigError, readControlPlaneConfig } from "./config.js";
import { createPool } from "./store.js";

function env(extra: NodeJS.ProcessEnv = {}): NodeJS.ProcessEnv {
  const keys = generateKeyPairSync("ed25519");
  return {
    NODE_ENV: "test",
    SCOPRA_API_PORT: "8080",
    SCOPRA_DATABASE_URL: "postgres://scopra:dev-only@127.0.0.1:5432/scopra",
    SCOPRA_DASHBOARD_JWT_PUBLIC_KEY: keys.publicKey.export({ format: "pem", type: "spki" }).toString(),
    ...extra,
  };
}

test("loads a valid development configuration with a loopback database", () => {
  const config = readControlPlaneConfig(env());
  assert.equal(config.host, "0.0.0.0");
  assert.equal(config.port, 8080);
  assert.equal(config.production, false);
});

test("requires a non-loopback database in production and a valid Ed25519 key", () => {
  assert.throws(() => readControlPlaneConfig(env({ NODE_ENV: "production" })), ConfigError);
  assert.throws(() => readControlPlaneConfig(env({ SCOPRA_DASHBOARD_JWT_PUBLIC_KEY: "not a key" })), ConfigError);
  assert.throws(() => readControlPlaneConfig(env({ SCOPRA_DATABASE_URL: "http://db.example" })), ConfigError);
  assert.throws(() => readControlPlaneConfig(env({ SCOPRA_API_PORT: "65536" })), ConfigError);
});

test("validates production database URLs and rejects SSL query overrides", () => {
  const base = env({
    NODE_ENV: "production",
    SCOPRA_DATABASE_URL: "postgresql://scopra:secret@db.example.net:5432/scopra",
  });
  assert.equal(readControlPlaneConfig(base).databaseTLSMode, "verify-full");
  assert.equal(readControlPlaneConfig({ ...base, SCOPRA_DATABASE_TLS_MODE: "require" }).databaseTLSMode, "require");
  assert.throws(() => readControlPlaneConfig({ ...base, SCOPRA_DATABASE_TLS_MODE: "disable" }), ConfigError);
  for (const parameter of ["sslmode=disable", "sslmode=require", "ssl=0", "ssl=no-verify", "sslrootcert=%2Ftmp%2Fca.pem"]) {
    assert.throws(
      () => readControlPlaneConfig({ ...base, SCOPRA_DATABASE_URL: `${base.SCOPRA_DATABASE_URL}?${parameter}` }),
      ConfigError,
      `accepted a database URL with ${parameter}`,
    );
  }
});

test("production PostgreSQL TLS modes configure certificate verification explicitly", async () => {
  const verified = createPool("postgresql://scopra:secret@db.example.net:5432/scopra", true);
  const renderInternal = createPool("postgresql://scopra:secret@dpg-example-a:5432/scopra", true, "require");
  try {
    assert.deepEqual(verified.options.ssl, { rejectUnauthorized: true });
    assert.deepEqual(renderInternal.options.ssl, { rejectUnauthorized: false });
  } finally {
    await Promise.all([verified.end(), renderInternal.end()]);
  }
});

test("Render private-network TLS mode rejects a plaintext-only database", async () => {
  const server = createServer((socket) => {
    socket.once("data", () => {
      socket.end("N");
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("test database did not bind a TCP port");
  const pool = createPool(`postgresql://scopra:secret@127.0.0.1:${address.port}/scopra`, true, "require");
  try {
    await assert.rejects(pool.query("SELECT 1"), /server does not support SSL connections/);
  } finally {
    await pool.end();
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
});

test("requires all security-sensitive settings", () => {
  assert.throws(() => readControlPlaneConfig({ NODE_ENV: "test" }), ConfigError);
});
