package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSecurity_EnvelopeAcceptsExactlyTheVersion1MessageTypes(t *testing.T) {
	if MessageTypeExecutionStarted != "execution.started" || MessageTypeExecutionOutput != "execution.output" {
		t.Fatalf("message types = %q and %q, want execution.started and execution.output (ADR-009)",
			MessageTypeExecutionStarted, MessageTypeExecutionOutput)
	}
	for _, messageType := range []MessageType{
		"execution.request", "execution.result", "execution.started", "execution.output",
		"execution.cancel", "machine.revocation", "message.ack",
	} {
		if !supportedMessageType(messageType) {
			t.Errorf("supportedMessageType(%q) = false, want true", messageType)
		}
	}
	for _, messageType := range []MessageType{"execution.progress", "execution.status", "Execution.started", "execution.output.v2", ""} {
		if supportedMessageType(messageType) {
			t.Errorf("supportedMessageType(%q) = true, want false", messageType)
		}
	}
}

func TestDecodeEnvelope_CarriesTheDaemonToCloudMessages(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		messageType MessageType
		payload     string
		decode      func(Envelope) (any, error)
		want        any
	}{
		{MessageTypeExecutionStarted, startedExample, func(e Envelope) (any, error) { return e.ExecutionStarted() }, startedExampleDecoded()},
		{MessageTypeExecutionOutput, outputExample, func(e Envelope) (any, error) { return e.ExecutionOutput() }, outputExampleDecoded()},
		{MessageTypeExecutionResult, resultExamples[StateFailed], func(e Envelope) (any, error) { return e.ExecutionResult() }, resultExampleDecoded(StateFailed)},
	}
	for _, test := range tests {
		t.Run(string(test.messageType), func(t *testing.T) {
			var payload bytes.Buffer
			if err := json.Compact(&payload, []byte(test.payload)); err != nil {
				t.Fatalf("json.Compact() error = %v", err)
			}
			envelope := validTestEnvelope(t, now)
			envelope.Type = test.messageType
			envelope.Payload = payload.Bytes()
			envelope.PayloadHash = hashTestPayload(envelope.Payload)
			decoded, err := DecodeEnvelope(bytes.NewReader(marshalTestEnvelope(t, envelope)))
			if err != nil {
				t.Fatalf("DecodeEnvelope() error = %v", err)
			}
			value, err := test.decode(decoded)
			if err != nil || !reflect.DeepEqual(value, test.want) {
				t.Fatalf("decoded payload = %+v, %v; want %+v", value, err, test.want)
			}
		})
	}
}

func TestEnvelope_PayloadHelpersDecodeOnlyTheirOwnType(t *testing.T) {
	helpers := map[MessageType]func(Envelope) error{
		MessageTypeExecutionRequest: func(e Envelope) error { _, err := e.ExecutionRequest(); return err },
		MessageTypeExecutionResult:  func(e Envelope) error { _, err := e.ExecutionResult(); return err },
		MessageTypeExecutionStarted: func(e Envelope) error { _, err := e.ExecutionStarted(); return err },
		MessageTypeExecutionOutput:  func(e Envelope) error { _, err := e.ExecutionOutput(); return err },
	}
	payloadErrors := []error{ErrInvalidExecutionRequest, ErrInvalidExecutionResult, ErrInvalidExecutionStarted, ErrInvalidExecutionOutput}
	messageTypes := []MessageType{
		MessageTypeExecutionRequest, MessageTypeExecutionResult, MessageTypeExecutionStarted, MessageTypeExecutionOutput,
		MessageTypeExecutionCancel, MessageTypeMachineRevocation, MessageTypeAcknowledgement, "execution.progress",
	}
	for own, helper := range helpers {
		for _, messageType := range messageTypes {
			err := helper(Envelope{Type: messageType, Payload: json.RawMessage(`{}`)})
			if messageType == own {
				// An empty object is invalid, so the helper must have tried to decode it.
				if errors.Is(err, ErrUnexpectedMessageType) || err == nil {
					t.Errorf("%s helper with its own type: error = %v, want a payload error", own, err)
				}
				continue
			}
			if !errors.Is(err, ErrUnexpectedMessageType) {
				t.Errorf("%s helper with type %q: error = %v, want ErrUnexpectedMessageType", own, messageType, err)
			}
			for _, payloadErr := range payloadErrors {
				if errors.Is(err, payloadErr) {
					t.Errorf("%s helper with type %q decoded the payload: %v", own, messageType, err)
				}
			}
		}
	}
}
