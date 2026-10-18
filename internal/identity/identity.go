// Package identity stores Scopra's per-machine Ed25519 identity in the private
// Mio state directory and signs the versioned enrollment/presence requests.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	keyFileName          = "machine.ed25519"
	cloudConfigFileName  = "cloud.json"
	maxCloudConfigBytes  = 64 * 1024
	maxCloudWorkspaces   = 256
	maxWorkspacePathSize = 4095
)

var (
	ErrInsecureStateDir  = errors.New("identity: insecure state directory")
	ErrInsecureKeyFile   = errors.New("identity: insecure machine key file")
	ErrInvalidCloud      = errors.New("identity: invalid cloud configuration")
	ErrWorkspaceConflict = errors.New("identity: workspace pairing conflicts with existing machine state")

	workspaceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	uuidV4Pattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	controlCharacters  = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f\x{2028}\x{2029}]`)
)

// KeyPair holds the local machine key in memory. Private must never be logged,
// serialized into a cloud request, or returned to the dashboard.
type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// WorkspaceBinding is the cloud-side identifier associated with a local
// workspace registration. It is metadata only and does not grant execution.
type WorkspaceBinding struct {
	LocalWorkspaceID string `json:"localWorkspaceId"`
	CloudWorkspaceID string `json:"workspaceId"`
	Path             string `json:"path"`
}

// CloudConfig is owner-only local metadata used by Mio to maintain presence.
// It deliberately contains no bearer token; device requests are signed by the
// separately stored machine key.
type CloudConfig struct {
	SchemaVersion int                `json:"schemaVersion"`
	APIURL        string             `json:"apiUrl"`
	DashboardURL  string             `json:"dashboardUrl"`
	MachineID     string             `json:"machineId"`
	Workspaces    []WorkspaceBinding `json:"workspaces"`
}

// LoadKey loads the stable machine key without creating or replacing it.
// It is used by Mio so missing pairing state cannot silently become a new
// machine identity.
func LoadKey(stateDir string) (KeyPair, error) {
	stateDir, err := validateStateDir(stateDir)
	if err != nil {
		return KeyPair{}, err
	}
	private, err := readKey(filepath.Join(stateDir, keyFileName))
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Public: append(ed25519.PublicKey(nil), private[ed25519.SeedSize:]...), Private: private}, nil
}

// LoadOrCreateKey loads the stable machine key or creates it with mode 0600.
// The caller must supply an already-created owner-only Scopra state directory.
func LoadOrCreateKey(stateDir string) (KeyPair, error) {
	stateDir, err := validateStateDir(stateDir)
	if err != nil {
		return KeyPair{}, err
	}
	path := filepath.Join(stateDir, keyFileName)
	private, err := readKey(path)
	if err == nil {
		return KeyPair{Public: append(ed25519.PublicKey(nil), private[ed25519.SeedSize:]...), Private: private}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return KeyPair{}, err
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate machine key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		private, readErr := readKey(path)
		if readErr != nil {
			return KeyPair{}, readErr
		}
		return KeyPair{Public: append(ed25519.PublicKey(nil), private[ed25519.SeedSize:]...), Private: private}, nil
	}
	if err != nil {
		return KeyPair{}, fmt.Errorf("create machine key: %w", err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return KeyPair{}, fmt.Errorf("secure machine key permissions: %w", err)
	}
	written, err := file.Write(private)
	if err != nil {
		return KeyPair{}, fmt.Errorf("write machine key: %w", err)
	}
	if written != len(private) {
		return KeyPair{}, fmt.Errorf("write machine key: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return KeyPair{}, fmt.Errorf("sync machine key: %w", err)
	}
	if err := file.Close(); err != nil {
		return KeyPair{}, fmt.Errorf("close machine key: %w", err)
	}
	if err := syncDirectory(stateDir); err != nil {
		return KeyPair{}, fmt.Errorf("sync machine state directory: %w", err)
	}
	ok = true
	return KeyPair{Public: public, Private: private}, nil
}

func readKey(path string) (ed25519.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !secureKeyFile(info) {
		return nil, ErrInsecureKeyFile
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open machine key: %w", err)
	}
	defer file.Close()
	private, err := io.ReadAll(io.LimitReader(file, ed25519.PrivateKeySize+1))
	if err != nil {
		return nil, fmt.Errorf("read machine key: %w", err)
	}
	if len(private) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: machine key has an invalid length", ErrInsecureKeyFile)
	}
	key := ed25519.PrivateKey(private)
	derived := key.Public().(ed25519.PublicKey)
	if !equalBytes(derived, key[ed25519.SeedSize:]) {
		return nil, fmt.Errorf("%w: public key does not match private seed", ErrInsecureKeyFile)
	}
	return key, nil
}

func validateStateDir(stateDir string) (string, error) {
	if stateDir == "" {
		return "", fmt.Errorf("%w: state directory is required", ErrInsecureStateDir)
	}
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("resolve state directory: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect state directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !ownedByCurrentUser(info) {
		return "", ErrInsecureStateDir
	}
	return absolute, nil
}

// SaveCloudConfig writes owner-only, token-free cloud metadata atomically.
func SaveCloudConfig(stateDir string, config CloudConfig) error {
	stateDir, err := validateStateDir(stateDir)
	if err != nil {
		return err
	}
	if err := validateCloudConfig(config); err != nil {
		return err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode cloud configuration: %w", err)
	}
	if len(encoded) > maxCloudConfigBytes {
		return fmt.Errorf("%w: configuration exceeds size limit", ErrInvalidCloud)
	}
	path := filepath.Join(stateDir, cloudConfigFileName)
	if info, err := os.Lstat(path); err == nil {
		if !secureConfigFile(info) {
			return fmt.Errorf("%w: unsafe cloud configuration file", ErrInvalidCloud)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect cloud configuration: %w", err)
	}
	temp, err := os.OpenFile(filepath.Join(stateDir, ".cloud.json.tmp-"+randomFileSuffix()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary cloud configuration: %w", err)
	}
	tempPath := temp.Name()
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure cloud configuration permissions: %w", err)
	}
	written, err := temp.Write(encoded)
	if err != nil {
		return fmt.Errorf("write cloud configuration: %w", err)
	}
	if written != len(encoded) {
		return fmt.Errorf("write cloud configuration: %w", io.ErrShortWrite)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync cloud configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close cloud configuration: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace cloud configuration: %w", err)
	}
	if err := syncDirectory(stateDir); err != nil {
		return fmt.Errorf("sync cloud configuration directory: %w", err)
	}
	ok = true
	return nil
}

// LoadCloudConfig reads and strictly validates cloud metadata. Missing
// configuration is reported as os.ErrNotExist so Mio can remain local-only.
func LoadCloudConfig(stateDir string) (CloudConfig, error) {
	stateDir, err := validateStateDir(stateDir)
	if err != nil {
		return CloudConfig{}, err
	}
	path := filepath.Join(stateDir, cloudConfigFileName)
	info, err := os.Lstat(path)
	if err != nil {
		return CloudConfig{}, err
	}
	if !secureConfigFile(info) || info.Size() > maxCloudConfigBytes {
		return CloudConfig{}, fmt.Errorf("%w: unsafe cloud configuration file", ErrInvalidCloud)
	}
	file, err := os.Open(path)
	if err != nil {
		return CloudConfig{}, fmt.Errorf("open cloud configuration: %w", err)
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, maxCloudConfigBytes+1))
	if err != nil {
		return CloudConfig{}, fmt.Errorf("read cloud configuration: %w", err)
	}
	if len(encoded) > maxCloudConfigBytes {
		return CloudConfig{}, fmt.Errorf("%w: configuration exceeds size limit", ErrInvalidCloud)
	}
	if err := rejectDuplicateJSONKeys(encoded); err != nil {
		return CloudConfig{}, fmt.Errorf("%w: malformed or duplicate cloud configuration fields", ErrInvalidCloud)
	}
	var config CloudConfig
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return CloudConfig{}, fmt.Errorf("%w: malformed cloud configuration", ErrInvalidCloud)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return CloudConfig{}, fmt.Errorf("%w: trailing cloud configuration data", ErrInvalidCloud)
	}
	if err := validateCloudConfig(config); err != nil {
		return CloudConfig{}, err
	}
	return config, nil
}

// UpsertWorkspaceBinding returns a config with the binding added, or unchanged
// when this local workspace is already paired identically.
func UpsertWorkspaceBinding(config CloudConfig, binding WorkspaceBinding) (CloudConfig, error) {
	if err := validateWorkspaceBinding(binding); err != nil {
		return CloudConfig{}, err
	}
	for _, current := range config.Workspaces {
		if current.LocalWorkspaceID != binding.LocalWorkspaceID {
			continue
		}
		if current == binding {
			return config, nil
		}
		return CloudConfig{}, ErrWorkspaceConflict
	}
	if len(config.Workspaces) >= maxCloudWorkspaces {
		return CloudConfig{}, fmt.Errorf("%w: too many workspaces", ErrInvalidCloud)
	}
	config.Workspaces = append(config.Workspaces, binding)
	return config, nil
}

// BeginEnrollmentSigningMessage is the exact newline-delimited v1 request
// signed by the Go CLI and verified by Caloris.
func BeginEnrollmentSigningMessage(requestID, machineName, workspaceID, publicKey, workspacePath string) []byte {
	return []byte(strings.Join([]string{
		"scopra/device-enrollment-begin/v1",
		requestID,
		machineName,
		workspaceID,
		publicKey,
		workspacePath,
	}, "\n"))
}

func EnrollmentStatusSigningMessage(enrollmentID string, timestamp int64, nonce string) []byte {
	return []byte(strings.Join([]string{
		"scopra/device-enrollment-status/v1",
		enrollmentID,
		fmt.Sprintf("%d", timestamp),
		nonce,
	}, "\n"))
}

func PresenceSigningMessage(machineID string, timestamp int64, nonce string) []byte {
	return []byte(strings.Join([]string{
		"scopra/device-presence/v1",
		machineID,
		fmt.Sprintf("%d", timestamp),
		nonce,
	}, "\n"))
}

func Sign(key KeyPair, message []byte) (string, error) {
	if len(key.Private) != ed25519.PrivateKeySize || len(key.Public) != ed25519.PublicKeySize || !equalBytes(key.Private[ed25519.SeedSize:], key.Public) {
		return "", fmt.Errorf("%w: invalid machine key pair", ErrInsecureKeyFile)
	}
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(key.Private, message)), nil
}

func NewRequestID() (string, error) { return randomToken(16) }
func NewNonce() (string, error)     { return randomToken(16) }

func EncodePublicKey(key KeyPair) (string, error) {
	if len(key.Public) != ed25519.PublicKeySize {
		return "", fmt.Errorf("%w: invalid public key", ErrInsecureKeyFile)
	}
	return base64.RawURLEncoding.EncodeToString(key.Public), nil
}

// Fingerprint returns the first 96 bits of SHA-256(public key) in lowercase
// hexadecimal for the human confirmation screen.
func Fingerprint(key KeyPair) (string, error) {
	if len(key.Public) != ed25519.PublicKeySize {
		return "", fmt.Errorf("%w: invalid public key", ErrInsecureKeyFile)
	}
	digest := sha256.Sum256(key.Public)
	return hex.EncodeToString(digest[:12]), nil
}

func validateCloudConfig(config CloudConfig) error {
	if config.SchemaVersion != 1 || !validOrigin(config.APIURL) || !validOrigin(config.DashboardURL) || !uuidV4Pattern.MatchString(config.MachineID) ||
		len(config.Workspaces) > maxCloudWorkspaces {
		return fmt.Errorf("%w: invalid schema or machine endpoints", ErrInvalidCloud)
	}
	seen := make(map[string]struct{}, len(config.Workspaces))
	for _, binding := range config.Workspaces {
		if err := validateWorkspaceBinding(binding); err != nil {
			return err
		}
		if _, exists := seen[binding.LocalWorkspaceID]; exists {
			return fmt.Errorf("%w: duplicate local workspace ID", ErrInvalidCloud)
		}
		seen[binding.LocalWorkspaceID] = struct{}{}
	}
	return nil
}

func validateWorkspaceBinding(binding WorkspaceBinding) error {
	if !workspaceIDPattern.MatchString(binding.LocalWorkspaceID) || !uuidV4Pattern.MatchString(binding.CloudWorkspaceID) ||
		!filepath.IsAbs(binding.Path) || !utf8.ValidString(binding.Path) || len(binding.Path) > maxWorkspacePathSize || controlCharacters.MatchString(binding.Path) {
		return fmt.Errorf("%w: invalid workspace binding", ErrInvalidCloud)
	}
	return nil
}

func validOrigin(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random identifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func randomFileSuffix() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return hex.EncodeToString(value)
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for index := range left {
		result |= left[index] ^ right[index]
	}
	return result == 0
}
