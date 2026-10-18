import assert from "node:assert/strict";
import test from "node:test";
import { createHash, generateKeyPairSync, randomBytes, sign } from "node:crypto";
import type { AddressInfo } from "node:net";
import {
  beginEnrollmentSigningMessage,
  type BeginEnrollmentRequest,
} from "./enrollment.js";
import { createControlPlaneServer } from "./http.js";
import {
  CONTROL_PLANE_AUDIENCE,
  DASHBOARD_ISSUER,
  deviceFingerprint,
  hashPairingCode,
} from "./security.js";
import type {
  ControlPlaneStore,
  EnrollmentInspection,
  EnrollmentStatus,
  MachineSummary,
  NewEnrollment,
} from "./store.js";

const FIXED_NOW = new Date("2026-10-18T12:00:00.000Z");
const WORKSPACE_ID = "4cfef09b057155cd8fc9826e40bd3f25";
const ENROLLMENT_ID = "5a222a2c-9a0b-4b25-9743-ae46e15b0f75";
const MACHINE_ID = "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e";
const CLOUD_WORKSPACE_ID = "6a597469-2af4-4f39-9704-4e1c3d9dc5d2";
const SECOND_WORKSPACE_ID = "a0d5cd4fe22456bcae625905401f8fe1";
const SECOND_CLOUD_WORKSPACE_ID = "81f29a7e-29fb-43c7-9ce4-52b82ef92d53";
const SECOND_ENROLLMENT_ID = "7f70b24d-79a2-43d1-99c1-48cd8a01d10f";
const PAIRING_CODE = "JRFsUqK7x1ENr9MeRw2AqA";

function b64url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64url");
}

function rawPublicKey(key: ReturnType<typeof generateKeyPairSync> extends never ? never : any): Buffer {
  const der = key.export({ format: "der", type: "spki" }) as Buffer;
  return der.subarray(der.length - 32);
}

function makeDashboardToken(keys: ReturnType<typeof generateKeyPairSync>, githubSubject = "123456789"): string {
  const now = Math.floor(FIXED_NOW.getTime() / 1000);
  const header = b64url(Buffer.from(JSON.stringify({ alg: "EdDSA", typ: "JWT" })));
  const payload = b64url(Buffer.from(JSON.stringify({
    iss: DASHBOARD_ISSUER,
    aud: CONTROL_PLANE_AUDIENCE,
    sub: githubSubject,
    iat: now,
    exp: now + 60,
    jti: b64url(randomBytes(16)),
  })));
  const input = `${header}.${payload}`;
  return `${input}.${b64url(sign(null, Buffer.from(input), keys.privateKey))}`;
}

function makeBeginRequest(keys: ReturnType<typeof generateKeyPairSync>, path = "/home/user/code/project", workspaceId = WORKSPACE_ID) {
  const publicKey = rawPublicKey(keys.publicKey);
  const partial = {
    protocolVersion: 1 as const,
    requestId: b64url(randomBytes(16)),
    publicKey: b64url(publicKey),
    machineName: "dev-machine",
    workspaceId,
    workspacePath: path,
  };
  const signature = b64url(sign(null, Buffer.from(beginEnrollmentSigningMessage(partial)), keys.privateKey));
  return { ...partial, signature };
}

async function withServer(
  store: FakeStore,
  dashboardKeys: ReturnType<typeof generateKeyPairSync>,
  run: (origin: string) => Promise<void>,
): Promise<void> {
  const server = createControlPlaneServer({
    store,
    dashboardPublicKeyPEM: dashboardKeys.publicKey.export({ format: "pem", type: "spki" }).toString(),
    now: () => FIXED_NOW,
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  try { await run(origin); }
  finally { await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve())); }
}

async function post(origin: string, path: string, body: unknown, token?: string): Promise<Response> {
  return fetch(`${origin}${path}`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...(token ? { authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  });
}

class FakeStore implements ControlPlaneStore {
  beginInput?: NewEnrollment;
  enrollment?: EnrollmentInspection & { codeHash: Buffer; status: EnrollmentStatus; localWorkspaceId: string };
  enrollmentsCreated = 0;
  workspaces: MachineSummary["workspaces"] = [];
  nonces = new Set<string>();
  heartbeats: string[] = [];
  ownerSubject = "123456789";

  async healthcheck(): Promise<void> {}
  async close(): Promise<void> {}

  async beginEnrollment(input: NewEnrollment): Promise<{ enrollmentId: string }> {
    this.beginInput = input;
    const enrollmentId = this.enrollmentsCreated++ === 0 ? ENROLLMENT_ID : SECOND_ENROLLMENT_ID;
    this.enrollment = {
      enrollmentId,
      publicKey: input.publicKey,
      fingerprint: input.fingerprint,
      machineName: input.machineName,
      workspacePath: input.workspacePath,
      expiresAt: input.expiresAt,
      codeHash: input.pairingCodeHash,
      status: { status: "pending" },
      localWorkspaceId: input.localWorkspaceId,
    };
    return { enrollmentId };
  }

  async inspectEnrollment(codeHash: Buffer, now: Date): Promise<EnrollmentInspection | null> {
    if (!this.enrollment || !this.enrollment.codeHash.equals(codeHash) || this.enrollment.expiresAt <= now || this.enrollment.status.status !== "pending") return null;
    return this.enrollment;
  }

  async claimEnrollment(input: { pairingCodeHash: Buffer; githubSubject: string; expectedFingerprint: string; expectedWorkspacePath: string; now: Date }): Promise<{ machineId: string; workspaceId: string } | null> {
    const enrollment = this.enrollment;
    if (!enrollment || !enrollment.codeHash.equals(input.pairingCodeHash) || enrollment.status.status !== "pending" || enrollment.expiresAt <= input.now ||
        enrollment.fingerprint !== input.expectedFingerprint || enrollment.workspacePath !== input.expectedWorkspacePath) return null;
    if (this.ownerSubject !== input.githubSubject && this.workspaces.length > 0) return null;
    const existingWorkspace = this.workspaces.find((workspace) => workspace.localWorkspaceId === enrollment.localWorkspaceId);
    if (existingWorkspace && existingWorkspace.path !== enrollment.workspacePath) return null;
    const workspaceId = existingWorkspace?.id ?? (this.workspaces.length === 0 ? CLOUD_WORKSPACE_ID : SECOND_CLOUD_WORKSPACE_ID);
    if (!existingWorkspace) {
      this.workspaces.push({
        id: workspaceId,
        localWorkspaceId: enrollment.localWorkspaceId,
        path: enrollment.workspacePath,
      });
    }
    enrollment.status = { status: "accepted", machineId: MACHINE_ID, workspaceId };
    this.ownerSubject = input.githubSubject;
    return { machineId: MACHINE_ID, workspaceId };
  }

  async enrollmentPublicKey(enrollmentId: string): Promise<Buffer | null> {
    return enrollmentId === this.enrollment?.enrollmentId ? this.enrollment.publicKey : null;
  }

  async enrollmentStatus(enrollmentId: string, publicKey: Buffer, _now: Date): Promise<EnrollmentStatus | null> {
    if (enrollmentId !== this.enrollment?.enrollmentId || !publicKey.equals(this.enrollment.publicKey)) return null;
    return this.enrollment.status;
  }

  async machinePublicKey(machineId: string): Promise<Buffer | null> {
    return machineId === MACHINE_ID && this.enrollment?.status.status === "accepted" ? this.enrollment.publicKey : null;
  }

  async consumeNonce(fingerprint: string, nonce: string, _expiresAt: Date, _now: Date): Promise<boolean> {
    const key = `${fingerprint}:${nonce}`;
    if (this.nonces.has(key)) return false;
    this.nonces.add(key);
    return true;
  }

  async heartbeat(machineId: string, publicKey: Buffer, _now: Date): Promise<boolean> {
    if (machineId !== MACHINE_ID || !this.enrollment || !publicKey.equals(this.enrollment.publicKey)) return false;
    this.heartbeats.push(machineId);
    return true;
  }

  async listMachines(githubSubject: string, _now: Date): Promise<MachineSummary[]> {
    if (githubSubject !== this.ownerSubject || !this.enrollment || this.enrollment.status.status !== "accepted") return [];
    return [{
      id: MACHINE_ID,
      machineName: this.enrollment.machineName,
      fingerprint: this.enrollment.fingerprint,
      createdAt: FIXED_NOW.toISOString(),
      lastSeenAt: null,
      presence: "offline",
      workspaces: this.workspaces,
    }];
  }
}

test("enrollment creates a 128-bit code, stores only its hash, and requires proof of the device key", async () => {
  const store = new FakeStore();
  const dashboardKeys = generateKeyPairSync("ed25519");
  const deviceKeys = generateKeyPairSync("ed25519");
  await withServer(store, dashboardKeys, async (origin) => {
    const body = makeBeginRequest(deviceKeys);
    const response = await post(origin, "/v1/enrollments", body);
    assert.equal(response.status, 201);
    const created = await response.json() as { enrollmentId: string; pairingCode: string; expiresAt: string };
    assert.equal(created.enrollmentId, ENROLLMENT_ID);
    assert.match(created.pairingCode, /^[A-Za-z0-9_-]{22}$/);
    assert.equal(new Date(created.expiresAt).getTime() - FIXED_NOW.getTime(), 5 * 60 * 1000);
    assert.ok(store.beginInput?.pairingCodeHash.equals(hashPairingCode(created.pairingCode)));
    assert.equal("pairingCode" in (store.beginInput as unknown as object), false);

    const modified = { ...body, workspacePath: "/home/user/other" };
    const rejected = await post(origin, "/v1/enrollments", modified);
    assert.equal(rejected.status, 400);
  });
});

test("pairing details require an authenticated GitHub identity and confirmation binds the displayed path/fingerprint", async () => {
  const store = new FakeStore();
  const dashboardKeys = generateKeyPairSync("ed25519");
  const deviceKeys = generateKeyPairSync("ed25519");
  const token = makeDashboardToken(dashboardKeys);
  await withServer(store, dashboardKeys, async (origin) => {
    const begin = await post(origin, "/v1/enrollments", makeBeginRequest(deviceKeys));
    const created = await begin.json() as { pairingCode: string };

    assert.equal((await post(origin, "/v1/enrollments/inspect", { pairingCode: created.pairingCode })).status, 401);
    const inspected = await post(origin, "/v1/enrollments/inspect", { pairingCode: created.pairingCode }, token);
    assert.equal(inspected.status, 200);
    const details = await inspected.json() as { fingerprint: string; workspacePath: string; machineName: string };
    assert.equal(details.fingerprint, deviceFingerprint(rawPublicKey(deviceKeys.publicKey)));
    assert.equal(details.workspacePath, "/home/user/code/project");
    assert.equal(details.machineName, "dev-machine");

    const changed = await post(origin, "/v1/enrollments/claim", {
      pairingCode: created.pairingCode,
      fingerprint: details.fingerprint,
      workspacePath: "/home/user/other",
    }, token);
    assert.equal(changed.status, 409);

    const claimed = await post(origin, "/v1/enrollments/claim", {
      pairingCode: created.pairingCode,
      fingerprint: details.fingerprint,
      workspacePath: details.workspacePath,
    }, token);
    assert.equal(claimed.status, 200);
    assert.deepEqual(await claimed.json(), { machineId: MACHINE_ID, workspaceId: CLOUD_WORKSPACE_ID });
    const replay = await post(origin, "/v1/enrollments/claim", {
      pairingCode: created.pairingCode,
      fingerprint: details.fingerprint,
      workspacePath: details.workspacePath,
    }, token);
    assert.equal(replay.status, 409);

    const otherAccount = makeDashboardToken(dashboardKeys, "999");
    const list = await fetch(`${origin}/v1/machines`, { headers: { authorization: `Bearer ${otherAccount}` } });
    assert.equal(list.status, 200);
    assert.deepEqual(await list.json(), { machines: [] });
    const ownerList = await fetch(`${origin}/v1/machines`, { headers: { authorization: `Bearer ${token}` } });
    assert.equal((await ownerList.json() as { machines: unknown[] }).machines.length, 1);
  });
});

test("one enrolled machine can explicitly add another workspace without creating a second machine", async () => {
  const store = new FakeStore();
  const dashboardKeys = generateKeyPairSync("ed25519");
  const deviceKeys = generateKeyPairSync("ed25519");
  const token = makeDashboardToken(dashboardKeys);
  await withServer(store, dashboardKeys, async (origin) => {
    const first = await post(origin, "/v1/enrollments", makeBeginRequest(deviceKeys));
    const firstCreated = await first.json() as { pairingCode: string };
    const firstInspection = await post(origin, "/v1/enrollments/inspect", { pairingCode: firstCreated.pairingCode }, token);
    const firstDetails = await firstInspection.json() as { fingerprint: string; workspacePath: string };
    const firstClaim = await post(origin, "/v1/enrollments/claim", {
      pairingCode: firstCreated.pairingCode,
      fingerprint: firstDetails.fingerprint,
      workspacePath: firstDetails.workspacePath,
    }, token);
    assert.deepEqual(await firstClaim.json(), { machineId: MACHINE_ID, workspaceId: CLOUD_WORKSPACE_ID });

    const second = await post(origin, "/v1/enrollments", makeBeginRequest(deviceKeys, "/home/user/code/second", SECOND_WORKSPACE_ID));
    assert.equal(second.status, 201);
    const secondCreated = await second.json() as { enrollmentId: string; pairingCode: string };
    assert.equal(secondCreated.enrollmentId, SECOND_ENROLLMENT_ID);
    const secondInspection = await post(origin, "/v1/enrollments/inspect", { pairingCode: secondCreated.pairingCode }, token);
    const secondDetails = await secondInspection.json() as { fingerprint: string; workspacePath: string };
    const secondClaim = await post(origin, "/v1/enrollments/claim", {
      pairingCode: secondCreated.pairingCode,
      fingerprint: secondDetails.fingerprint,
      workspacePath: secondDetails.workspacePath,
    }, token);
    assert.deepEqual(await secondClaim.json(), { machineId: MACHINE_ID, workspaceId: SECOND_CLOUD_WORKSPACE_ID });

    const listed = await fetch(`${origin}/v1/machines`, { headers: { authorization: `Bearer ${token}` } });
    const result = await listed.json() as { machines: MachineSummary[] };
    assert.equal(result.machines.length, 1);
    assert.deepEqual(result.machines[0].workspaces.map((workspace) => workspace.path).sort(), [
      "/home/user/code/project", "/home/user/code/second",
    ]);
  });
});

test("status and presence require signed fresh requests and reject nonce replay", async () => {
  const store = new FakeStore();
  const dashboardKeys = generateKeyPairSync("ed25519");
  const deviceKeys = generateKeyPairSync("ed25519");
  const token = makeDashboardToken(dashboardKeys);
  await withServer(store, dashboardKeys, async (origin) => {
    const createdResponse = await post(origin, "/v1/enrollments", makeBeginRequest(deviceKeys));
    const created = await createdResponse.json() as { pairingCode: string };
    const inspected = await post(origin, "/v1/enrollments/inspect", { pairingCode: created.pairingCode }, token);
    const details = await inspected.json() as { fingerprint: string; workspacePath: string };
    await post(origin, "/v1/enrollments/claim", {
      pairingCode: created.pairingCode,
      fingerprint: details.fingerprint,
      workspacePath: details.workspacePath,
    }, token);

    const timestamp = Math.floor(FIXED_NOW.getTime() / 1000);
    const nonce = b64url(randomBytes(16));
    const statusMessage = ["scopra/device-enrollment-status/v1", ENROLLMENT_ID, String(timestamp), nonce].join("\n");
    const statusBody = { protocolVersion: 1, enrollmentId: ENROLLMENT_ID, timestamp, nonce, signature: b64url(sign(null, Buffer.from(statusMessage), deviceKeys.privateKey)) };
    const status = await post(origin, "/v1/enrollments/status", statusBody);
    assert.equal(status.status, 200);
    assert.deepEqual(await status.json(), { status: "accepted", machineId: MACHINE_ID, workspaceId: CLOUD_WORKSPACE_ID });
    assert.equal((await post(origin, "/v1/enrollments/status", statusBody)).status, 409);

    const badTimestamp = timestamp - 61;
    const oldNonce = b64url(randomBytes(16));
    const oldMessage = ["scopra/device-enrollment-status/v1", ENROLLMENT_ID, String(badTimestamp), oldNonce].join("\n");
    const oldStatus = await post(origin, "/v1/enrollments/status", {
      ...statusBody,
      timestamp: badTimestamp,
      nonce: oldNonce,
      signature: b64url(sign(null, Buffer.from(oldMessage), deviceKeys.privateKey)),
    });
    assert.equal(oldStatus.status, 401);

    const presenceNonce = b64url(randomBytes(16));
    const presenceMessage = ["scopra/device-presence/v1", MACHINE_ID, String(timestamp), presenceNonce].join("\n");
    const presenceBody = { protocolVersion: 1, machineId: MACHINE_ID, timestamp, nonce: presenceNonce, signature: b64url(sign(null, Buffer.from(presenceMessage), deviceKeys.privateKey)) };
    const presence = await post(origin, "/v1/presence", presenceBody);
    assert.equal(presence.status, 204);
    assert.equal(store.heartbeats.length, 1);
    assert.equal((await post(origin, "/v1/presence", presenceBody)).status, 409);
  });
});

test("rejects duplicate JSON keys and code-like query strings", async () => {
  const store = new FakeStore();
  const dashboardKeys = generateKeyPairSync("ed25519");
  await withServer(store, dashboardKeys, async (origin) => {
    const duplicate = await fetch(`${origin}/v1/enrollments`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: `{"protocolVersion":1,"protocolVersion":1}`,
    });
    assert.equal(duplicate.status, 400);
    const query = await fetch(`${origin}/v1/enrollments/inspect?pairingCode=${encodeURIComponent(PAIRING_CODE)}`, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${makeDashboardToken(dashboardKeys)}` },
      body: JSON.stringify({ pairingCode: PAIRING_CODE }),
    });
    assert.equal(query.status, 400);
    assert.equal(store.beginInput, undefined);
  });
});
