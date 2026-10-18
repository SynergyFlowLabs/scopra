package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateKeyIsStableAndOwnerOnly(t *testing.T) {
	stateDir := privateTestDir(t)
	first, err := LoadOrCreateKey(stateDir)
	if err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	second, err := LoadOrCreateKey(stateDir)
	if err != nil {
		t.Fatalf("LoadOrCreateKey() repeat error = %v", err)
	}
	if !equalBytes(first.Private, second.Private) || !equalBytes(first.Public, second.Public) {
		t.Fatal("machine key changed when it was loaded a second time")
	}
	if len(first.Private) != ed25519.PrivateKeySize || len(first.Public) != ed25519.PublicKeySize {
		t.Fatalf("key lengths = %d/%d, want %d/%d", len(first.Private), len(first.Public), ed25519.PrivateKeySize, ed25519.PublicKeySize)
	}
	info, err := os.Lstat(filepath.Join(stateDir, keyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("machine key mode = %v, want regular owner-only 0600", info.Mode())
	}
	loaded, err := LoadKey(stateDir)
	if err != nil || !equalBytes(loaded.Private, first.Private) {
		t.Fatalf("LoadKey() = %v, %v; want the existing key", loaded.Public, err)
	}
}

func TestLoadOrCreateKeyFailsClosedForInsecureStateOrKey(t *testing.T) {
	t.Run("group-readable state directory", func(t *testing.T) {
		stateDir := t.TempDir()
		if err := os.Chmod(stateDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateKey(stateDir); !errors.Is(err, ErrInsecureStateDir) {
			t.Fatalf("LoadOrCreateKey() error = %v, want ErrInsecureStateDir", err)
		}
	})
	t.Run("symlink key file", func(t *testing.T) {
		stateDir := privateTestDir(t)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, make([]byte, ed25519.PrivateKeySize), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(stateDir, keyFileName)); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateKey(stateDir); !errors.Is(err, ErrInsecureKeyFile) {
			t.Fatalf("LoadOrCreateKey() error = %v, want ErrInsecureKeyFile", err)
		}
	})
	t.Run("permissive key mode", func(t *testing.T) {
		stateDir := privateTestDir(t)
		key := make([]byte, ed25519.PrivateKeySize)
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(stateDir, keyFileName)
		if err := os.WriteFile(path, key, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateKey(stateDir); !errors.Is(err, ErrInsecureKeyFile) {
			t.Fatalf("LoadOrCreateKey() error = %v, want ErrInsecureKeyFile", err)
		}
	})
	t.Run("corrupt key length", func(t *testing.T) {
		stateDir := privateTestDir(t)
		path := filepath.Join(stateDir, keyFileName)
		if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadOrCreateKey(stateDir); !errors.Is(err, ErrInsecureKeyFile) {
			t.Fatalf("LoadOrCreateKey() error = %v, want ErrInsecureKeyFile", err)
		}
	})
}

func TestEnrollmentSignaturesMatchVersionedCanonicalForms(t *testing.T) {
	stateDir := privateTestDir(t)
	key, err := LoadOrCreateKey(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(requestID); err != nil || len(raw) != 16 {
		t.Fatalf("request ID %q is not 16 random bytes: %v", requestID, err)
	}
	publicKey, err := EncodePublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	message := BeginEnrollmentSigningMessage(requestID, "dev-machine", "4cfef09b057155cd8fc9826e40bd3f25", publicKey, "/home/user/code/project")
	signature, err := Sign(key, message)
	if err != nil {
		t.Fatal(err)
	}
	rawSignature, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(key.Public, message, rawSignature) {
		t.Fatalf("signature did not verify: %v", err)
	}
	want := strings.Join([]string{
		"scopra/device-enrollment-begin/v1",
		requestID,
		"dev-machine",
		"4cfef09b057155cd8fc9826e40bd3f25",
		publicKey,
		"/home/user/code/project",
	}, "\n")
	if string(message) != want {
		t.Fatalf("canonical enrollment message = %q, want %q", message, want)
	}
	fingerprint, err := Fingerprint(key)
	if err != nil || len(fingerprint) != 24 {
		t.Fatalf("Fingerprint() = %q, %v; want 24 hex characters", fingerprint, err)
	}
	if _, err := hex.DecodeString(fingerprint); err != nil {
		t.Fatalf("fingerprint %q is not lowercase hex: %v", fingerprint, err)
	}
}

func TestCloudConfigSaveLoadAndWorkspaceUpsert(t *testing.T) {
	stateDir := privateTestDir(t)
	config := CloudConfig{
		SchemaVersion: 1,
		APIURL:        "https://api.scopra.example",
		DashboardURL:  "https://app.scopra.example",
		MachineID:     "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e",
		Workspaces:    []WorkspaceBinding{},
	}
	binding := WorkspaceBinding{
		LocalWorkspaceID: "4cfef09b057155cd8fc9826e40bd3f25",
		CloudWorkspaceID: "6a597469-2af4-4f39-9704-4e1c3d9dc5d2",
		Path:             "/home/user/code/project",
	}
	updated, err := UpsertWorkspaceBinding(config, binding)
	if err != nil {
		t.Fatal(err)
	}
	again, err := UpsertWorkspaceBinding(updated, binding)
	if err != nil || len(again.Workspaces) != 1 {
		t.Fatalf("idempotent upsert = %+v, %v", again, err)
	}
	if _, err := UpsertWorkspaceBinding(updated, WorkspaceBinding{
		LocalWorkspaceID: binding.LocalWorkspaceID,
		CloudWorkspaceID: binding.CloudWorkspaceID,
		Path:             "/home/user/code/other",
	}); !errors.Is(err, ErrWorkspaceConflict) {
		t.Fatalf("conflicting upsert error = %v, want ErrWorkspaceConflict", err)
	}
	if err := SaveCloudConfig(stateDir, updated); err != nil {
		t.Fatalf("SaveCloudConfig() error = %v", err)
	}
	loaded, err := LoadCloudConfig(stateDir)
	if err != nil {
		t.Fatalf("LoadCloudConfig() error = %v", err)
	}
	if len(loaded.Workspaces) != 1 || loaded.Workspaces[0] != binding || loaded.MachineID != config.MachineID {
		t.Fatalf("loaded config = %+v, want saved config %+v", loaded, updated)
	}
	info, err := os.Lstat(filepath.Join(stateDir, cloudConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cloud config mode = %v, want 0600", info.Mode())
	}
	encoded, err := os.ReadFile(filepath.Join(stateDir, cloudConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "pairingCode") || strings.Contains(string(encoded), "token") {
		t.Fatalf("cloud config contains a reusable credential: %s", encoded)
	}
}

func TestCloudConfigRejectsDuplicateJSONKeys(t *testing.T) {
	stateDir := privateTestDir(t)
	path := filepath.Join(stateDir, cloudConfigFileName)
	contents := []byte(`{"schemaVersion":1,"schemaVersion":1,"apiUrl":"https://api.example","dashboardUrl":"https://app.example","machineId":"2c0672b9-89e6-4cd5-9507-2044e1e4ae5e","workspaces":[]}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCloudConfig(stateDir); !errors.Is(err, ErrInvalidCloud) {
		t.Fatalf("LoadCloudConfig() error = %v, want ErrInvalidCloud", err)
	}
}

func TestCloudConfigRejectsUnsafeOrUntrustedValues(t *testing.T) {
	config := CloudConfig{
		SchemaVersion: 1,
		APIURL:        "https://api.scopra.example",
		DashboardURL:  "https://app.scopra.example",
		MachineID:     "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e",
		Workspaces:    []WorkspaceBinding{},
	}
	cases := []CloudConfig{
		{SchemaVersion: 2, APIURL: config.APIURL, DashboardURL: config.DashboardURL, MachineID: config.MachineID},
		{SchemaVersion: 1, APIURL: "http://api.example", DashboardURL: config.DashboardURL, MachineID: config.MachineID},
		{SchemaVersion: 1, APIURL: "https://api.example/path", DashboardURL: config.DashboardURL, MachineID: config.MachineID},
		{SchemaVersion: 1, APIURL: config.APIURL, DashboardURL: config.DashboardURL, MachineID: config.MachineID,
			Workspaces: []WorkspaceBinding{{LocalWorkspaceID: "bad", CloudWorkspaceID: "bad", Path: "/tmp"}}},
	}
	for _, candidate := range cases {
		if err := validateCloudConfig(candidate); !errors.Is(err, ErrInvalidCloud) {
			t.Fatalf("validateCloudConfig(%+v) error = %v, want ErrInvalidCloud", candidate, err)
		}
	}
}

func privateTestDir(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return stateDir
}
