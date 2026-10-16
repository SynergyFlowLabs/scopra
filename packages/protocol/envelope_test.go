package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDecodeEnvelope_AcceptsValidV1Envelope(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	want := validTestEnvelope(t, now)
	encoded := marshalTestEnvelope(t, want)

	got, err := DecodeEnvelope(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeEnvelope() error = %v", err)
	}
	if err := got.ValidateAt(now); err != nil {
		t.Fatalf("ValidateAt() error = %v", err)
	}
	if got.MessageID != want.MessageID {
		t.Errorf("MessageID = %q, want %q", got.MessageID, want.MessageID)
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Errorf("Payload = %s, want %s", got.Payload, want.Payload)
	}
}

func TestDecodeEnvelope_RejectsUnknownFields(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	encoded := marshalTestEnvelope(t, validTestEnvelope(t, now))
	encoded = appendEnvelopeField(encoded, `"unrecognized":true`)

	if _, err := DecodeEnvelope(bytes.NewReader(encoded)); err == nil {
		t.Fatal("DecodeEnvelope() accepted an unknown envelope field")
	}
}

func TestDecodeEnvelope_RejectsTrailingJSONValue(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	encoded := marshalTestEnvelope(t, validTestEnvelope(t, now))
	encoded = append(encoded, []byte(` {"second":true}`)...)

	if _, err := DecodeEnvelope(bytes.NewReader(encoded)); err == nil {
		t.Fatal("DecodeEnvelope() accepted a second JSON value")
	}
}

func TestDecodeEnvelope_RejectsOversizedInput(t *testing.T) {
	encoded := bytes.Repeat([]byte(" "), MaxEnvelopeBytes+1)

	_, err := DecodeEnvelope(bytes.NewReader(encoded))
	if !errors.Is(err, ErrEnvelopeTooLarge) {
		t.Fatalf("DecodeEnvelope() error = %v, want ErrEnvelopeTooLarge", err)
	}
}

func TestSecurity_RejectsDuplicateJSONKeysAtAnyDepth(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.Payload = json.RawMessage(`{"request":{"workspace":"one","workspace":"two"}}`)
	envelope.PayloadHash = hashTestPayload(envelope.Payload)
	encoded := marshalTestEnvelope(t, envelope)

	_, err := DecodeEnvelope(bytes.NewReader(encoded))
	if !errors.Is(err, ErrDuplicateJSONKey) {
		t.Fatalf("DecodeEnvelope() error = %v, want ErrDuplicateJSONKey", err)
	}
}

func TestSecurity_RejectsTamperedPayload(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.Payload = json.RawMessage(`{"executionId":"changed"}`)

	if err := envelope.ValidateAt(now); !errors.Is(err, ErrPayloadHashMismatch) {
		t.Fatalf("ValidateAt() error = %v, want ErrPayloadHashMismatch", err)
	}
}

func TestSecurity_RejectsExpiredEnvelope(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.ExpiresAt = now

	if err := envelope.ValidateAt(now); !errors.Is(err, ErrEnvelopeExpired) {
		t.Fatalf("ValidateAt() error = %v, want ErrEnvelopeExpired", err)
	}
}

func TestSecurity_RejectsUnknownMessageType(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.Type = MessageType("execution.unknown")

	if err := envelope.ValidateAt(now); !errors.Is(err, ErrUnsupportedMessageType) {
		t.Fatalf("ValidateAt() error = %v, want ErrUnsupportedMessageType", err)
	}
}

func TestSecurity_RejectsPayloadOverLimit(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.Payload = json.RawMessage(fmt.Sprintf(`{"pad":%q}`, strings.Repeat("x", MaxPayloadBytes)))
	envelope.PayloadHash = hashTestPayload(envelope.Payload)

	_, err := DecodeEnvelope(bytes.NewReader(marshalTestEnvelope(t, envelope)))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("DecodeEnvelope() error = %v, want ErrPayloadTooLarge", err)
	}
}

func FuzzDecodeEnvelope(f *testing.F) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	f.Add(string(marshalTestEnvelope(f, validTestEnvelope(f, now))))
	f.Add(`{"version":`)
	f.Add(`[]`)

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = DecodeEnvelope(strings.NewReader(input))
	})
}

func validTestEnvelope(t testing.TB, now time.Time) Envelope {
	t.Helper()
	payload := json.RawMessage(`{"executionId":"exec_001"}`)
	return Envelope{
		Version:             ProtocolVersionV1,
		Type:                MessageTypeExecutionRequest,
		MessageID:           "msg_001",
		DeviceAudience:      "machine_001",
		Issuer:              "caloris",
		Sequence:            1,
		CreatedAt:           now.Add(-time.Second),
		ExpiresAt:           now.Add(time.Minute),
		Nonce:               "AAECAwQFBgcICQoLDA0ODw",
		PayloadHash:         hashTestPayload(payload),
		AuthenticationProof: "structural-test-only",
		Payload:             payload,
	}
}

func marshalTestEnvelope(t testing.TB, envelope Envelope) []byte {
	t.Helper()
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}

func appendEnvelopeField(encoded []byte, field string) []byte {
	return append([]byte(strings.TrimSuffix(string(encoded), "}")), []byte(","+field+"}")...)
}

func hashTestPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
