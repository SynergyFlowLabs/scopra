import assert from "node:assert/strict";
import test from "node:test";
import { DashboardConfigError, readDashboardConfig } from "./config";

test("accepts an HTTPS control-plane origin in production", () => {
  const config = readDashboardConfig({
    NODE_ENV: "production",
    SCOPRA_CONTROL_PLANE_URL: "https://api.scopra.example",
    AUTH_GITHUB_ID: "client-id",
    AUTH_GITHUB_SECRET: "configured-out-of-band",
  });
  assert.equal(config.controlPlaneUrl?.origin, "https://api.scopra.example");
  assert.equal(config.githubOAuthConfigured, true);
});

test("allows a missing API origin without fabricating a connected state", () => {
  const config = readDashboardConfig({ NODE_ENV: "production" });
  assert.equal(config.controlPlaneUrl, undefined);
  assert.equal(config.githubOAuthConfigured, false);
});

test("rejects HTTP outside development", () => {
  assert.throws(
    () => readDashboardConfig({ NODE_ENV: "production", SCOPRA_CONTROL_PLANE_URL: "http://api.example" }),
    DashboardConfigError,
  );
});

test("rejects loopback origins in production", () => {
  assert.throws(
    () => readDashboardConfig({ NODE_ENV: "production", SCOPRA_CONTROL_PLANE_URL: "https://localhost:8080" }),
    DashboardConfigError,
  );
});

test("rejects credentials, path, query, and fragments", () => {
  for (const value of [
    "https://user:pass@api.example",
    "https://api.example/v1",
    "https://api.example?token=secret",
    "https://api.example/#fragment",
  ]) {
    assert.throws(
      () => readDashboardConfig({ NODE_ENV: "production", SCOPRA_CONTROL_PLANE_URL: value }),
      DashboardConfigError,
      value,
    );
  }
});
