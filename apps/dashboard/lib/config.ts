export type DashboardConfig = {
  controlPlaneUrl?: URL;
  githubOAuthConfigured: boolean;
};

export class DashboardConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DashboardConfigError";
  }
}

/** Parse only server-side dashboard settings; browser code never receives the API origin. */
export function readDashboardConfig(env: NodeJS.ProcessEnv = process.env): DashboardConfig {
  const githubOAuthConfigured = Boolean(env.AUTH_GITHUB_ID && env.AUTH_GITHUB_SECRET);
  const configuredUrl = env.SCOPRA_CONTROL_PLANE_URL?.trim();
  if (!configuredUrl) return { githubOAuthConfigured };

  let controlPlaneUrl: URL;
  try {
    controlPlaneUrl = new URL(configuredUrl);
  } catch {
    throw new DashboardConfigError("SCOPRA_CONTROL_PLANE_URL must be an absolute HTTPS origin");
  }
  if (controlPlaneUrl.protocol !== "https:" && env.NODE_ENV !== "development" && env.NODE_ENV !== "test") {
    throw new DashboardConfigError("SCOPRA_CONTROL_PLANE_URL must use HTTPS outside development and tests");
  }
  if (controlPlaneUrl.username || controlPlaneUrl.password || controlPlaneUrl.search || controlPlaneUrl.hash) {
    throw new DashboardConfigError("SCOPRA_CONTROL_PLANE_URL must not contain credentials, query, or fragment data");
  }
  if (controlPlaneUrl.pathname !== "/" && controlPlaneUrl.pathname !== "") {
    throw new DashboardConfigError("SCOPRA_CONTROL_PLANE_URL must be an origin, not a path");
  }
  if (env.NODE_ENV === "production" && ["localhost", "127.0.0.1", "::1"].includes(controlPlaneUrl.hostname)) {
    throw new DashboardConfigError("production dashboard cannot target a loopback control plane");
  }

  controlPlaneUrl.pathname = "";
  return { controlPlaneUrl, githubOAuthConfigured };
}
