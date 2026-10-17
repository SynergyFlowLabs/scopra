package localipc

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// validRunJSON is testRunRequest on the wire.
const validRunJSON = `{"workspaceId":"` + testWorkspaceID + `",` +
	`"command":{"executable":"go","arguments":["test","./..."],"shell":false,"workingDirectory":"."},` +
	`"workspaceAccess":"read-only",` +
	`"limits":{"timeoutSeconds":600,"cpuMillicores":2000,"memoryBytes":2147483648,"pids":512,"outputBytes":16777216},` +
	`"reason":"local run by the workspace owner"}`

// testRunRequest returns a valid run with ADR-011's scopra run defaults.
func testRunRequest() RunRequest {
	return RunRequest{
		WorkspaceID:     testWorkspaceID,
		Command:         protocol.Command{Executable: "go", Arguments: []string{"test", "./..."}, WorkingDirectory: "."},
		WorkspaceAccess: protocol.WorkspaceReadOnly,
		Limits: protocol.Limits{
			TimeoutSeconds: 600,
			CPUMillicores:  2000,
			MemoryBytes:    2 << 30,
			PIDs:           512,
			OutputBytes:    16 << 20,
		},
		Reason: "local run by the workspace owner",
	}
}

func TestEncodeRun_MatchesTheWireForm(t *testing.T) {
	encoded, err := encodeRun(testRunRequest())
	if err != nil {
		t.Fatalf("encodeRun() error = %v", err)
	}
	if string(encoded) != validRunJSON {
		t.Fatalf("encodeRun() =\n%s\nwant\n%s", encoded, validRunJSON)
	}

	run := testRunRequest()
	run.Command.Arguments = nil
	encoded, err = encodeRun(run)
	if err != nil || !strings.Contains(string(encoded), `"arguments":[]`) {
		t.Fatalf("encodeRun() without arguments = %s, %v; want an empty arguments array", encoded, err)
	}
}

func TestSecurity_EncodeRunRejectsInvalidRunsBeforeSending(t *testing.T) {
	tests := map[string]func(*RunRequest){
		"a shell command":        func(run *RunRequest) { run.Command.Shell = true },
		"no executable":          func(run *RunRequest) { run.Command.Executable = "" },
		"an absolute workdir":    func(run *RunRequest) { run.Command.WorkingDirectory = "/etc" },
		"an unknown access mode": func(run *RunRequest) { run.WorkspaceAccess = "read-mostly" },
		"a zero timeout":         func(run *RunRequest) { run.Limits.TimeoutSeconds = 0 },
		"a malformed workspace":  func(run *RunRequest) { run.WorkspaceID = strings.ToUpper(testWorkspaceID) },
		"a line separator":       func(run *RunRequest) { run.Reason = "first\u2028second" },
		"invalid UTF-8":          func(run *RunRequest) { run.Command.Arguments = []string{"\xff"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			run := testRunRequest()
			mutate(&run)
			encoded, err := encodeRun(run)
			if !errors.Is(err, ErrInvalidMessage) || !errors.Is(err, protocol.ErrInvalidExecutionRequest) {
				t.Fatalf("encodeRun() = %s, %v; want ErrInvalidMessage wrapping ErrInvalidExecutionRequest", encoded, err)
			}
		})
	}
}

func TestBuildExecutionRequest_FillsMiosFields(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 30, 15, 500_000_000, time.UTC)
	request, err := buildExecutionRequest([]byte(validRunJSON), 1001, now)
	if err != nil {
		t.Fatalf("buildExecutionRequest() error = %v", err)
	}
	run := testRunRequest()
	want := protocol.ExecutionRequest{
		ExecutionID:     request.ExecutionID,
		Principals:      protocol.Principals{UserID: "local:1001", AgentID: "local", ClientID: "local-socket"},
		Resources:       protocol.Resources{MachineID: "local", WorkspaceID: testWorkspaceID},
		GrantID:         "local",
		SessionID:       "local",
		Command:         run.Command,
		WorkspaceAccess: run.WorkspaceAccess,
		Network:         protocol.NetworkNone,
		Limits:          run.Limits,
		Reason:          run.Reason,
		CreatedAt:       time.Date(2026, 10, 17, 9, 30, 15, 0, time.UTC),
		ExpiresAt:       time.Date(2026, 10, 17, 10, 30, 15, 0, time.UTC),
		Nonce:           request.Nonce,
	}
	if !equalRequests(request, want) {
		t.Fatalf("buildExecutionRequest() =\n%+v\nwant\n%+v", request, want)
	}
	if !strings.HasPrefix(request.ExecutionID, "local-") || len(request.ExecutionID) != len("local-")+32 {
		t.Fatalf("ExecutionID = %q, want local- and 32 random hex digits", request.ExecutionID)
	}
	if nonce, err := base64.RawURLEncoding.DecodeString(request.Nonce); err != nil || len(nonce) != 16 {
		t.Fatalf("Nonce = %q, want 16 random bytes in unpadded base64url", request.Nonce)
	}
	if err := request.ValidateAt(now); err != nil {
		t.Fatalf("the built request is not valid now: %v", err)
	}

	again, err := buildExecutionRequest([]byte(validRunJSON), 1001, now)
	if err != nil {
		t.Fatalf("buildExecutionRequest() again error = %v", err)
	}
	if again.ExecutionID == request.ExecutionID || again.Nonce == request.Nonce {
		t.Fatalf("two builds share an execution ID or a nonce: %q/%q", again.ExecutionID, again.Nonce)
	}
}

// TestSecurity_BuildExecutionRequestAppliesADR007Rules sends the client's own
// text through ADR-007's strict decoder: IPC v1's checks alone would let null
// become zero and a lone surrogate become U+FFFD.
func TestSecurity_BuildExecutionRequestAppliesADR007Rules(t *testing.T) {
	tests := map[string]string{
		"not an object":             `"go test"`,
		"invalid JSON":              `{"workspaceId":`,
		"a network key":             strings.Replace(validRunJSON, `"reason"`, `"network":"none","reason"`, 1),
		"principals":                strings.Replace(validRunJSON, `"reason"`, `"principals":{"userId":"u","agentId":"a","clientId":"c"},"reason"`, 1),
		"a missing reason":          strings.Replace(validRunJSON, `,"reason":"local run by the workspace owner"`, ``, 1),
		"a duplicate key":           strings.Replace(validRunJSON, `"reason"`, `"reason":"x","reason"`, 1),
		"a shell command":           strings.Replace(validRunJSON, `"shell":false`, `"shell":true`, 1),
		"a null limit":              strings.Replace(validRunJSON, `"pids":512`, `"pids":null`, 1),
		"a null argument list":      strings.Replace(validRunJSON, `["test","./..."]`, `null`, 1),
		"a lone surrogate":          strings.Replace(validRunJSON, `"test"`, `"\ud800"`, 1),
		"an unknown command key":    strings.Replace(validRunJSON, `"shell":false`, `"shell":false,"env":{}`, 1),
		"an unknown limits key":     strings.Replace(validRunJSON, `"pids":512`, `"pids":512,"swapBytes":0`, 1),
		"an uppercase workspace":    strings.Replace(validRunJSON, testWorkspaceID, strings.ToUpper(testWorkspaceID), 1),
		"a zero timeout":            strings.Replace(validRunJSON, `"timeoutSeconds":600`, `"timeoutSeconds":0`, 1),
		"a fractional limit":        strings.Replace(validRunJSON, `"pids":512`, `"pids":512.5`, 1),
		"a numeric workspace":       strings.Replace(validRunJSON, `"`+testWorkspaceID+`"`, `7`, 1),
		"an escaped line separator": strings.Replace(validRunJSON, `"local run by the workspace owner"`, `"a\u2028b"`, 1),
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			request, err := buildExecutionRequest([]byte(run), 1001, time.Now())
			if !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("buildExecutionRequest() = %+v, %v; want ErrInvalidMessage", request, err)
			}
		})
	}
}

func TestDecodeRequest_ChecksTheRunObjectsKeys(t *testing.T) {
	frame := func(run string) string { return requestPrefix + `"command":"execution.run","run":` + run + `}` }
	message, err := decodeRequest([]byte(frame(validRunJSON)))
	if err != nil || message.Command != runCommand || message.Run != validRunJSON {
		t.Fatalf("decodeRequest() = %+v, %v; want the run object unchanged", message, err)
	}

	payloads := map[string]string{
		"a network key":    frame(strings.Replace(validRunJSON, `"reason"`, `"network":"none","reason"`, 1)),
		"a missing reason": frame(strings.Replace(validRunJSON, `,"reason":"local run by the workspace owner"`, ``, 1)),
		"a miscased key":   frame(strings.Replace(validRunJSON, `"workspaceId"`, `"workspaceID"`, 1)),
		"not an object":    frame(`[]`),
		"no run":           requestPrefix + `"command":"execution.run"}`,
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			if message, err := decodeRequest([]byte(payload)); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("decodeRequest() = %+v, %v; want ErrInvalidMessage", message, err)
			}
		})
	}
}

func TestSecurity_RunStaysWithItsCommand(t *testing.T) {
	base := request{ProtocolVersion: protocolVersion, RequestID: testRequestID}
	with := func(command string, mutate func(*request)) request {
		message := base
		message.Command = command
		mutate(&message)
		return message
	}
	messages := map[string]request{
		"status with a run":           with(statusCommand, func(m *request) { m.Run = validRunJSON }),
		"workspace.add with a run":    with(addWorkspaceCommand, func(m *request) { m.Path, m.Run = "/srv/project", validRunJSON }),
		"workspace.list with a run":   with(listWorkspacesCommand, func(m *request) { m.Run = validRunJSON }),
		"workspace.remove with a run": with(removeWorkspaceCommand, func(m *request) { m.ID, m.Run = testWorkspaceID, validRunJSON }),
		"a run with a path":           with(runCommand, func(m *request) { m.Run, m.Path = validRunJSON, "/srv/project" }),
		"a run with an ID":            with(runCommand, func(m *request) { m.Run, m.ID = validRunJSON, testWorkspaceID }),
		"a run with a cursor":         with(runCommand, func(m *request) { m.Run, m.Cursor = validRunJSON, testWorkspaceID }),
	}
	for name, message := range messages {
		t.Run(name, func(t *testing.T) {
			if encoded, err := encodeRequest(message); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("encodeRequest() = %s, %v; want ErrInvalidMessage", encoded, err)
			}
		})
	}
	if _, err := encodeRequest(with(runCommand, func(m *request) { m.Run = validRunJSON })); err != nil {
		t.Fatalf("encodeRequest() of a valid run error = %v", err)
	}
}

func TestEncodeOutputFrame_StaysWithinTheFrameLimit(t *testing.T) {
	data := bytes.Repeat([]byte{0xff}, maxOutputFrameData)
	frame, err := encodeOutputFrame(testRequestID, protocol.StreamStderr, data)
	if err != nil {
		t.Fatalf("encodeOutputFrame() error = %v", err)
	}
	if len(frame) > MaxFrameBytes {
		t.Fatalf("an output frame of %d bytes exceeds MaxFrameBytes", len(frame))
	}
	decoded, err := decodeRunFrame(frame, testRequestID)
	if err != nil || decoded.kind != outputFrame || decoded.stream != protocol.StreamStderr || !bytes.Equal(decoded.data, data) {
		t.Fatalf("decodeRunFrame() = %+v, %v; want the stderr data back", decoded, err)
	}

	for _, size := range []int{0, maxOutputFrameData + 1} {
		if frame, err := encodeOutputFrame(testRequestID, protocol.StreamStdout, make([]byte, size)); err == nil {
			t.Fatalf("encodeOutputFrame() with %d bytes = %d bytes, want an error", size, len(frame))
		}
	}
}

func TestSecurity_FrameEncodersRefuseInvalidFrames(t *testing.T) {
	request := mustBuildRequest(t)
	invalidNotice := startedFor(request)
	invalidNotice.ActionHash = "not-a-hash"
	invalidResult := succeededResult(request, 0, 0)
	invalidResult.ExitCode = 3

	tests := map[string]func() ([]byte, error){
		"an invalid start notice": func() ([]byte, error) { return encodeStartedFrame(testRequestID, invalidNotice) },
		"an invalid result":       func() ([]byte, error) { return encodeResultFrame(testRequestID, invalidResult) },
		"an unknown stream": func() ([]byte, error) {
			return encodeOutputFrame(testRequestID, protocol.OutputStream("stdin"), []byte("x"))
		},
		"an invalid request ID": func() ([]byte, error) {
			return encodeResultFrame(strings.ToUpper(testRequestID), succeededResult(request, 0, 0))
		},
		"an oversized frame": func() ([]byte, error) {
			return encodeRunFrame(testRequestID, resultFrame, bytes.Repeat([]byte{' '}, MaxFrameBytes))
		},
	}
	for name, encode := range tests {
		t.Run(name, func(t *testing.T) {
			if frame, err := encode(); err == nil {
				t.Fatalf("the encoder returned %q, want an error", frame)
			}
		})
	}
}

func TestReadRunReplies_DeliversFramesInOrder(t *testing.T) {
	request := mustBuildRequest(t)
	started := startedFor(request)
	result := succeededResult(request, 3, 2)
	var stream bytes.Buffer
	writeTestFrame(t, &stream, mustFrame(encodeStartedFrame(testRequestID, started)))
	writeTestFrame(t, &stream, mustFrame(encodeOutputFrame(testRequestID, protocol.StreamStdout, []byte("out"))))
	writeTestFrame(t, &stream, mustFrame(encodeOutputFrame(testRequestID, protocol.StreamStderr, []byte("er"))))
	writeTestFrame(t, &stream, mustFrame(encodeResultFrame(testRequestID, result)))
	writeTestFrame(t, &stream, mustFrame(encodeOutputFrame(testRequestID, protocol.StreamStdout, []byte("never read"))))

	var events []string
	got, err := readRunReplies(&stream, testRequestID, RunEvents{
		Started: func(notice protocol.ExecutionStarted) {
			events = append(events, "started "+notice.ExecutionID)
		},
		Output: func(stream protocol.OutputStream, data []byte) {
			events = append(events, string(stream)+" "+string(data))
		},
	})
	if err != nil {
		t.Fatalf("readRunReplies() error = %v", err)
	}
	if !equalResults(got, result) {
		t.Fatalf("readRunReplies() = %+v, want %+v", got, result)
	}
	want := []string{"started " + request.ExecutionID, "stdout out", "stderr er"}
	if strings.Join(events, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %q, want %q", events, want)
	}
	if stream.Len() == 0 {
		t.Fatal("readRunReplies() read past the result frame")
	}

	// A refusal is a lone result frame, and nil callbacks are allowed.
	rejected := protocol.ExecutionResult{
		ExecutionID: request.ExecutionID,
		ActionHash:  started.ActionHash,
		State:       protocol.StateRejected,
		Error:       protocol.ResultError{Code: protocol.ErrorCodeBusy, Message: "busy"},
	}
	stream.Reset()
	writeTestFrame(t, &stream, mustFrame(encodeResultFrame(testRequestID, rejected)))
	if got, err := readRunReplies(&stream, testRequestID, RunEvents{}); err != nil || !equalResults(got, rejected) {
		t.Fatalf("readRunReplies() of a refusal = %+v, %v", got, err)
	}
}

func TestSecurity_ReadRunRepliesRejectsInvalidFrames(t *testing.T) {
	request := mustBuildRequest(t)
	startedJSON := string(mustFrame(startedFor(request).CanonicalJSON()))
	resultJSON := string(mustFrame(succeededResult(request, 0, 0).CanonicalJSON()))
	prefix := `{"protocolVersion":1,"requestId":"` + testRequestID + `",`
	startedFrame := prefix + `"started":` + startedJSON + `}`
	resultFrame := prefix + `"result":` + resultJSON + `}`
	output := func(body string) string { return prefix + `"output":` + body + `}` }

	other := request
	other.ExecutionID = "local-other"
	otherResult := string(mustFrame(succeededResult(other, 0, 0).CanonicalJSON()))
	sameIDOtherHash := request
	sameIDOtherHash.Reason = "another reason"
	otherHashResult := string(mustFrame(succeededResult(sameIDOtherHash, 0, 0).CanonicalJSON()))

	tests := map[string]struct {
		frames []string
		want   error
	}{
		"another request ID":       {[]string{strings.Replace(startedFrame, testRequestID, strings.Repeat("a", 32), 1)}, ErrInvalidMessage},
		"another protocol version": {[]string{strings.Replace(startedFrame, `"protocolVersion":1`, `"protocolVersion":2`, 1)}, ErrInvalidMessage},
		"an unknown frame":         {[]string{prefix + `"progress":{}}`}, ErrInvalidMessage},
		"two payloads":             {[]string{prefix + `"started":` + startedJSON + `,"result":` + resultJSON + `}`}, ErrInvalidMessage},
		"no payload":               {[]string{`{"protocolVersion":1,"requestId":"` + testRequestID + `"}`}, ErrInvalidMessage},
		"invalid JSON":             {[]string{prefix + `"started":`}, ErrInvalidMessage},
		"a duplicate key":          {[]string{prefix + `"requestId":"` + testRequestID + `","result":` + resultJSON + `}`}, ErrInvalidMessage},
		"output first":             {[]string{output(`{"stream":"stdout","data":"aGk"}`)}, ErrInvalidMessage},
		"two start notices":        {[]string{startedFrame, startedFrame}, ErrInvalidMessage},
		"empty output":             {[]string{startedFrame, output(`{"stream":"stdout","data":""}`)}, ErrInvalidMessage},
		"padded output":            {[]string{startedFrame, output(`{"stream":"stdout","data":"aGk="}`)}, ErrInvalidMessage},
		"standard base64 output":   {[]string{startedFrame, output(`{"stream":"stdout","data":"+/8"}`)}, ErrInvalidMessage},
		"non-canonical output":     {[]string{startedFrame, output(`{"stream":"stdout","data":"aGl"}`)}, ErrInvalidMessage},
		"an unknown stream":        {[]string{startedFrame, output(`{"stream":"stdin","data":"aGk"}`)}, ErrInvalidMessage},
		"an extra output key":      {[]string{startedFrame, output(`{"stream":"stdout","data":"aGk","sequence":1}`)}, ErrInvalidMessage},
		"null output data":         {[]string{startedFrame, output(`{"stream":"stdout","data":null}`)}, ErrInvalidMessage},
		"oversized output": {[]string{startedFrame, output(`{"stream":"stdout","data":"` +
			base64.RawURLEncoding.EncodeToString(make([]byte, maxOutputFrameData+1)) + `"}`)}, ErrInvalidMessage},
		"an invalid start notice":      {[]string{strings.Replace(startedFrame, `"actionHash"`, `"actionhash"`, 1)}, ErrInvalidMessage},
		"an invalid result":            {[]string{strings.Replace(resultFrame, `"succeeded"`, `"done"`, 1)}, ErrInvalidMessage},
		"another action hash's result": {[]string{startedFrame, prefix + `"result":` + otherHashResult + `}`}, ErrInvalidMessage},
		"output with a newline":        {[]string{startedFrame, output(`{"stream":"stdout","data":"aG\nk"}`)}, ErrInvalidMessage},
		"another execution's result":   {[]string{startedFrame, prefix + `"result":` + otherResult + `}`}, ErrInvalidMessage},
		"no result":                    {[]string{startedFrame}, ErrNoRunResult},
		"no frames":                    {nil, ErrNoRunResult},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var stream bytes.Buffer
			for _, frame := range test.frames {
				writeTestFrame(t, &stream, []byte(frame))
			}
			result, err := readRunReplies(&stream, testRequestID, RunEvents{})
			if !errors.Is(err, test.want) {
				t.Fatalf("readRunReplies() = %+v, %v; want %v", result, err, test.want)
			}
			if !equalResults(result, protocol.ExecutionResult{}) {
				t.Fatalf("readRunReplies() returned a result with its error: %+v", result)
			}
		})
	}
}

func writeTestFrame(t *testing.T, stream *bytes.Buffer, payload []byte) {
	t.Helper()
	if err := writeFrame(stream, payload); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}
}

func mustFrame(encoded []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return encoded
}

func mustBuildRequest(t *testing.T) protocol.ExecutionRequest {
	t.Helper()
	request, err := buildExecutionRequest([]byte(validRunJSON), 1001, time.Now())
	if err != nil {
		t.Fatalf("buildExecutionRequest() error = %v", err)
	}
	return request
}

// startedFor returns a valid start notice for request.
func startedFor(request protocol.ExecutionRequest) protocol.ExecutionStarted {
	actionHash, err := request.ActionHash()
	if err != nil {
		panic(err)
	}
	return protocol.ExecutionStarted{
		ExecutionID: request.ExecutionID,
		ActionHash:  actionHash,
		StartedAt:   time.Date(2026, 10, 17, 9, 30, 16, 250_000_000, time.UTC),
	}
}

// succeededResult returns a valid succeeded result for request that reports
// stdoutBytes and stderrBytes of output.
func succeededResult(request protocol.ExecutionRequest, stdoutBytes, stderrBytes uint64) protocol.ExecutionResult {
	notice := startedFor(request)
	var chunks uint64
	if stdoutBytes > 0 {
		chunks++
	}
	if stderrBytes > 0 {
		chunks++
	}
	return protocol.ExecutionResult{
		ExecutionID: request.ExecutionID,
		ActionHash:  notice.ActionHash,
		State:       protocol.StateSucceeded,
		StartedAt:   notice.StartedAt,
		EndedAt:     notice.StartedAt.Add(1500 * time.Millisecond),
		Output:      protocol.OutputSummary{StdoutBytes: stdoutBytes, StderrBytes: stderrBytes, Chunks: chunks},
	}
}

// cancelledResult returns a valid cancelled result for request.
func cancelledResult(request protocol.ExecutionRequest) protocol.ExecutionResult {
	result := succeededResult(request, 0, 0)
	result.State = protocol.StateCancelled
	return result
}

func equalRequests(got, want protocol.ExecutionRequest) bool {
	gotJSON, gotErr := got.CanonicalJSON()
	wantJSON, wantErr := want.CanonicalJSON()
	return gotErr == nil && wantErr == nil && bytes.Equal(gotJSON, wantJSON)
}

func equalResults(got, want protocol.ExecutionResult) bool {
	return got.ExecutionID == want.ExecutionID &&
		got.ActionHash == want.ActionHash &&
		got.State == want.State &&
		got.StartedAt.Equal(want.StartedAt) &&
		got.EndedAt.Equal(want.EndedAt) &&
		got.ExitCode == want.ExitCode &&
		got.Output == want.Output &&
		got.Error == want.Error
}
