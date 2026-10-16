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

func TestDecodeEnvelope_RejectsInvalidWireFields(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*Envelope)
		want   error
	}{
		{
			name: "unsupported version",
			mutate: func(envelope *Envelope) {
				envelope.Version = "v2"
			},
			want: ErrUnsupportedVersion,
		},
		{
			name: "invalid identifier",
			mutate: func(envelope *Envelope) {
				envelope.Issuer = "caloris/1"
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "zero sequence",
			mutate: func(envelope *Envelope) {
				envelope.Sequence = 0
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "zero creation time",
			mutate: func(envelope *Envelope) {
				envelope.CreatedAt = time.Time{}
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "expiration before creation",
			mutate: func(envelope *Envelope) {
				envelope.ExpiresAt = envelope.CreatedAt.Add(-time.Second)
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "lifetime exceeds limit",
			mutate: func(envelope *Envelope) {
				envelope.CreatedAt = now.Add(-10 * time.Minute)
				envelope.ExpiresAt = now.Add(time.Minute)
			},
			want: ErrEnvelopeLifetimeTooLong,
		},
		{
			name: "short nonce",
			mutate: func(envelope *Envelope) {
				envelope.Nonce = "AQID"
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "padded nonce is not canonical",
			mutate: func(envelope *Envelope) {
				envelope.Nonce = "AAECAwQFBgcICQoLDA0ODw=="
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "missing proof",
			mutate: func(envelope *Envelope) {
				envelope.AuthenticationProof = ""
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "oversized proof",
			mutate: func(envelope *Envelope) {
				envelope.AuthenticationProof = strings.Repeat("p", 8<<10+1)
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "missing payload",
			mutate: func(envelope *Envelope) {
				envelope.Payload = nil
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "payload must be an object",
			mutate: func(envelope *Envelope) {
				envelope.Payload = json.RawMessage(`[]`)
				envelope.PayloadHash = hashTestPayload(envelope.Payload)
			},
			want: ErrInvalidEnvelope,
		},
		{
			name: "payload digest mismatch",
			mutate: func(envelope *Envelope) {
				envelope.PayloadHash = strings.Repeat("0", sha256.Size*2)
			},
			want: ErrPayloadHashMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := validTestEnvelope(t, now)
			test.mutate(&envelope)

			_, err := DecodeEnvelope(bytes.NewReader(marshalTestEnvelope(t, envelope)))
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeEnvelope() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnvelope_ValidateAtRejectsInvalidClock(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		now    time.Time
		mutate func(*Envelope)
		want   error
	}{
		{
			name: "zero validation time",
			now:  time.Time{},
			want: ErrInvalidEnvelope,
		},
		{
			name: "issued beyond allowed clock skew",
			now:  now,
			mutate: func(envelope *Envelope) {
				envelope.CreatedAt = now.Add(AllowedClockSkew + time.Second)
				envelope.ExpiresAt = envelope.CreatedAt.Add(time.Minute)
			},
			want: ErrEnvelopeNotYetValid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := validTestEnvelope(t, now)
			if test.mutate != nil {
				test.mutate(&envelope)
			}
			if err := envelope.ValidateAt(test.now); !errors.Is(err, test.want) {
				t.Fatalf("ValidateAt() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeEnvelope_RejectsMalformedPayloadAndNilReader(t *testing.T) {
	if _, err := DecodeEnvelope(nil); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("DecodeEnvelope(nil) error = %v, want ErrInvalidEnvelope", err)
	}

	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	envelope.Payload = json.RawMessage(`{`)
	if err := envelope.ValidateStructure(); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("ValidateStructure() error = %v, want ErrInvalidEnvelope", err)
	}
}

func TestSecurity_RejectsOverlyDeepPayload(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	envelope := validTestEnvelope(t, now)
	deepValue := strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1)
	envelope.Payload = json.RawMessage(`{"deep":` + deepValue + `}`)
	envelope.PayloadHash = hashTestPayload(envelope.Payload)

	_, err := DecodeEnvelope(bytes.NewReader(marshalTestEnvelope(t, envelope)))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("DecodeEnvelope() error = %v, want ErrInvalidEnvelope", err)
	}
}
