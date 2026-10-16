// Package protocol defines the wire contract for Scopra Messenger.
//
// Structural validation is not cryptographic verification, authorization, or
// replay protection. Callers must perform those checks before dispatching an
// envelope or acting on its payload.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// ProtocolVersionV1 is the initial Scopra Messenger wire version.
	ProtocolVersionV1 = "v1"

	// MaxEnvelopeBytes bounds a complete encoded envelope.
	MaxEnvelopeBytes = 1 << 20
	// MaxPayloadBytes bounds the raw JSON payload carried by an envelope.
	MaxPayloadBytes = 512 << 10

	maxAuthenticationProofBytes = 8 << 10
	maxIdentifierBytes          = 128
	minNonceBytes               = 16
	maxNonceBytes               = 128
	maxJSONDepth                = 64

	// MaxMessageLifetime bounds how long a signed message may remain usable.
	MaxMessageLifetime = 5 * time.Minute
	// AllowedClockSkew permits a small difference between issuer and receiver clocks.
	AllowedClockSkew = 30 * time.Second
)

// MessageType identifies a versioned Messenger operation. Unknown values fail closed.
type MessageType string

const (
	MessageTypeExecutionRequest  MessageType = "execution.request"
	MessageTypeExecutionResult   MessageType = "execution.result"
	MessageTypeExecutionCancel   MessageType = "execution.cancel"
	MessageTypeMachineRevocation MessageType = "machine.revocation"
	MessageTypeAcknowledgement   MessageType = "message.ack"
)

var (
	ErrInvalidEnvelope         = errors.New("protocol: invalid envelope")
	ErrEnvelopeTooLarge        = errors.New("protocol: envelope exceeds size limit")
	ErrPayloadTooLarge         = errors.New("protocol: payload exceeds size limit")
	ErrPayloadHashMismatch     = errors.New("protocol: payload hash mismatch")
	ErrDuplicateJSONKey        = errors.New("protocol: duplicate JSON object key")
	ErrUnsupportedVersion      = errors.New("protocol: unsupported version")
	ErrUnsupportedMessageType  = errors.New("protocol: unsupported message type")
	ErrEnvelopeNotYetValid     = errors.New("protocol: envelope creation time is in the future")
	ErrEnvelopeExpired         = errors.New("protocol: envelope has expired")
	ErrEnvelopeLifetimeTooLong = errors.New("protocol: envelope lifetime exceeds limit")
)

// Envelope is an untrusted cloud-to-daemon or daemon-to-cloud message.
// AuthenticationProof is opaque here; this package does not verify it.
type Envelope struct {
	Version             string          `json:"version"`
	Type                MessageType     `json:"type"`
	MessageID           string          `json:"messageId"`
	DeviceAudience      string          `json:"deviceAudience"`
	Issuer              string          `json:"issuer"`
	Sequence            uint64          `json:"sequence"`
	CreatedAt           time.Time       `json:"createdAt"`
	ExpiresAt           time.Time       `json:"expiresAt"`
	Nonce               string          `json:"nonce"`
	PayloadHash         string          `json:"payloadHash"`
	AuthenticationProof string          `json:"authenticationProof"`
	Payload             json.RawMessage `json:"payload"`
}

// DecodeEnvelope reads one bounded JSON envelope, rejects unknown fields and
// duplicate object keys, and validates its non-time-dependent structure. It
// does not authenticate the envelope or authorize its operation.
func DecodeEnvelope(r io.Reader) (Envelope, error) {
	if r == nil {
		return Envelope{}, fmt.Errorf("%w: nil reader", ErrInvalidEnvelope)
	}

	encoded, err := io.ReadAll(io.LimitReader(r, int64(MaxEnvelopeBytes)+1))
	if err != nil {
		return Envelope{}, fmt.Errorf("read envelope: %w", err)
	}
	if len(encoded) > MaxEnvelopeBytes {
		return Envelope{}, ErrEnvelopeTooLarge
	}
	if !json.Valid(encoded) {
		return Envelope{}, fmt.Errorf("%w: malformed JSON", ErrInvalidEnvelope)
	}
	if err := rejectDuplicateJSONKeys(encoded); err != nil {
		return Envelope{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()

	var envelope Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if err := envelope.ValidateStructure(); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

// ValidateStructure checks the versioned wire shape and payload digest. It
// does not check freshness, cryptographic authenticity, authorization, or replay state.
func (e Envelope) ValidateStructure() error {
	if e.Version != ProtocolVersionV1 {
		return ErrUnsupportedVersion
	}
	if !supportedMessageType(e.Type) {
		return ErrUnsupportedMessageType
	}
	if !validIdentifier(e.MessageID) || !validIdentifier(e.DeviceAudience) || !validIdentifier(e.Issuer) {
		return fmt.Errorf("%w: invalid message, issuer, or audience identifier", ErrInvalidEnvelope)
	}
	if e.Sequence == 0 {
		return fmt.Errorf("%w: sequence must be positive", ErrInvalidEnvelope)
	}
	if e.CreatedAt.IsZero() || e.ExpiresAt.IsZero() || !e.ExpiresAt.After(e.CreatedAt) {
		return fmt.Errorf("%w: invalid message timestamps", ErrInvalidEnvelope)
	}
	if e.ExpiresAt.Sub(e.CreatedAt) > MaxMessageLifetime {
		return ErrEnvelopeLifetimeTooLong
	}
	if !validNonce(e.Nonce) {
		return fmt.Errorf("%w: nonce must contain at least 128 bits of canonical base64url data", ErrInvalidEnvelope)
	}
	if len(e.AuthenticationProof) == 0 || len(e.AuthenticationProof) > maxAuthenticationProofBytes {
		return fmt.Errorf("%w: invalid authentication proof size", ErrInvalidEnvelope)
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("%w: missing payload", ErrInvalidEnvelope)
	}
	if len(e.Payload) > MaxPayloadBytes {
		return ErrPayloadTooLarge
	}
	if !json.Valid(e.Payload) || firstNonSpace(e.Payload) != '{' {
		return fmt.Errorf("%w: payload must be a JSON object", ErrInvalidEnvelope)
	}
	if err := rejectDuplicateJSONKeys(e.Payload); err != nil {
		return err
	}

	digest := sha256.Sum256(e.Payload)
	if e.PayloadHash != hex.EncodeToString(digest[:]) {
		return ErrPayloadHashMismatch
	}
	return nil
}

// ValidateAt validates the structure and verifies the envelope's freshness
// against now. It still does not verify AuthenticationProof or authorize the request.
func (e Envelope) ValidateAt(now time.Time) error {
	if err := e.ValidateStructure(); err != nil {
		return err
	}
	if now.IsZero() {
		return fmt.Errorf("%w: missing validation time", ErrInvalidEnvelope)
	}
	if e.CreatedAt.After(now.Add(AllowedClockSkew)) {
		return ErrEnvelopeNotYetValid
	}
	if !now.Before(e.ExpiresAt) {
		return ErrEnvelopeExpired
	}
	return nil
}

func supportedMessageType(messageType MessageType) bool {
	switch messageType {
	case MessageTypeExecutionRequest,
		MessageTypeExecutionResult,
		MessageTypeExecutionCancel,
		MessageTypeMachineRevocation,
		MessageTypeAcknowledgement:
		return true
	default:
		return false
	}
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > maxIdentifierBytes || strings.TrimSpace(value) != value {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' || char == ':' {
			continue
		}
		return false
	}
	return true
}

func validNonce(value string) bool {
	if value == "" {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) < minNonceBytes || len(decoded) > maxNonceBytes {
		return false
	}
	return base64.RawURLEncoding.EncodeToString(decoded) == value
}

func firstNonSpace(value []byte) byte {
	for _, char := range value {
		if char != ' ' && char != '\n' && char != '\r' && char != '\t' {
			return char
		}
	}
	return 0
}

func rejectDuplicateJSONKeys(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, 0); err != nil {
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("%w: JSON nesting exceeds limit", ErrInvalidEnvelope)
	}

	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: malformed JSON value", ErrInvalidEnvelope)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("%w: malformed JSON object key", ErrInvalidEnvelope)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("%w: malformed JSON object key", ErrInvalidEnvelope)
			}
			if _, exists := keys[key]; exists {
				return ErrDuplicateJSONKey
			}
			keys[key] = struct{}{}
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%w: malformed JSON object", ErrInvalidEnvelope)
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%w: malformed JSON array", ErrInvalidEnvelope)
		}
	default:
		return fmt.Errorf("%w: unexpected JSON delimiter", ErrInvalidEnvelope)
	}
	return nil
}
