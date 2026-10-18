package localipc

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/journal"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// logsRequestPrefix is the part of an execution.logs request that every case
// shares.
const logsRequestPrefix = `{"protocolVersion":1,"requestId":"` + testRequestID + `",`

func testLogEntry(t *testing.T, workspaceID string, state protocol.ExecutionState) journal.Entry {
	t.Helper()
	result := protocol.ExecutionResult{
		ExecutionID: "local-0123456789abcdef0123456789abcdef",
		State:       state,
		StartedAt:   time.Date(2026, 10, 17, 5, 0, 0, 0, time.UTC),
		EndedAt:     time.Date(2026, 10, 17, 5, 0, 1, 500_000_000, time.UTC),
	}
	switch state {
	case protocol.StateFailed:
		result.ExitCode = 1
	case protocol.StateRejected, protocol.StateErrored:
		result.StartedAt, result.EndedAt = time.Time{}, time.Time{}
		result.Error = protocol.ResultError{Code: protocol.ErrorCodeBusy, Message: "busy"}
	}
	entry, err := journal.NewEntry(workspaceID, result, time.Date(2026, 10, 17, 5, 0, 2, 0, time.UTC))
	if err != nil {
		t.Fatalf("journal.NewEntry() error = %v", err)
	}
	return entry
}

func TestEncodeLogsRequestMatchesTheWireForm(t *testing.T) {
	encoded, err := encodeRequest(request{
		ProtocolVersion: protocolVersion,
		RequestID:       testRequestID,
		Command:         logsCommand,
		LogsWorkspaceID: testWorkspaceID,
		LogsLimit:       20,
	})
	if err != nil {
		t.Fatalf("encodeRequest() error = %v", err)
	}
	want := logsRequestPrefix + `"command":"execution.logs","workspaceId":"` + testWorkspaceID + `","limit":20}`
	if string(encoded) != want {
		t.Fatalf("encodeRequest() =\n%s\nwant\n%s", encoded, want)
	}

	encoded, err = encodeRequest(request{
		ProtocolVersion: protocolVersion,
		RequestID:       testRequestID,
		Command:         logsCommand,
		LogsLimit:       5,
	})
	if err != nil {
		t.Fatalf("encodeRequest() without a workspace error = %v", err)
	}
	if string(encoded) != logsRequestPrefix+`"command":"execution.logs","workspaceId":"","limit":5}` {
		t.Fatalf("encodeRequest() without a workspace = %s", encoded)
	}
}

func TestDecodeLogsRequestRejectsInvalidRequests(t *testing.T) {
	tests := map[string]string{
		"missing limit":       logsRequestPrefix + `"command":"execution.logs","workspaceId":"` + testWorkspaceID + `"}`,
		"missing workspace":   logsRequestPrefix + `"command":"execution.logs","limit":20}`,
		"zero limit":          logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":0}`,
		"negative limit":      logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":-1}`,
		"limit too high":      logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":101}`,
		"limit as a string":   logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":"20"}`,
		"limit as null":       logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":null}`,
		"limit fraction":      logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":1.5}`,
		"bad workspace":       logsRequestPrefix + `"command":"execution.logs","workspaceId":"NOT-HEX","limit":20}`,
		"uppercase workspace": logsRequestPrefix + `"command":"execution.logs","workspaceId":"` + strings.ToUpper(testWorkspaceID) + `","limit":20}`,
		"extra key":           logsRequestPrefix + `"command":"execution.logs","workspaceId":"","limit":20,"cursor":""}`,
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			if decoded, err := decodeRequest([]byte(encoded)); err == nil {
				t.Fatalf("decodeRequest(%s) = %+v, want a refusal", encoded, decoded)
			}
		})
	}
}

func TestDecodeLogsRequestAcceptsTheBounds(t *testing.T) {
	for _, limit := range []int{1, journal.DefaultLimit, journal.MaxLimit} {
		message := request{ProtocolVersion: protocolVersion, RequestID: testRequestID, Command: logsCommand,
			LogsLimit: limit}
		if err := validateRequest(message); err != nil {
			t.Fatalf("validateRequest(limit %d) error = %v", limit, err)
		}
	}
}

func TestDecodeLogsReply(t *testing.T) {
	entry := testLogEntry(t, testWorkspaceID, protocol.StateSucceeded)
	encodedEntry, err := entry.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	encoded, err := encodeLogsReply(testRequestID, []journal.Entry{entry})
	if err != nil {
		t.Fatalf("encodeLogsReply() error = %v", err)
	}
	decoded, err := decodeLogsReply(encoded, testRequestID)
	if err != nil {
		t.Fatalf("decodeLogsReply() error = %v", err)
	}
	if len(decoded) != 1 || decoded[0].ExecutionID != entry.ExecutionID {
		t.Fatalf("decodeLogsReply() = %+v, want the entry back", decoded)
	}

	empty, err := encodeLogsReply(testRequestID, nil)
	if err != nil {
		t.Fatalf("encodeLogsReply(nil) error = %v", err)
	}
	if !strings.Contains(string(empty), `"entries":[]`) {
		t.Fatalf("an empty logs reply = %s, want an empty array", empty)
	}
	if decoded, err := decodeLogsReply(empty, testRequestID); err != nil || len(decoded) != 0 {
		t.Fatalf("decodeLogsReply(empty) = %+v, %v; want no entries", decoded, err)
	}

	// An error reply is a typed error, as for the workspace commands.
	errorReply, err := encodeErrorReply(testRequestID, errors.New("the journal is unreadable"))
	if err != nil {
		t.Fatalf("encodeErrorReply() error = %v", err)
	}
	var replyErr *ReplyError
	if _, err := decodeLogsReply(errorReply, testRequestID); !errors.As(err, &replyErr) {
		t.Fatalf("decodeLogsReply(error) = %v, want *ReplyError", err)
	}
	if replyErr.Code != CodeInternal {
		t.Fatalf("error code = %q, want %q", replyErr.Code, CodeInternal)
	}

	bad := map[string]string{
		"another request ID":   strings.Replace(string(encoded), testRequestID, "fedcba9876543210fedcba9876543210", 1),
		"entries not an array": `{"protocolVersion":1,"requestId":"` + testRequestID + `","entries":{}}`,
		"entries null":         `{"protocolVersion":1,"requestId":"` + testRequestID + `","entries":null}`,
		"an invalid entry":     `{"protocolVersion":1,"requestId":"` + testRequestID + `","entries":[{"schemaVersion":9}]}`,
		"duplicate entry keys": `{"protocolVersion":1,"requestId":"` + testRequestID + `","entries":[` +
			strings.Replace(string(encodedEntry), `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1) + `]}`,
		"unknown reply key": `{"protocolVersion":1,"requestId":"` + testRequestID + `","entries":[],"nextCursor":""}`,
	}
	for name, reply := range bad {
		t.Run(name, func(t *testing.T) {
			if decoded, err := decodeLogsReply([]byte(reply), testRequestID); err == nil {
				t.Fatalf("decodeLogsReply(%s) = %+v, want a refusal", reply, decoded)
			}
		})
	}
}

func TestLogsClientValidatesBeforeDialing(t *testing.T) {
	tests := map[string]LogFilter{
		"zero limit":     {},
		"negative limit": {Limit: -1},
		"limit too high": {Limit: journal.MaxLimit + 1},
		"bad workspace":  {WorkspaceID: "not-hex", Limit: 1},
		"uppercase":      {WorkspaceID: strings.ToUpper(testWorkspaceID), Limit: 1},
	}
	for name, filter := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Logs(t.Context(), t.TempDir(), filter); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("Logs(%+v) error = %v, want ErrInvalidMessage", filter, err)
			}
		})
	}
}
