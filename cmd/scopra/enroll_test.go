package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/identity"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

const (
	testCloudMachineID   = "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e"
	testCloudWorkspaceID = "6a597469-2af4-4f39-9704-4e1c3d9dc5d2"
)

func TestRunInitEnrollsWorkspaceAndPersistsNoPairingCode(t *testing.T) {
	if !localipc.PlatformSupported() {
		t.Skip("authenticated local IPC is Linux-only")
	}
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	workspacePath := filepath.Join(root, "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	stopDaemon := startInitDaemon(t, stateDir)
	defer stopDaemon()

	const pairingCode = "AAAAAAAAAAAAAAAAAAAAAA"
	var beginCount atomic.Int32
	var statusCount atomic.Int32
	var keyMu sync.Mutex
	var devicePublicKey ed25519.PublicKey
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != "" {
			t.Errorf("request included a query string: %s", request.URL.String())
		}
		switch request.URL.Path {
		case "/v1/enrollments":
			beginCount.Add(1)
			var body enrollmentBeginRequest
			if err := decodeStrictAPIJSON(readBody(t, request), &body); err != nil {
				t.Errorf("decode begin request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			publicBytes, err := base64.RawURLEncoding.DecodeString(body.PublicKey)
			if err != nil || len(publicBytes) != ed25519.PublicKeySize {
				t.Errorf("invalid public key: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			signature, err := base64.RawURLEncoding.DecodeString(body.Signature)
			message := identity.BeginEnrollmentSigningMessage(body.RequestID, body.MachineName, body.WorkspaceID, body.PublicKey, body.WorkspacePath)
			if err != nil || !ed25519.Verify(ed25519.PublicKey(publicBytes), message, signature) {
				t.Errorf("begin request signature is invalid: %v", err)
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			keyMu.Lock()
			devicePublicKey = append(ed25519.PublicKey(nil), publicBytes...)
			keyMu.Unlock()
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(enrollmentBeginResponse{
				EnrollmentID: "5a222a2c-9a0b-4b25-9743-ae46e15b0f75",
				PairingCode:  pairingCode,
				ExpiresAt:    time.Now().Add(4 * time.Minute).UTC().Format(time.RFC3339Nano),
			})
		case "/v1/enrollments/status":
			statusCount.Add(1)
			var body enrollmentStatusRequest
			if err := decodeStrictAPIJSON(readBody(t, request), &body); err != nil {
				t.Errorf("decode status request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			keyMu.Lock()
			publicBytes := append(ed25519.PublicKey(nil), devicePublicKey...)
			keyMu.Unlock()
			signature, err := base64.RawURLEncoding.DecodeString(body.Signature)
			message := identity.EnrollmentStatusSigningMessage(body.EnrollmentID, body.Timestamp, body.Nonce)
			if err != nil || len(publicBytes) != ed25519.PublicKeySize || !ed25519.Verify(publicBytes, message, signature) {
				t.Errorf("status request signature is invalid: %v", err)
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(enrollmentStatusResponse{
				Status: "accepted", MachineID: testCloudMachineID, WorkspaceID: testCloudWorkspaceID,
			})
		default:
			t.Errorf("unexpected API route %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var stdout, stderr strings.Builder
	args := []string{
		"--state-dir", stateDir,
		"--api-url", server.URL,
		"--dashboard-url", "http://127.0.0.1:3000",
		"--timeout", "4s",
		workspacePath,
	}
	if err := runInit(ctx, args, &stdout, &stderr); err != nil {
		t.Fatalf("runInit() error = %v; stdout=%q; stderr=%q", err, stdout.String(), stderr.String())
	}
	if beginCount.Load() != 1 || statusCount.Load() != 1 {
		t.Fatalf("API calls begin=%d status=%d, want one each", beginCount.Load(), statusCount.Load())
	}
	if !strings.Contains(stdout.String(), pairingCode) || !strings.Contains(stdout.String(), "paired successfully") {
		t.Fatalf("runInit() output = %q, want code and success", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("runInit() wrote unexpected stderr: %q", stderr.String())
	}
	config, err := identity.LoadCloudConfig(stateDir)
	if err != nil {
		t.Fatalf("LoadCloudConfig() error = %v", err)
	}
	if config.MachineID != testCloudMachineID || len(config.Workspaces) != 1 || config.Workspaces[0].CloudWorkspaceID != testCloudWorkspaceID {
		t.Fatalf("saved cloud config = %+v", config)
	}
	encoded, err := os.ReadFile(filepath.Join(stateDir, "cloud.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), pairingCode) || strings.Contains(string(encoded), "pairingCode") {
		t.Fatalf("one-time pairing code was persisted: %s", encoded)
	}

	stdout.Reset()
	if err := runInit(ctx, args, &stdout, &stderr); err != nil {
		t.Fatalf("repeat runInit() error = %v", err)
	}
	if beginCount.Load() != 1 || !strings.Contains(stdout.String(), "already paired") {
		t.Fatalf("repeat init unexpectedly re-enrolled: begin count=%d, output=%q", beginCount.Load(), stdout.String())
	}
}

func TestCanonicalInitWorkspacePathResolvesSymlinksAndRejectsControls(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "real", "project")
	if err := os.Mkdir(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "child"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	got, err := canonicalInitWorkspacePath(filepath.Join(root, "link", "..", "project"))
	if err != nil {
		t.Fatalf("canonicalInitWorkspacePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("canonicalInitWorkspacePath() = %q, want symlink-aware path %q", got, want)
	}
	if _, err := canonicalInitWorkspacePath(root + "/bad\npath"); err == nil {
		t.Fatal("canonicalInitWorkspacePath() accepted a path with a control character")
	}
}

func TestRunInitRejectsUntrustedOriginsBeforeStateChanges(t *testing.T) {
	for _, value := range []string{"http://api.example.test", "https://user:pass@api.example.test", "https://api.example.test/path"} {
		if _, err := normalizeServiceOrigin(value); err == nil {
			t.Errorf("normalizeServiceOrigin(%q) succeeded, want rejection", value)
		}
	}
	if _, err := resolveInitOrigins("", "", identity.CloudConfig{}, false); err == nil {
		t.Fatal("resolveInitOrigins() accepted missing configured origins")
	}
}

func TestDecodeStrictAPIJSONRejectsDuplicateAndUnknownFields(t *testing.T) {
	var response enrollmentBeginResponse
	for _, data := range []string{
		`{"enrollmentId":"one","enrollmentId":"two","pairingCode":"AAAAAAAAAAAAAAAAAAAAAA","expiresAt":"2026-10-18T12:00:00Z"}`,
		`{"enrollmentId":"one","pairingCode":"AAAAAAAAAAAAAAAAAAAAAA","expiresAt":"2026-10-18T12:00:00Z","extra":true}`,
		`{"enrollmentId":"one","pairingCode":"AAAAAAAAAAAAAAAAAAAAAA","expiresAt":"2026-10-18T12:00:00Z"} {}`,
	} {
		if err := decodeStrictAPIJSON([]byte(data), &response); err == nil {
			t.Errorf("decodeStrictAPIJSON(%s) succeeded, want rejection", data)
		}
	}
}

func TestValidPairingCodeRequiresCanonical128BitValue(t *testing.T) {
	if !validPairingCode("AAAAAAAAAAAAAAAAAAAAAA") {
		t.Fatal("valid 128-bit pairing code rejected")
	}
	for _, code := range []string{"", "aaaaaaaaaaaaaaaaaaaaa", "AAAAAAAAAAAAAAAAAAAAA=", "AAAAAAAAAAAAAAAAAAAAAB"} {
		if validPairingCode(code) {
			t.Errorf("validPairingCode(%q) = true, want false", code)
		}
	}
}

func startInitDaemon(t *testing.T, stateDir string) func() {
	t.Helper()
	daemon, err := localipc.Listen(stateDir, "init-test")
	if err != nil {
		t.Fatalf("localipc.Listen() error = %v", err)
	}
	validator, err := workspace.NewValidator(daemon.StateDir())
	if err != nil {
		_ = daemon.Close()
		t.Fatalf("workspace.NewValidator() error = %v", err)
	}
	workspaces, err := registry.Open(daemon.StateDir(), validator)
	if err != nil {
		_ = daemon.Close()
		t.Fatalf("registry.Open() error = %v", err)
	}
	if err := daemon.EnableWorkspaces(workspaces); err != nil {
		_ = daemon.Close()
		t.Fatalf("EnableWorkspaces() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon Serve() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("init test daemon did not stop")
		}
		_ = daemon.Close()
	}
}

func readBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(io.LimitReader(request.Body, maxAPIResponseBytes+1))
	if err != nil {
		t.Errorf("read request body: %v", err)
	}
	return body
}
