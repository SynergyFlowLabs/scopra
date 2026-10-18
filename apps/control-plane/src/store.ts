import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { Pool, type PoolClient } from "pg";
import { deviceFingerprint } from "./security.js";
import type { DatabaseTLSMode } from "./config.js";

export type NewEnrollment = {
  requestId: string;
  publicKey: Buffer;
  fingerprint: string;
  machineName: string;
  localWorkspaceId: string;
  workspacePath: string;
  pairingCodeHash: Buffer;
  createdAt: Date;
  expiresAt: Date;
};

export type EnrollmentInspection = {
  enrollmentId: string;
  publicKey: Buffer;
  fingerprint: string;
  machineName: string;
  workspacePath: string;
  expiresAt: Date;
};

export type EnrollmentStatus = {
  status: "pending" | "accepted" | "expired";
  machineId?: string;
  workspaceId?: string;
};

export type MachineWorkspace = {
  id: string;
  localWorkspaceId: string;
  path: string;
};

export type MachineSummary = {
  id: string;
  machineName: string;
  fingerprint: string;
  createdAt: string;
  lastSeenAt: string | null;
  presence: "online" | "offline";
  workspaces: MachineWorkspace[];
};

export interface ControlPlaneStore {
  healthcheck(): Promise<void>;
  beginEnrollment(input: NewEnrollment): Promise<{ enrollmentId: string }>;
  inspectEnrollment(pairingCodeHash: Buffer, now: Date): Promise<EnrollmentInspection | null>;
  claimEnrollment(input: {
    pairingCodeHash: Buffer;
    githubSubject: string;
    expectedFingerprint: string;
    expectedWorkspacePath: string;
    now: Date;
  }): Promise<{ machineId: string; workspaceId: string } | null>;
  enrollmentPublicKey(enrollmentId: string): Promise<Buffer | null>;
  enrollmentStatus(enrollmentId: string, publicKey: Buffer, now: Date): Promise<EnrollmentStatus | null>;
  machinePublicKey(machineId: string): Promise<Buffer | null>;
  consumeNonce(fingerprint: string, nonce: string, expiresAt: Date, now: Date): Promise<boolean>;
  heartbeat(machineId: string, publicKey: Buffer, now: Date): Promise<boolean>;
  listMachines(githubSubject: string, now: Date): Promise<MachineSummary[]>;
  close(): Promise<void>;
}

export class StoreConflictError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StoreConflictError";
  }
}

export class PostgresStore implements ControlPlaneStore {
  constructor(private readonly pool: Pool) {}

  async migrate(): Promise<void> {
    const connection = await this.pool.connect();
    try {
      await connection.query("SELECT pg_advisory_lock($1)", [741_108_018]);
      await connection.query(`CREATE TABLE IF NOT EXISTS scopra_schema_migrations (
        version text PRIMARY KEY,
        applied_at timestamptz NOT NULL DEFAULT now()
      )`);
      const migrationPath = fileURLToPath(new URL("../migrations/001_enrollment_presence.sql", import.meta.url));
      const migration = await readFile(migrationPath, "utf8");
      const version = "001_enrollment_presence";
      const applied = await connection.query("SELECT 1 FROM scopra_schema_migrations WHERE version = $1", [version]);
      if (applied.rowCount === 0) {
        await connection.query("BEGIN");
        try {
          await connection.query(migration);
          await connection.query("INSERT INTO scopra_schema_migrations(version) VALUES ($1)", [version]);
          await connection.query("COMMIT");
        } catch (error) {
          await connection.query("ROLLBACK");
          throw error;
        }
      }
    } finally {
      try { await connection.query("SELECT pg_advisory_unlock($1)", [741_108_018]); } finally { connection.release(); }
    }
  }

  async healthcheck(): Promise<void> {
    await this.pool.query("SELECT 1");
  }

  async beginEnrollment(input: NewEnrollment): Promise<{ enrollmentId: string }> {
    const enrollmentId = randomUUID();
    try {
      await this.pool.query(
        `INSERT INTO scopra_enrollments(
          id, request_id, device_public_key, device_fingerprint, machine_name,
          local_workspace_id, workspace_path, pairing_code_hash, status, created_at, expires_at
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9,$10)`,
        [enrollmentId, input.requestId, input.publicKey, input.fingerprint, input.machineName,
          input.localWorkspaceId, input.workspacePath, input.pairingCodeHash, input.createdAt, input.expiresAt],
      );
    } catch (error) {
      if (isUniqueViolation(error)) throw new StoreConflictError("enrollment request was already used");
      throw error;
    }
    return { enrollmentId };
  }

  async inspectEnrollment(pairingCodeHash: Buffer, now: Date): Promise<EnrollmentInspection | null> {
    const result = await this.pool.query(
      `SELECT id, device_public_key, device_fingerprint, machine_name, workspace_path, expires_at
       FROM scopra_enrollments
       WHERE pairing_code_hash = $1 AND status = 'pending' AND expires_at > $2`,
      [pairingCodeHash, now],
    );
    const row = result.rows[0];
    if (!row) return null;
    return {
      enrollmentId: row.id,
      publicKey: row.device_public_key,
      fingerprint: row.device_fingerprint,
      machineName: row.machine_name,
      workspacePath: row.workspace_path,
      expiresAt: new Date(row.expires_at),
    };
  }

  async claimEnrollment(input: {
    pairingCodeHash: Buffer;
    githubSubject: string;
    expectedFingerprint: string;
    expectedWorkspacePath: string;
    now: Date;
  }): Promise<{ machineId: string; workspaceId: string } | null> {
    const client = await this.pool.connect();
    try {
      await client.query("BEGIN");
      const selected = await client.query(
        `SELECT id, device_public_key, device_fingerprint, machine_name, local_workspace_id,
                workspace_path, status, expires_at
         FROM scopra_enrollments WHERE pairing_code_hash = $1 FOR UPDATE`,
        [input.pairingCodeHash],
      );
      const row = selected.rows[0];
      if (!row || row.status !== "pending" || new Date(row.expires_at).getTime() <= input.now.getTime() ||
          row.device_fingerprint !== input.expectedFingerprint || row.workspace_path !== input.expectedWorkspacePath) {
        await client.query("COMMIT");
        return null;
      }

      const existingMachine = await client.query(
        `SELECT id, owner_github_subject FROM scopra_machines
         WHERE device_public_key = $1 AND revoked_at IS NULL FOR UPDATE`,
        [row.device_public_key],
      );
      let machineId: string;
      if (existingMachine.rows[0]) {
        if (existingMachine.rows[0].owner_github_subject !== input.githubSubject) {
          throw new StoreConflictError("machine is already paired to another account");
        }
        machineId = existingMachine.rows[0].id;
      } else {
        machineId = randomUUID();
        await client.query(
          `INSERT INTO scopra_machines(id, owner_github_subject, device_public_key, device_fingerprint,
                                       machine_name, created_at)
           VALUES ($1,$2,$3,$4,$5,$6)`,
          [machineId, input.githubSubject, row.device_public_key, row.device_fingerprint, row.machine_name, input.now],
        );
      }

      const existingWorkspace = await client.query(
        `SELECT id, workspace_path FROM scopra_workspaces
         WHERE machine_id = $1 AND local_workspace_id = $2 FOR UPDATE`,
        [machineId, row.local_workspace_id],
      );
      let workspaceId: string;
      if (existingWorkspace.rows[0]) {
        if (existingWorkspace.rows[0].workspace_path !== row.workspace_path) {
          throw new StoreConflictError("workspace ID is already paired to a different path");
        }
        workspaceId = existingWorkspace.rows[0].id;
      } else {
        workspaceId = randomUUID();
        await client.query(
          `INSERT INTO scopra_workspaces(id, machine_id, local_workspace_id, workspace_path, created_at)
           VALUES ($1,$2,$3,$4,$5)`,
          [workspaceId, machineId, row.local_workspace_id, row.workspace_path, input.now],
        );
      }
      await client.query(
        `UPDATE scopra_enrollments SET status = 'accepted', owner_github_subject = $2,
          machine_id = $3, workspace_id = $4, claimed_at = $5 WHERE id = $1`,
        [row.id, input.githubSubject, machineId, workspaceId, input.now],
      );
      await client.query("COMMIT");
      return { machineId, workspaceId };
    } catch (error) {
      await client.query("ROLLBACK");
      if (isUniqueViolation(error)) throw new StoreConflictError("machine is already paired");
      throw error;
    } finally {
      client.release();
    }
  }

  async enrollmentPublicKey(enrollmentId: string): Promise<Buffer | null> {
    const result = await this.pool.query(
      "SELECT device_public_key FROM scopra_enrollments WHERE id = $1",
      [enrollmentId],
    );
    return result.rows[0]?.device_public_key ?? null;
  }

  async enrollmentStatus(enrollmentId: string, publicKey: Buffer, now: Date): Promise<EnrollmentStatus | null> {
    const result = await this.pool.query(
      `UPDATE scopra_enrollments
       SET status = 'expired'
       WHERE id = $1 AND status = 'pending' AND expires_at <= $3 AND device_public_key = $2`,
      [enrollmentId, publicKey, now],
    );
    void result;
    const selected = await this.pool.query(
      `SELECT status, machine_id, workspace_id FROM scopra_enrollments
       WHERE id = $1 AND device_public_key = $2`,
      [enrollmentId, publicKey],
    );
    const row = selected.rows[0];
    if (!row) return null;
    return {
      status: row.status,
      ...(row.machine_id ? { machineId: row.machine_id } : {}),
      ...(row.workspace_id ? { workspaceId: row.workspace_id } : {}),
    };
  }

  async machinePublicKey(machineId: string): Promise<Buffer | null> {
    const result = await this.pool.query(
      "SELECT device_public_key FROM scopra_machines WHERE id = $1 AND revoked_at IS NULL",
      [machineId],
    );
    return result.rows[0]?.device_public_key ?? null;
  }

  async consumeNonce(fingerprint: string, nonce: string, expiresAt: Date, now: Date): Promise<boolean> {
    const client = await this.pool.connect();
    try {
      await client.query("BEGIN");
      await client.query("DELETE FROM scopra_device_nonces WHERE expires_at <= $1", [now]);
      const inserted = await client.query(
        `INSERT INTO scopra_device_nonces(device_fingerprint, nonce, expires_at)
         VALUES ($1,$2,$3) ON CONFLICT DO NOTHING RETURNING nonce`,
        [fingerprint, nonce, expiresAt],
      );
      await client.query("COMMIT");
      return inserted.rowCount === 1;
    } catch (error) {
      await client.query("ROLLBACK");
      throw error;
    } finally {
      client.release();
    }
  }

  async heartbeat(machineId: string, publicKey: Buffer, now: Date): Promise<boolean> {
    const result = await this.pool.query(
      `UPDATE scopra_machines SET last_seen_at = $3
       WHERE id = $1 AND device_public_key = $2 AND revoked_at IS NULL RETURNING id`,
      [machineId, publicKey, now],
    );
    return result.rowCount === 1;
  }

  async listMachines(githubSubject: string, now: Date): Promise<MachineSummary[]> {
    const result = await this.pool.query(
      `SELECT m.id, m.machine_name, m.device_fingerprint, m.created_at, m.last_seen_at,
              COALESCE(json_agg(json_build_object('id',w.id,'localWorkspaceId',w.local_workspace_id,'path',w.workspace_path))
                FILTER (WHERE w.id IS NOT NULL), '[]'::json) AS workspaces
       FROM scopra_machines m
       LEFT JOIN scopra_workspaces w ON w.machine_id = m.id
       WHERE m.owner_github_subject = $1 AND m.revoked_at IS NULL
       GROUP BY m.id
       ORDER BY m.created_at DESC`,
      [githubSubject],
    );
    const onlineAfter = now.getTime() - 90_000;
    return result.rows.map((row) => {
      const lastSeenAt = row.last_seen_at ? new Date(row.last_seen_at).toISOString() : null;
      return {
        id: row.id,
        machineName: row.machine_name,
        fingerprint: row.device_fingerprint,
        createdAt: new Date(row.created_at).toISOString(),
        lastSeenAt,
        presence: lastSeenAt && new Date(lastSeenAt).getTime() > onlineAfter ? "online" : "offline",
        workspaces: row.workspaces as MachineWorkspace[],
      };
    });
  }

  async close(): Promise<void> {
    await this.pool.end();
  }
}

export function createPool(databaseUrl: string, production: boolean, tlsMode: DatabaseTLSMode = "verify-full"): Pool {
  return new Pool({
    connectionString: databaseUrl,
    max: 10,
    idleTimeoutMillis: 30_000,
    connectionTimeoutMillis: 5_000,
    ...(production ? {
      // Render's internal Postgres endpoint uses a self-signed certificate.
      // `require` still negotiates TLS and fails if TLS is unavailable, but
      // does not authenticate the certificate; only opt into it on a private
      // provider network. The default verifies the public CA and hostname.
      ssl: { rejectUnauthorized: tlsMode === "verify-full" },
    } : {}),
    application_name: "scopra-caloris",
  });
}

function isUniqueViolation(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && (error as { code?: unknown }).code === "23505";
}

// Ensure migrations compile with the same connection type at their call site.
export type MigrationConnection = PoolClient;
