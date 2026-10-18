package presence

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/identity"
)

func TestSendOnceSignsFreshPresenceRequest(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := identity.KeyPair{Public: publicKey, Private: privateKey}
	const machineID = "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e"
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	var captured presenceRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/presence" {
			t.Errorf("request = %s %s, want POST /v1/presence", request.Method, request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", request.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request body: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	config := identity.CloudConfig{APIURL: server.URL, MachineID: machineID}
	if err := SendOnce(t.Context(), client, config, key, now); err != nil {
		t.Fatalf("SendOnce() error = %v", err)
	}
	if captured.ProtocolVersion != 1 || captured.MachineID != machineID || captured.Timestamp != now.Unix() {
		t.Fatalf("captured request = %+v", captured)
	}
	if !validateSignedRequest(captured, publicKey) {
		t.Fatal("presence request signature did not verify")
	}
}

func TestSendOnceRejectsRedirectsAndNonLoopbackHTTP(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", target.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	config := identity.CloudConfig{APIURL: source.URL, MachineID: "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e"}
	if err := SendOnce(t.Context(), client, config, identity.KeyPair{Public: publicKey, Private: privateKey}, time.Now()); err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("SendOnce(redirect) error = %v, want HTTP 307", err)
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests, want zero", targetRequests.Load())
	}
	config.APIURL = "http://api.example.test"
	if err := SendOnce(t.Context(), client, config, identity.KeyPair{Public: publicKey, Private: privateKey}, time.Now()); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("SendOnce(non-loopback HTTP) error = %v, want loopback validation error", err)
	}
}

func TestSendOnceRejectsNonSuccessAndInvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	config := identity.CloudConfig{APIURL: server.URL, MachineID: "2c0672b9-89e6-4cd5-9507-2044e1e4ae5e"}
	if err := SendOnce(t.Context(), client, config, identity.KeyPair{Public: publicKey, Private: privateKey}, time.Now()); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("SendOnce(401) error = %v, want HTTP 401", err)
	}
	if err := SendOnce(t.Context(), client, config, identity.KeyPair{}, time.Now()); err == nil || !strings.Contains(err.Error(), "invalid machine key") {
		t.Fatalf("SendOnce(invalid key) error = %v, want invalid machine key", err)
	}
}

func TestRunReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	Run(ctx, identity.CloudConfig{}, identity.KeyPair{}, nil)
}
