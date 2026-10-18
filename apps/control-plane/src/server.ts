import { createPool, PostgresStore } from "./store.js";
import { createControlPlaneServer } from "./http.js";
import { readControlPlaneConfig } from "./config.js";

async function main(): Promise<void> {
  const config = readControlPlaneConfig();
  const store = new PostgresStore(createPool(config.databaseUrl, config.production, config.databaseTLSMode));
  try {
    await store.migrate();
    await store.healthcheck();
  } catch (error) {
    await store.close().catch(() => undefined);
    // Do not print the connection URL or parameters; PostgreSQL errors can
    // include deployment-specific details. The process manager records this
    // non-secret startup marker and the readiness probe remains unhealthy.
    console.error("scopra-caloris: database initialization failed", error instanceof Error ? error.name : "unknown error");
    process.exitCode = 1;
    return;
  }

  const server = createControlPlaneServer({
    store,
    dashboardPublicKeyPEM: config.dashboardPublicKeyPEM,
  });
  server.requestTimeout = 15_000;
  server.headersTimeout = 10_000;
  server.keepAliveTimeout = 5_000;
  server.maxHeadersCount = 64;
  server.maxRequestsPerSocket = 1000;

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(config.port, config.host, () => {
      server.removeListener("error", reject);
      resolve();
    });
  });
  console.info(`scopra-caloris: ready on ${config.host}:${config.port}`);

  let shuttingDown = false;
  const shutdown = (signal: string) => {
    if (shuttingDown) return;
    shuttingDown = true;
    console.info(`scopra-caloris: shutting down (${signal})`);
    const force = setTimeout(() => process.exit(1), 10_000);
    force.unref();
    server.close(async () => {
      clearTimeout(force);
      await store.close().catch(() => undefined);
      process.exitCode = 0;
    });
  };
  process.once("SIGTERM", () => shutdown("SIGTERM"));
  process.once("SIGINT", () => shutdown("SIGINT"));
}

main().catch((error: unknown) => {
  console.error("scopra-caloris: startup failed", error instanceof Error ? error.name : "unknown error");
  process.exitCode = 1;
});
