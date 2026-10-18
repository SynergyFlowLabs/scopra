package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/internal/identity"
	"github.com/SynergyFlowLabs/scopra/internal/localipc"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

const (
	pairingCodeLifetime = 5 * time.Minute
	pollInterval        = 2 * time.Second
	maxAPIResponseBytes = 16 * 1024
)

var (
	cloudUUIDPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	pairingCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
	machineNameControl = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f\x{2028}\x{2029}]`)
	pathControl        = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f\x{2028}\x{2029}]`)
)

type enrollmentBeginRequest struct {
	ProtocolVersion int    `json:"protocolVersion"`
	RequestID       string `json:"requestId"`
	PublicKey       string `json:"publicKey"`
	MachineName     string `json:"machineName"`
	WorkspaceID     string `json:"workspaceId"`
	WorkspacePath   string `json:"workspacePath"`
	Signature       string `json:"signature"`
}

type enrollmentBeginResponse struct {
	EnrollmentID string `json:"enrollmentId"`
	PairingCode  string `json:"pairingCode"`
	ExpiresAt    string `json:"expiresAt"`
}

type enrollmentStatusRequest struct {
	ProtocolVersion int    `json:"protocolVersion"`
	EnrollmentID    string `json:"enrollmentId"`
	Timestamp       int64  `json:"timestamp"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

type enrollmentStatusResponse struct {
	Status      string `json:"status"`
	MachineID   string `json:"machineId,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
}

type apiStatusError struct {
	statusCode int
}

func (err *apiStatusError) Error() string {
	return fmt.Sprintf("Caloris returned HTTP %d", err.statusCode)
}

type enrollmentProtocolError struct {
	err error
}

func (err *enrollmentProtocolError) Error() string { return err.err.Error() }
func (err *enrollmentProtocolError) Unwrap() error { return err.err }

// runInit enrolls one registered local workspace under the authenticated
// Scopra account. The one-time pairing code remains only in memory and on the
// user's terminal; persistent cloud metadata contains no bearer credential.
func runInit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopra init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(stderr) }
	stateDirFlag := flags.String("state-dir", "", "Mio state directory")
	apiURLFlag := flags.String("api-url", os.Getenv("SCOPRA_API_URL"), "Caloris API origin (HTTPS; loopback HTTP for development)")
	dashboardURLFlag := flags.String("dashboard-url", os.Getenv("SCOPRA_DASHBOARD_URL"), "Scopra dashboard origin (HTTPS; loopback HTTP for development)")
	timeoutFlag := flags.Duration("timeout", pairingCodeLifetime, "maximum time to wait for dashboard approval (1s to 5m)")
	if err := flags.Parse(args); err != nil {
		return &usageError{err: err}
	}
	if flags.NArg() > 1 {
		return failUsage(stderr, "init takes at most one workspace PATH")
	}
	if *timeoutFlag <= 0 || *timeoutFlag > pairingCodeLifetime {
		return failUsage(stderr, "--timeout must be greater than zero and no more than %s", pairingCodeLifetime)
	}
	workspaceArg := "."
	if flags.NArg() == 1 {
		workspaceArg = flags.Arg(0)
	}

	stateDir, err := localipc.ResolveStateDir(*stateDirFlag)
	if err != nil {
		return fmt.Errorf("resolve Mio state directory: %w", err)
	}
	if err := localipc.CheckStateDir(stateDir); err != nil {
		return fmt.Errorf("check Mio state directory: %w", err)
	}

	storedConfig, configErr := identity.LoadCloudConfig(stateDir)
	hasStoredConfig := configErr == nil
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		return fmt.Errorf("load existing cloud pairing state: %w", configErr)
	}
	apiURL, dashboardURL, err := resolveInitOrigins(*apiURLFlag, *dashboardURLFlag, storedConfig, hasStoredConfig)
	if err != nil {
		return err
	}

	key, err := identity.LoadOrCreateKey(stateDir)
	if err != nil {
		return fmt.Errorf("load machine identity: %w", err)
	}
	workspacePath, err := canonicalInitWorkspacePath(workspaceArg)
	if err != nil {
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	registered, err := ensureInitWorkspace(ctx, stateDir, workspacePath)
	if err != nil {
		return fmt.Errorf("register workspace with Mio: %w", err)
	}

	if hasStoredConfig {
		for _, binding := range storedConfig.Workspaces {
			if binding.LocalWorkspaceID != registered.ID {
				continue
			}
			if binding.Path != registered.Path {
				return identity.ErrWorkspaceConflict
			}
			_, err := fmt.Fprintf(stdout, "Workspace %s is already paired with this machine.\n", terminalSafe(registered.Path))
			return err
		}
	}

	machineName, err := localMachineName()
	if err != nil {
		return err
	}
	requestID, err := identity.NewRequestID()
	if err != nil {
		return fmt.Errorf("create enrollment request ID: %w", err)
	}
	publicKey, err := identity.EncodePublicKey(key)
	if err != nil {
		return err
	}
	begin := enrollmentBeginRequest{
		ProtocolVersion: 1,
		RequestID:       requestID,
		PublicKey:       publicKey,
		MachineName:     machineName,
		WorkspaceID:     registered.ID,
		WorkspacePath:   registered.Path,
	}
	begin.Signature, err = identity.Sign(key, identity.BeginEnrollmentSigningMessage(
		begin.RequestID, begin.MachineName, begin.WorkspaceID, begin.PublicKey, begin.WorkspacePath,
	))
	if err != nil {
		return fmt.Errorf("sign enrollment request: %w", err)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	beginStatus, beginBody, err := postEnrollmentJSON(ctx, client, apiURL, "/v1/enrollments", begin)
	if err != nil {
		return fmt.Errorf("contact Caloris to begin enrollment: %w", err)
	}
	if beginStatus != http.StatusCreated {
		return fmt.Errorf("Caloris refused enrollment (HTTP %d)", beginStatus)
	}
	var created enrollmentBeginResponse
	if err := decodeStrictAPIJSON(beginBody, &created); err != nil {
		return fmt.Errorf("Caloris returned an invalid enrollment response: %w", err)
	}
	if !cloudUUIDPattern.MatchString(created.EnrollmentID) || !validPairingCode(created.PairingCode) {
		return errors.New("Caloris returned an invalid enrollment ID or one-time pairing code")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, created.ExpiresAt)
	if err != nil || !expiresAt.After(time.Now()) || expiresAt.After(time.Now().Add(pairingCodeLifetime+90*time.Second)) {
		return errors.New("Caloris returned an invalid pairing-code expiration")
	}

	fingerprint, err := identity.Fingerprint(key)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout,
		"\nMachine fingerprint: %s\nWorkspace: %s\n\nOpen %s and enter this one-time code:\n\n  %s\n\nThe code is not saved. Waiting for you to confirm the fingerprint and workspace path in the dashboard...\n",
		fingerprint, terminalSafe(registered.Path), dashboardURL, created.PairingCode); err != nil {
		return err
	}

	deadline := time.Now().Add(*timeoutFlag)
	if expiresAt.Before(deadline) {
		deadline = expiresAt
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return errors.New("pairing timed out; no workspace was paired (run scopra init again to create a new code)")
		}
		status, response, requestErr := pollEnrollment(ctx, client, apiURL, created.EnrollmentID, key)
		if requestErr == nil {
			switch status {
			case "pending":
			case "expired":
				return errors.New("the pairing code expired before approval; run scopra init again")
			case "accepted":
				if !cloudUUIDPattern.MatchString(response.MachineID) || !cloudUUIDPattern.MatchString(response.WorkspaceID) {
					return errors.New("Caloris returned invalid paired machine or workspace identifiers")
				}
				if hasStoredConfig && storedConfig.MachineID != response.MachineID {
					return errors.New("Caloris returned a different machine ID than this machine's saved pairing")
				}
				config := identity.CloudConfig{
					SchemaVersion: 1,
					APIURL:        apiURL,
					DashboardURL:  dashboardURL,
					MachineID:     response.MachineID,
					Workspaces:    []identity.WorkspaceBinding{},
				}
				if hasStoredConfig {
					config = storedConfig
				}
				config, err = identity.UpsertWorkspaceBinding(config, identity.WorkspaceBinding{
					LocalWorkspaceID: registered.ID,
					CloudWorkspaceID: response.WorkspaceID,
					Path:             registered.Path,
				})
				if err != nil {
					return fmt.Errorf("update machine pairing state: %w", err)
				}
				if err := identity.SaveCloudConfig(stateDir, config); err != nil {
					return fmt.Errorf("save machine pairing state: %w", err)
				}
				_, err := fmt.Fprintf(stdout, "\nWorkspace paired successfully: %s\nScopra Mio will send signed presence heartbeats automatically.\n",
					terminalSafe(registered.Path))
				return err
			default:
				return errors.New("Caloris returned an unknown enrollment state")
			}
		} else if isTerminalEnrollmentFailure(requestErr) {
			return fmt.Errorf("check enrollment status: %w", requestErr)
		}
		if err := waitForNextPoll(ctx, deadline); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return errors.New("pairing timed out; no workspace was paired (run scopra init again to create a new code)")
			}
			return err
		}
	}
}

func resolveInitOrigins(apiValue, dashboardValue string, stored identity.CloudConfig, hasStored bool) (string, string, error) {
	if hasStored {
		if apiValue == "" {
			apiValue = stored.APIURL
		}
		if dashboardValue == "" {
			dashboardValue = stored.DashboardURL
		}
	}
	if apiValue == "" || dashboardValue == "" {
		return "", "", errors.New("set SCOPRA_API_URL and SCOPRA_DASHBOARD_URL, or pass --api-url and --dashboard-url before pairing")
	}
	apiURL, err := normalizeServiceOrigin(apiValue)
	if err != nil {
		return "", "", fmt.Errorf("invalid Caloris API origin: %w", err)
	}
	dashboardURL, err := normalizeServiceOrigin(dashboardValue)
	if err != nil {
		return "", "", fmt.Errorf("invalid dashboard origin: %w", err)
	}
	if hasStored && (apiURL != stored.APIURL || dashboardURL != stored.DashboardURL) {
		return "", "", errors.New("API and dashboard origins differ from this machine's existing pairing; refusing to change the trusted control plane")
	}
	return apiURL, dashboardURL, nil
}

func normalizeServiceOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return "", errors.New("must be an origin without credentials, path, query, or fragment")
	}
	if parsed.Scheme == "https" {
		return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), nil
	}
	if parsed.Scheme != "http" {
		return "", errors.New("must use HTTPS (HTTP is allowed only for loopback development)")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("plain HTTP is allowed only for localhost or a loopback IP")
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), nil
}

func canonicalInitWorkspacePath(path string) (string, error) {
	if path == "" || !utf8.ValidString(path) || pathControl.MatchString(path) {
		return "", errors.New("path must be non-empty valid UTF-8 without control characters")
	}
	if !filepath.IsAbs(path) {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", err
		}
		// Do not clean before EvalSymlinks: ".." is resolved after symlinks,
		// as it is by the kernel and by Mio's workspace validator.
		path = workingDir + string(filepath.Separator) + path
	}
	if len(path) > registry.MaxPathBytes {
		return "", errors.New("path exceeds Mio's workspace path limit")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(canonical) || len(canonical) > registry.MaxPathBytes || pathControl.MatchString(canonical) {
		return "", errors.New("canonical path is invalid or exceeds Mio's workspace path limit")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(canonical), nil
}

func ensureInitWorkspace(ctx context.Context, stateDir, path string) (localipc.Workspace, error) {
	workspaces, err := localipc.ListWorkspaces(ctx, stateDir)
	if err != nil {
		return localipc.Workspace{}, err
	}
	for _, registered := range workspaces {
		if registered.Path == path {
			return registered, nil
		}
	}
	registered, err := localipc.AddWorkspace(ctx, stateDir, path)
	if err == nil {
		return registered, nil
	}
	var reply *localipc.ReplyError
	if !errors.As(err, &reply) || reply.Code != localipc.CodeAlreadyRegistered {
		return localipc.Workspace{}, err
	}
	workspaces, listErr := localipc.ListWorkspaces(ctx, stateDir)
	if listErr != nil {
		return localipc.Workspace{}, errors.Join(err, listErr)
	}
	for _, existing := range workspaces {
		if existing.Path == path {
			return existing, nil
		}
	}
	return localipc.Workspace{}, err
}

func localMachineName() (string, error) {
	name, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("read machine name: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || len(name) > 128 || machineNameControl.MatchString(name) {
		return "", errors.New("machine hostname is empty, invalid, or longer than 128 bytes")
	}
	return name, nil
}

func postEnrollmentJSON(ctx context.Context, client *http.Client, origin, path string, payload any) (int, []byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("encode request: %w", err)
	}
	base, err := url.Parse(origin)
	if err != nil {
		return 0, nil, err
	}
	endpoint := base.ResolveReference(&url.URL{Path: path})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxAPIResponseBytes {
		return response.StatusCode, nil, &enrollmentProtocolError{err: errors.New("Caloris response exceeds the size limit")}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
		if mediaType != "application/json" {
			return response.StatusCode, nil, &enrollmentProtocolError{err: errors.New("Caloris returned a non-JSON success response")}
		}
	}
	return response.StatusCode, body, nil
}

func pollEnrollment(ctx context.Context, client *http.Client, apiURL, enrollmentID string, key identity.KeyPair) (string, enrollmentStatusResponse, error) {
	nonce, err := identity.NewNonce()
	if err != nil {
		return "", enrollmentStatusResponse{}, err
	}
	timestamp := time.Now().Unix()
	request := enrollmentStatusRequest{
		ProtocolVersion: 1,
		EnrollmentID:    enrollmentID,
		Timestamp:       timestamp,
		Nonce:           nonce,
	}
	request.Signature, err = identity.Sign(key, identity.EnrollmentStatusSigningMessage(enrollmentID, timestamp, nonce))
	if err != nil {
		return "", enrollmentStatusResponse{}, err
	}
	statusCode, body, err := postEnrollmentJSON(ctx, client, apiURL, "/v1/enrollments/status", request)
	if err != nil {
		return "", enrollmentStatusResponse{}, err
	}
	if statusCode != http.StatusOK {
		return "", enrollmentStatusResponse{}, &apiStatusError{statusCode: statusCode}
	}
	var response enrollmentStatusResponse
	if err := decodeStrictAPIJSON(body, &response); err != nil {
		return "", enrollmentStatusResponse{}, &enrollmentProtocolError{err: fmt.Errorf("Caloris returned an invalid status response: %w", err)}
	}
	if response.Status != "pending" && response.Status != "accepted" && response.Status != "expired" {
		return "", enrollmentStatusResponse{}, &enrollmentProtocolError{err: errors.New("Caloris returned an unknown enrollment state")}
	}
	if response.Status == "accepted" {
		if !cloudUUIDPattern.MatchString(response.MachineID) || !cloudUUIDPattern.MatchString(response.WorkspaceID) {
			return "", enrollmentStatusResponse{}, &enrollmentProtocolError{err: errors.New("Caloris returned invalid paired identifiers")}
		}
	} else if response.MachineID != "" || response.WorkspaceID != "" {
		return "", enrollmentStatusResponse{}, &enrollmentProtocolError{err: errors.New("Caloris returned pairing identifiers before approval")}
	}
	return response.Status, response, nil
}

func decodeStrictAPIJSON(data []byte, destination any) error {
	if err := rejectDuplicateAPIJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("response contains trailing JSON data")
	}
	return nil
}

func rejectDuplicateAPIJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanAPIJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("response contains trailing JSON data")
	}
	return nil
}

func scanAPIJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("response JSON nesting exceeds the limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("response object key is invalid")
			}
			if _, exists := seen[key]; exists {
				return errors.New("response has duplicate object keys")
			}
			seen[key] = struct{}{}
			if err := scanAPIJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("response object is malformed")
		}
	case '[':
		for decoder.More() {
			if err := scanAPIJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("response array is malformed")
		}
	default:
		return errors.New("response delimiter is invalid")
	}
	return nil
}

func validPairingCode(code string) bool {
	if !pairingCodePattern.MatchString(code) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(code)
	return err == nil && len(decoded) == 16 && base64.RawURLEncoding.EncodeToString(decoded) == code
}

func isTerminalEnrollmentFailure(err error) bool {
	var protocolErr *enrollmentProtocolError
	if errors.As(err, &protocolErr) {
		return true
	}
	var statusErr *apiStatusError
	return errors.As(err, &statusErr) && statusErr.statusCode < 500 && statusErr.statusCode != http.StatusTooManyRequests
}

func waitForNextPoll(ctx context.Context, deadline time.Time) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	interval := pollInterval
	if remaining < interval {
		interval = remaining
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
