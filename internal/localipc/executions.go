package localipc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

const (
	runCommand = "execution.run"
	// maxConcurrentRuns bounds executions, so that runs can never hold all
	// maxConcurrentConnections (ADR-011).
	maxConcurrentRuns = 4
	// maxOutputFrameData bounds the bytes in one output frame, so that their
	// base64url form and the frame's other fields fit in MaxFrameBytes.
	maxOutputFrameData = 32 << 10
	// defaultFrameWriteTimeout bounds writing one reply frame of a run.
	defaultFrameWriteTimeout = 30 * time.Second

	startedFrame = "started"
	outputFrame  = "output"
	resultFrame  = "result"

	// Mio fills these request fields for every local run (ADR-011).
	localExecutionIDPrefix = "local-"
	localAgentID           = "local"
	localClientID          = "local-socket"
	localMachineID         = "local"
	localGrantID           = "local"
	localSessionID         = "local"
	localRequestLifetime   = time.Hour
	localNonceBytes        = 16
)

// runKeys lists the exact keys of execution.run's run object.
var runKeys = []string{"workspaceId", "command", "workspaceAccess", "limits", "reason"}

// ErrNoRunResult reports a run whose connection ended before its result
// frame, so the run's outcome is unknown. Mio ends a run this way when it
// stops reading the client, or when its executor fails without a valid result.
var ErrNoRunResult = errors.New("localipc: Mio ended the run without a result")

// Executor runs local executions for Mio (ADR-011). Execute reports the
// start notice and output chunks through events, in order and from one
// goroutine at a time, stops the command when ctx is done, and returns the
// terminal result. An error or a result that does not belong to request ends
// the run without a result frame.
type Executor interface {
	Execute(ctx context.Context, request protocol.ExecutionRequest, events ExecutionEvents) (protocol.ExecutionResult, error)
}

// ExecutionEvents receives an execution's start notice and output chunks.
// Both callbacks are always set; each blocks until its frames are written.
type ExecutionEvents struct {
	Started func(protocol.ExecutionStarted)
	Output  func(protocol.ExecutionOutput)
}

// RunRequest is what a client asks Mio to run (ADR-011): ADR-007's command,
// workspace access, limits, and reason, in a registered workspace. Mio fills
// in every other request field itself.
type RunRequest struct {
	WorkspaceID     string
	Command         protocol.Command
	WorkspaceAccess protocol.WorkspaceAccess
	Limits          protocol.Limits
	Reason          string
}

// RunEvents receives a run's start notice and output frames as they arrive.
// A nil callback is skipped. Output data holds 1 to 32768 bytes, exactly as
// the command wrote them, and is only valid during the call.
type RunEvents struct {
	Started func(protocol.ExecutionStarted)
	Output  func(stream protocol.OutputStream, data []byte)
}

// EnableExecutions makes the daemon serve execution.run (ADR-011) with
// executor. It must be called before Serve; until then, execution.run is
// treated as an unknown command and gets no reply.
func (d *Daemon) EnableExecutions(executor Executor) error {
	if d == nil || executor == nil {
		return errors.New("localipc: a daemon and an executor are required")
	}
	d.serveMu.Lock()
	defer d.serveMu.Unlock()
	if d.served {
		return ErrDaemonAlreadyServing
	}
	d.executor = executor
	return nil
}

// RunExecution asks Mio to run run locally (ADR-011), passes the start notice
// and output to events as their frames arrive, and returns the result. A run
// that Mio refuses returns a rejected result and no error. The run is checked
// before the filesystem or the socket is touched. Cancelling ctx closes the
// connection, which makes Mio cancel the run; only ctx bounds how long the
// run may take. A connection that ends before the result returns
// ErrNoRunResult.
func RunExecution(ctx context.Context, stateDir string, run RunRequest, events RunEvents) (protocol.ExecutionResult, error) {
	if ctx == nil {
		return protocol.ExecutionResult{}, ErrInvalidContext
	}
	if !platformSupported() {
		return protocol.ExecutionResult{}, ErrUnsupportedPlatform
	}
	encodedRun, err := encodeRun(run)
	if err != nil {
		return protocol.ExecutionResult{}, err
	}
	requestID, err := newRequestID()
	if err != nil {
		return protocol.ExecutionResult{}, err
	}
	encodedRequest, err := encodeRequest(request{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		Command:         runCommand,
		Run:             string(encodedRun),
	})
	if err != nil {
		return protocol.ExecutionResult{}, err
	}

	connection, err := dialMio(ctx, stateDir)
	if err != nil {
		return protocol.ExecutionResult{}, err
	}
	defer connection.Close()
	stopCancellation := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancellation()

	if err := setConnectionDeadline(ctx, connection); err != nil {
		return protocol.ExecutionResult{}, contextualError(ctx, "set IPC deadline", err)
	}
	if err := writeFrame(connection, encodedRequest); err != nil {
		return protocol.ExecutionResult{}, contextualError(ctx, "write run request", err)
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return protocol.ExecutionResult{}, contextualError(ctx, "clear IPC deadline", err)
	}
	result, err := readRunReplies(connection, requestID, events)
	if err != nil {
		return protocol.ExecutionResult{}, contextualError(ctx, "read run replies", err)
	}
	return result, nil
}

// runJSON is the wire form of a run object.
type runJSON struct {
	WorkspaceID     string      `json:"workspaceId"`
	Command         commandJSON `json:"command"`
	WorkspaceAccess string      `json:"workspaceAccess"`
	Limits          limitsJSON  `json:"limits"`
	Reason          string      `json:"reason"`
}

type commandJSON struct {
	Executable       string   `json:"executable"`
	Arguments        []string `json:"arguments"`
	Shell            bool     `json:"shell"`
	WorkingDirectory string   `json:"workingDirectory"`
}

type limitsJSON struct {
	TimeoutSeconds uint64 `json:"timeoutSeconds"`
	CPUMillicores  uint64 `json:"cpuMillicores"`
	MemoryBytes    uint64 `json:"memoryBytes"`
	PIDs           uint64 `json:"pids"`
	OutputBytes    uint64 `json:"outputBytes"`
}

// encodeRun returns run's wire form after checking it against ADR-007 with
// the values that Mio fills in. The typed fields are checked, not the JSON,
// because json.Marshal silently replaces invalid UTF-8.
func encodeRun(run RunRequest) ([]byte, error) {
	placeholderCreatedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	placeholder := localRequest(run, 0, localExecutionIDPrefix+"validation", "AAAAAAAAAAAAAAAAAAAAAA", placeholderCreatedAt)
	if err := placeholder.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidMessage, err)
	}

	arguments := run.Command.Arguments
	if arguments == nil {
		arguments = []string{}
	}
	encoded, err := json.Marshal(runJSON{
		WorkspaceID: run.WorkspaceID,
		Command: commandJSON{
			Executable:       run.Command.Executable,
			Arguments:        arguments,
			Shell:            run.Command.Shell,
			WorkingDirectory: run.Command.WorkingDirectory,
		},
		WorkspaceAccess: string(run.WorkspaceAccess),
		Limits: limitsJSON{
			TimeoutSeconds: run.Limits.TimeoutSeconds,
			CPUMillicores:  run.Limits.CPUMillicores,
			MemoryBytes:    run.Limits.MemoryBytes,
			PIDs:           run.Limits.PIDs,
			OutputBytes:    run.Limits.OutputBytes,
		},
		Reason: run.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode run: %v", ErrInvalidMessage, err)
	}
	return encoded, nil
}

// localRequest returns the request for run with Mio's fields filled in.
func localRequest(run RunRequest, uid uint32, executionID, nonce string, createdAt time.Time) protocol.ExecutionRequest {
	return protocol.ExecutionRequest{
		ExecutionID:     executionID,
		Principals:      protocol.Principals{UserID: localUserID(uid), AgentID: localAgentID, ClientID: localClientID},
		Resources:       protocol.Resources{MachineID: localMachineID, WorkspaceID: run.WorkspaceID},
		GrantID:         localGrantID,
		SessionID:       localSessionID,
		Command:         run.Command,
		WorkspaceAccess: run.WorkspaceAccess,
		Network:         protocol.NetworkNone,
		Limits:          run.Limits,
		Reason:          run.Reason,
		CreatedAt:       createdAt,
		ExpiresAt:       createdAt.Add(localRequestLifetime),
		Nonce:           nonce,
	}
}

func localUserID(uid uint32) string {
	return "local:" + strconv.FormatUint(uint64(uid), 10)
}

// buildExecutionRequest returns the ADR-007 request for a client's run object
// (ADR-011). Mio fills in every field that a client may not choose: local
// principals for uid, a random execution ID and nonce, a one-hour window from
// now, and no network. The client's fields are copied unchanged into the
// request, and the whole request is decoded with
// protocol.DecodeExecutionRequest, so that the client's own text meets every
// ADR-007 rule; IPC v1's checks alone would let null become zero and a lone
// surrogate become U+FFFD.
func buildExecutionRequest(run []byte, uid uint32, now time.Time) (protocol.ExecutionRequest, error) {
	if err := validateObjectKeys(run, runKeys); err != nil {
		return protocol.ExecutionRequest{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(run, &fields); err != nil {
		return protocol.ExecutionRequest{}, fmt.Errorf("%w: the run must be an object", ErrInvalidMessage)
	}
	executionID, err := newRequestID()
	if err != nil {
		return protocol.ExecutionRequest{}, err
	}
	var nonce [localNonceBytes]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return protocol.ExecutionRequest{}, fmt.Errorf("generate execution nonce: %w", err)
	}
	createdAt := now.UTC().Truncate(time.Second)

	// Every value spliced in is either one of the run's own JSON values,
	// which json.Unmarshal has checked, or a string that needs no escaping.
	var payload bytes.Buffer
	payload.WriteString(`{"executionId":"` + localExecutionIDPrefix + executionID + `",`)
	payload.WriteString(`"principals":{"userId":"` + localUserID(uid) + `","agentId":"` + localAgentID + `","clientId":"` + localClientID + `"},`)
	payload.WriteString(`"resources":{"machineId":"` + localMachineID + `","workspaceId":`)
	payload.Write(fields["workspaceId"])
	payload.WriteString(`},"grantId":"` + localGrantID + `","sessionId":"` + localSessionID + `","command":`)
	payload.Write(fields["command"])
	payload.WriteString(`,"workspaceAccess":`)
	payload.Write(fields["workspaceAccess"])
	payload.WriteString(`,"network":"` + string(protocol.NetworkNone) + `","limits":`)
	payload.Write(fields["limits"])
	payload.WriteString(`,"reason":`)
	payload.Write(fields["reason"])
	payload.WriteString(`,"createdAt":"` + createdAt.Format(time.RFC3339) + `",`)
	payload.WriteString(`"expiresAt":"` + createdAt.Add(localRequestLifetime).Format(time.RFC3339) + `",`)
	payload.WriteString(`"nonce":"` + base64.RawURLEncoding.EncodeToString(nonce[:]) + `"}`)

	request, err := protocol.DecodeExecutionRequest(payload.Bytes())
	if err != nil {
		return protocol.ExecutionRequest{}, fmt.Errorf("%w: %w", ErrInvalidMessage, err)
	}
	return request, nil
}

// serveRun answers an execution.run request whose frame has been read
// (ADR-011). A daemon without an executor and a malformed run get no reply.
// A fifth concurrent run gets a busy refusal. Otherwise the executor runs
// until it returns, the client closes the connection, a reply frame misses
// its write deadline, or ctx is done, and its events and result are written
// as frames.
func (d *Daemon) serveRun(ctx context.Context, connection *net.UnixConn, message request, uid uint32) {
	if d.executor == nil {
		return
	}
	request, err := buildExecutionRequest([]byte(message.Run), uid, time.Now())
	if err != nil {
		return
	}
	actionHash, err := request.ActionHash()
	if err != nil {
		return
	}
	stream := &runStream{
		connection: connection,
		requestID:  message.RequestID,
		timeout:    d.frameWriteTimeout,
		request:    request,
	}

	select {
	case d.runs <- struct{}{}:
		defer func() { <-d.runs }()
	default:
		stream.result(protocol.ExecutionResult{
			ExecutionID: request.ExecutionID,
			ActionHash:  actionHash,
			State:       protocol.StateRejected,
			Error: protocol.ResultError{
				Code:    protocol.ErrorCodeBusy,
				Message: fmt.Sprintf("Mio is already running %d executions", maxConcurrentRuns),
			},
		})
		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream.cancel = cancel
	// The request has been read, so reading now only watches for the client
	// closing the connection, which cancels the run. A client must send
	// nothing more; anything it sends also cancels the run.
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		return
	}
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		var buffer [1]byte
		_, _ = connection.Read(buffer[:])
		cancel()
	}()
	defer func() {
		_ = connection.Close()
		<-watcherDone
	}()

	result, err := d.executor.Execute(runCtx, request, ExecutionEvents{Started: stream.started, Output: stream.output})
	if err != nil || result.ValidateAgainst(request) != nil {
		return
	}
	stream.result(result)
}

// runStream writes one run's reply frames. It checks that the executor's
// events belong to the request and arrive in order. After a fault or a failed
// write, it cancels the run and writes nothing more, so the client receives
// no result.
type runStream struct {
	connection *net.UnixConn
	requestID  string
	timeout    time.Duration
	request    protocol.ExecutionRequest
	cancel     context.CancelFunc

	mu       sync.Mutex
	sawStart bool
	failed   bool
}

func (s *runStream) started(notice protocol.ExecutionStarted) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sawStart || notice.ValidateAgainst(s.request) != nil {
		s.fail()
		return
	}
	s.sawStart = true
	s.write(encodeStartedFrame(s.requestID, notice))
}

// output writes chunk as output frames of at most maxOutputFrameData bytes.
func (s *runStream) output(chunk protocol.ExecutionOutput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sawStart || chunk.ExecutionID != s.request.ExecutionID || chunk.Validate() != nil {
		s.fail()
		return
	}
	for data := chunk.Data; len(data) > 0; {
		size := min(len(data), maxOutputFrameData)
		s.write(encodeOutputFrame(s.requestID, chunk.Stream, data[:size]))
		data = data[size:]
	}
}

func (s *runStream) result(result protocol.ExecutionResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.write(encodeResultFrame(s.requestID, result))
}

// write sends one encoded frame within the write deadline, unless the stream
// has failed: after a write times out, part of a frame may have been sent, so
// nothing more may follow. s.mu must be held.
func (s *runStream) write(frame []byte, err error) {
	if s.failed {
		return
	}
	if err == nil {
		err = s.connection.SetWriteDeadline(time.Now().Add(s.timeout))
	}
	if err == nil {
		err = writeFrame(s.connection, frame)
	}
	if err != nil {
		s.fail()
	}
}

// fail stops the stream and cancels the run. s.mu must be held.
func (s *runStream) fail() {
	s.failed = true
	if s.cancel != nil {
		s.cancel()
	}
}

// runFrame is one decoded reply frame of a run.
type runFrame struct {
	kind    string
	started protocol.ExecutionStarted
	stream  protocol.OutputStream
	data    []byte
	result  protocol.ExecutionResult
}

// decodeRunFrame strictly decodes one reply frame of the run with requestID:
// exactly the protocolVersion, requestId, and one started, output, or result
// payload.
func decodeRunFrame(encoded []byte, requestID string) (runFrame, error) {
	if err := checkJSONDocument(encoded); err != nil {
		return runFrame{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return runFrame{}, fmt.Errorf("%w: a run frame must be an object", ErrInvalidMessage)
	}
	frame := runFrame{}
	for _, kind := range []string{startedFrame, outputFrame, resultFrame} {
		if _, ok := fields[kind]; ok && frame.kind == "" {
			frame.kind = kind
		}
	}
	if frame.kind == "" {
		return runFrame{}, fmt.Errorf("%w: a run frame needs a started, output, or result payload", ErrInvalidMessage)
	}
	if err := validateObjectKeys(encoded, []string{"protocolVersion", "requestId", frame.kind}); err != nil {
		return runFrame{}, err
	}
	var version int
	if err := json.Unmarshal(fields["protocolVersion"], &version); err != nil || version != protocolVersion {
		return runFrame{}, fmt.Errorf("%w: unsupported run frame version", ErrInvalidMessage)
	}
	if id, err := jsonString(fields["requestId"]); err != nil || id != requestID {
		return runFrame{}, fmt.Errorf("%w: the run frame answers another request", ErrInvalidMessage)
	}

	payload := fields[frame.kind]
	var err error
	switch frame.kind {
	case startedFrame:
		frame.started, err = protocol.DecodeExecutionStarted(payload)
	case outputFrame:
		frame.stream, frame.data, err = decodeOutputPayload(payload)
	case resultFrame:
		frame.result, err = protocol.DecodeExecutionResult(payload)
	}
	if err != nil {
		return runFrame{}, fmt.Errorf("%w: %s frame: %w", ErrInvalidMessage, frame.kind, err)
	}
	return frame, nil
}

// decodeOutputPayload decodes an output frame's {stream, data}, whose data is
// canonical unpadded base64url of 1 to maxOutputFrameData bytes.
func decodeOutputPayload(payload []byte) (protocol.OutputStream, []byte, error) {
	if err := validateObjectKeys(payload, []string{"stream", "data"}); err != nil {
		return "", nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return "", nil, errors.New("the output must be an object")
	}
	stream, err := jsonString(fields["stream"])
	if err != nil || (stream != string(protocol.StreamStdout) && stream != string(protocol.StreamStderr)) {
		return "", nil, errors.New(`stream must be "stdout" or "stderr"`)
	}
	encoded, err := jsonString(fields["data"])
	if err != nil {
		return "", nil, errors.New("data must be a string")
	}
	// Decoding skips newlines, so only re-encoding proves the form canonical.
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxOutputFrameData || base64.RawURLEncoding.EncodeToString(data) != encoded {
		return "", nil, fmt.Errorf("data must be canonical unpadded base64url of 1 to %d bytes", maxOutputFrameData)
	}
	return protocol.OutputStream(stream), data, nil
}

func encodeStartedFrame(requestID string, notice protocol.ExecutionStarted) ([]byte, error) {
	payload, err := notice.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	return encodeRunFrame(requestID, startedFrame, payload)
}

func encodeOutputFrame(requestID string, stream protocol.OutputStream, data []byte) ([]byte, error) {
	if stream != protocol.StreamStdout && stream != protocol.StreamStderr {
		return nil, fmt.Errorf("%w: unknown output stream", ErrInvalidMessage)
	}
	if len(data) == 0 || len(data) > maxOutputFrameData {
		return nil, fmt.Errorf("%w: output frames hold 1 to %d bytes", ErrInvalidMessage, maxOutputFrameData)
	}
	payload := `{"stream":"` + string(stream) + `","data":"` + base64.RawURLEncoding.EncodeToString(data) + `"}`
	return encodeRunFrame(requestID, outputFrame, []byte(payload))
}

func encodeResultFrame(requestID string, result protocol.ExecutionResult) ([]byte, error) {
	payload, err := result.CanonicalJSON()
	if err != nil {
		return nil, err
	}
	return encodeRunFrame(requestID, resultFrame, payload)
}

// encodeRunFrame wraps payload, which must be a JSON object, as the kind
// frame of the run with requestID.
func encodeRunFrame(requestID, kind string, payload []byte) ([]byte, error) {
	if !validRequestID(requestID) {
		return nil, fmt.Errorf("%w: invalid request ID", ErrInvalidMessage)
	}
	frame := make([]byte, 0, len(payload)+96)
	frame = append(frame, `{"protocolVersion":`+strconv.Itoa(protocolVersion)+`,"requestId":"`+requestID+`","`+kind+`":`...)
	frame = append(frame, payload...)
	frame = append(frame, '}')
	if len(frame) > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	return frame, nil
}

// readRunReplies reads the reply frames of the run with requestID up to its
// result: at most one start notice, then output, then exactly one result.
// It reads nothing after the result.
func readRunReplies(reader io.Reader, requestID string, events RunEvents) (protocol.ExecutionResult, error) {
	var notice protocol.ExecutionStarted
	sawStart := false
	for {
		encoded, err := readFrame(reader)
		if err != nil {
			return protocol.ExecutionResult{}, fmt.Errorf("%w: %w", ErrNoRunResult, err)
		}
		frame, err := decodeRunFrame(encoded, requestID)
		if err != nil {
			return protocol.ExecutionResult{}, err
		}
		switch frame.kind {
		case startedFrame:
			if sawStart {
				return protocol.ExecutionResult{}, fmt.Errorf("%w: a second start notice", ErrInvalidMessage)
			}
			sawStart, notice = true, frame.started
			if events.Started != nil {
				events.Started(frame.started)
			}
		case outputFrame:
			if !sawStart {
				return protocol.ExecutionResult{}, fmt.Errorf("%w: output before the start notice", ErrInvalidMessage)
			}
			if events.Output != nil {
				events.Output(frame.stream, frame.data)
			}
		default:
			if sawStart && (frame.result.ExecutionID != notice.ExecutionID || frame.result.ActionHash != notice.ActionHash) {
				return protocol.ExecutionResult{}, fmt.Errorf("%w: the result belongs to another execution", ErrInvalidMessage)
			}
			return frame.result, nil
		}
	}
}
