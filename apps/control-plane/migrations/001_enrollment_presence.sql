CREATE TABLE scopra_enrollments (
  id uuid PRIMARY KEY,
  request_id text NOT NULL,
  device_public_key bytea NOT NULL CHECK (octet_length(device_public_key) = 32),
  device_fingerprint char(24) NOT NULL,
  machine_name text NOT NULL CHECK (char_length(machine_name) BETWEEN 1 AND 128),
  local_workspace_id char(32) NOT NULL,
  workspace_path text NOT NULL CHECK (char_length(workspace_path) BETWEEN 1 AND 4095),
  pairing_code_hash bytea NOT NULL UNIQUE CHECK (octet_length(pairing_code_hash) = 32),
  status text NOT NULL CHECK (status IN ('pending', 'accepted', 'expired')),
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  owner_github_subject text,
  machine_id uuid,
  workspace_id uuid,
  claimed_at timestamptz,
  CONSTRAINT scopra_enrollments_request_unique UNIQUE (device_public_key, request_id),
  CONSTRAINT scopra_enrollments_expiry_after_creation CHECK (expires_at > created_at),
  CONSTRAINT scopra_enrollments_accepted_fields CHECK (
    (status = 'accepted' AND owner_github_subject IS NOT NULL AND machine_id IS NOT NULL AND workspace_id IS NOT NULL AND claimed_at IS NOT NULL)
    OR (status <> 'accepted' AND owner_github_subject IS NULL AND machine_id IS NULL AND workspace_id IS NULL AND claimed_at IS NULL)
  )
);

CREATE INDEX scopra_enrollments_pending_expiry_idx
  ON scopra_enrollments(expires_at) WHERE status = 'pending';

CREATE TABLE scopra_machines (
  id uuid PRIMARY KEY,
  owner_github_subject text NOT NULL CHECK (owner_github_subject ~ '^[0-9]{1,20}$'),
  device_public_key bytea NOT NULL UNIQUE CHECK (octet_length(device_public_key) = 32),
  device_fingerprint char(24) NOT NULL UNIQUE,
  machine_name text NOT NULL CHECK (char_length(machine_name) BETWEEN 1 AND 128),
  created_at timestamptz NOT NULL,
  last_seen_at timestamptz,
  revoked_at timestamptz
);

CREATE INDEX scopra_machines_owner_idx ON scopra_machines(owner_github_subject, created_at DESC);

CREATE TABLE scopra_workspaces (
  id uuid PRIMARY KEY,
  machine_id uuid NOT NULL REFERENCES scopra_machines(id) ON DELETE CASCADE,
  local_workspace_id char(32) NOT NULL,
  workspace_path text NOT NULL CHECK (char_length(workspace_path) BETWEEN 1 AND 4095),
  created_at timestamptz NOT NULL,
  CONSTRAINT scopra_workspaces_machine_local_id_unique UNIQUE (machine_id, local_workspace_id)
);

CREATE INDEX scopra_workspaces_machine_idx ON scopra_workspaces(machine_id);

CREATE TABLE scopra_device_nonces (
  device_fingerprint char(24) NOT NULL,
  nonce char(22) NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (device_fingerprint, nonce)
);

CREATE INDEX scopra_device_nonces_expiry_idx ON scopra_device_nonces(expires_at);
