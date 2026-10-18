// Package presence sends signed, outbound-only machine heartbeats to Caloris.
// It does not accept commands or open an inbound listener.
package presence

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/identity"
)

const (
	interval       = 30 * time.Second
	requestTimeout = 10 * time.Second
	responseLimit  = 4096
)

type presenceRequest struct {
	ProtocolVersion int    `json:"protocolVersion"`
	MachineID       string `json:"machineId"`
	Timestamp       int64  `json:"timestamp"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

// SendOnce sends one signed presence update. It uses the provided client so
// tests can verify the complete protocol against a local loopback server.
func SendOnce(ctx context.Context, client *http.Client, config identity.CloudConfig, key identity.KeyPair, now time.Time) error {
	if ctx == nil {
		return errors.New("presence: context is required")
	}
	if client == nil {
		return errors.New("presence: HTTP client is required")
	}
	origin, err := validateOrigin(config.APIURL)
	if err != nil {
		return err
	}
	if config.MachineID == "" {
		return errors.New("presence: machine ID is required")
	}
	nonce, err := identity.NewNonce()
	if err != nil {
		return fmt.Errorf("generate presence nonce: %w", err)
	}
	timestamp := now.Unix()
	signature, err := identity.Sign(key, identity.PresenceSigningMessage(config.MachineID, timestamp, nonce))
	if err != nil {
		return fmt.Errorf("sign presence request: %w", err)
	}
	body, err := json.Marshal(presenceRequest{
		ProtocolVersion: 1,
		MachineID:       config.MachineID,
		Timestamp:       timestamp,
		Nonce:           nonce,
		Signature:       signature,
	})
	if err != nil {
		return fmt.Errorf("encode presence request: %w", err)
	}
	base, _ := url.Parse(origin)
	endpoint := base.ResolveReference(&url.URL{Path: "/v1/presence"})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create presence request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send presence request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return fmt.Errorf("Caloris returned HTTP %d", response.StatusCode)
	}
	return nil
}

// Run sends immediately, then every 30 seconds (the server considers a machine
// online for 90 seconds). Transient failures are retried; warnings are
// rate-limited so an offline API does not flood Mio's journal.
func Run(ctx context.Context, config identity.CloudConfig, key identity.KeyPair, logf func(string, ...any)) {
	if ctx == nil {
		return
	}
	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastWarning time.Time
	warned := false
	for {
		err := SendOnce(ctx, client, config, key, time.Now())
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if logf != nil && (!warned || time.Since(lastWarning) >= 5*time.Minute) {
				logf("presence update failed: %v", err)
				lastWarning = time.Now()
				warned = true
			}
		} else if warned {
			if logf != nil {
				logf("presence updates resumed")
			}
			warned = false
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func validateOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Opaque != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("presence: invalid Caloris API origin")
	}
	if parsed.Scheme == "https" {
		return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), nil
	}
	if parsed.Scheme != "http" {
		return "", errors.New("presence: Caloris API must use HTTPS")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("presence: HTTP is allowed only for loopback development")
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), nil
}

// validateSignedRequest verifies the wire payload's canonical signature.
func validateSignedRequest(request presenceRequest, publicKey ed25519.PublicKey) bool {
	if request.ProtocolVersion != 1 || request.MachineID == "" || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	nonce, err := base64.RawURLEncoding.DecodeString(request.Nonce)
	if err != nil || len(nonce) != 16 || base64.RawURLEncoding.EncodeToString(nonce) != request.Nonce {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(request.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(signature) != request.Signature {
		return false
	}
	message := identity.PresenceSigningMessage(request.MachineID, request.Timestamp, request.Nonce)
	return ed25519.Verify(publicKey, message, signature)
}
