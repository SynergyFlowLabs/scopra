import { createPublicKey } from "node:crypto";

export type DatabaseTLSMode = "verify-full" | "require";

export type ControlPlaneConfig = {
  host: string;
  port: number;
  databaseUrl: string;
  databaseTLSMode: DatabaseTLSMode;
  dashboardPublicKeyPEM: string;
  production: boolean;
};

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ConfigError";
  }
}

export function readControlPlaneConfig(env: NodeJS.ProcessEnv = process.env): ControlPlaneConfig {
  const production = env.NODE_ENV === "production";
  const host = env.SCOPRA_API_HOST?.trim() || "0.0.0.0";
  const portValue = env.SCOPRA_API_PORT?.trim() || env.PORT?.trim() || "8080";
  if (!/^[0-9]{1,5}$/.test(portValue)) throw new ConfigError("SCOPRA_API_PORT must be an integer between 1 and 65535");
  const port = Number(portValue);
  if (port < 1 || port > 65535) throw new ConfigError("SCOPRA_API_PORT must be an integer between 1 and 65535");

  const databaseUrl = env.SCOPRA_DATABASE_URL?.trim() ?? "";
  if (!databaseUrl) throw new ConfigError("SCOPRA_DATABASE_URL is required");
  let parsedDatabaseUrl: URL;
  try { parsedDatabaseUrl = new URL(databaseUrl); }
  catch { throw new ConfigError("SCOPRA_DATABASE_URL must be a valid PostgreSQL URL"); }
  if (!(["postgres:", "postgresql:"].includes(parsedDatabaseUrl.protocol)) ||
      !parsedDatabaseUrl.hostname || !parsedDatabaseUrl.pathname || parsedDatabaseUrl.pathname === "/") {
    throw new ConfigError("SCOPRA_DATABASE_URL must identify a PostgreSQL database");
  }
  if (production && ["localhost", "127.0.0.1", "::1"].includes(parsedDatabaseUrl.hostname)) {
    throw new ConfigError("production database must not be a loopback address");
  }
  const tlsOverrideParameters = ["ssl", "sslmode", "sslcert", "sslkey", "sslrootcert", "sslnegotiation", "uselibpqcompat"];
  if (production && tlsOverrideParameters.some((parameter) => parsedDatabaseUrl.searchParams.has(parameter))) {
    throw new ConfigError("SCOPRA_DATABASE_URL must not contain SSL override parameters; configure SCOPRA_DATABASE_TLS_MODE explicitly");
  }
  const databaseTLSModeValue = env.SCOPRA_DATABASE_TLS_MODE?.trim() || "verify-full";
  if (databaseTLSModeValue !== "verify-full" && databaseTLSModeValue !== "require") {
    throw new ConfigError("SCOPRA_DATABASE_TLS_MODE must be verify-full or require");
  }
  const databaseTLSMode: DatabaseTLSMode = databaseTLSModeValue;

  const dashboardPublicKeyPEM = (env.SCOPRA_DASHBOARD_JWT_PUBLIC_KEY ?? "").replace(/\\n/g, "\n").trim();
  if (!dashboardPublicKeyPEM) throw new ConfigError("SCOPRA_DASHBOARD_JWT_PUBLIC_KEY is required");
  try {
    const key = createPublicKey(dashboardPublicKeyPEM);
    if (key.asymmetricKeyType !== "ed25519") throw new Error("wrong key type");
  } catch {
    throw new ConfigError("SCOPRA_DASHBOARD_JWT_PUBLIC_KEY must be a valid Ed25519 public key in PEM format");
  }

  return { host, port, databaseUrl, databaseTLSMode, dashboardPublicKeyPEM, production };
}
