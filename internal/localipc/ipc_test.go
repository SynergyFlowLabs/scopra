package localipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const validStatusRequest = `{"protocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"status"}`

func TestFrameRoundTrip(t *testing.T) {
	want := []byte(validStatusRequest)
	var frame bytes.Buffer
	if err := writeFrame(&frame, want); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}

	got, err := readFrame(&frame)
	if err != nil {
		t.Fatalf("readFrame() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("readFrame() = %q, want %q", got, want)
	}
}

func TestFrameWriterHandlesShortWrites(t *testing.T) {
	payload := []byte(validStatusRequest)
	writer := &limitedWriter{maxWrite: 2}
	if err := writeFrame(writer, payload); err != nil {
		t.Fatalf("writeFrame() with short writer error = %v", err)
	}
	got, err := readFrame(bytes.NewReader(writer.buffer.Bytes()))
	if err != nil {
		t.Fatalf("readFrame() after short writes error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("short-write frame = %q, want %q", got, payload)
	}

	if err := writeFrame(&limitedWriter{}, payload); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeFrame() with zero-progress writer error = %v, want io.ErrShortWrite", err)
	}
}

func TestWriteFrameRejectsInvalidPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    error
	}{
		{
			name:    "empty payload",
			payload: nil,
			want:    ErrInvalidFrame,
		},
		{
			name:    "oversized payload",
			payload: bytes.Repeat([]byte{'x'}, MaxFrameBytes+1),
			want:    ErrFrameTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var frame bytes.Buffer
			err := writeFrame(&frame, test.payload)
			if !errors.Is(err, test.want) {
				t.Fatalf("writeFrame() error = %v, want %v", err, test.want)
			}
			if frame.Len() != 0 {
				t.Fatalf("writeFrame() wrote %d bytes before rejecting input", frame.Len())
			}
		})
	}
}

func TestReadFrameRejectsInvalidLengths(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{
			name: "zero length",
			data: make([]byte, 4),
			want: ErrInvalidFrame,
		},
		{
			name: "length above maximum",
			data: frameHeader(MaxFrameBytes + 1),
			want: ErrFrameTooLarge,
		},
		{
			name: "truncated header",
			data: []byte{0, 0},
			want: ErrInvalidFrame,
		},
		{
			name: "truncated payload",
			data: append(frameHeader(4), []byte("ab")...),
			want: ErrInvalidFrame,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readFrame(bytes.NewReader(test.data))
			if !errors.Is(err, test.want) {
				t.Fatalf("readFrame() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeRequestRejectsInvalidMessages(t *testing.T) {
	tests := []struct {
		name string
		data string
		want error
	}{
		{
			name: "unknown fields",
			data: `{"protocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"status","extra":true}`,
			want: ErrInvalidMessage,
		},
		{
			name: "duplicate keys",
			data: `{"protocolVersion":1,"protocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"status"}`,
			want: ErrInvalidMessage,
		},
		{
			name: "case-insensitive field alias",
			data: `{"ProtocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"status"}`,
			want: ErrInvalidMessage,
		},
		{
			name: "trailing value",
			data: validStatusRequest + `{}`,
			want: ErrInvalidMessage,
		},
		{
			name: "unsupported version",
			data: `{"protocolVersion":2,"requestId":"0123456789abcdef0123456789abcdef","command":"status"}`,
			want: ErrInvalidMessage,
		},
		{
			name: "unknown command",
			data: `{"protocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"execute"}`,
			want: ErrInvalidMessage,
		},
		{
			name: "invalid request id",
			data: `{"protocolVersion":1,"requestId":"not-random","command":"status"}`,
			want: ErrInvalidMessage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeRequest([]byte(test.data)); !errors.Is(err, test.want) {
				t.Fatalf("decodeRequest() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeRequestRejectsExcessiveJSONNesting(t *testing.T) {
	deepValue := strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1)
	message := `{"protocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"status","extra":` + deepValue + `}`
	if _, err := decodeRequest([]byte(message)); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("decodeRequest() error = %v, want ErrInvalidMessage", err)
	}
}

func TestDecodeResponseRejectsInvalidMessages(t *testing.T) {
	requestID := "0123456789abcdef0123456789abcdef"
	validResponse := `{"protocolVersion":1,"requestId":"` + requestID + `","status":"running","daemonVersion":"dev"}`
	tests := []struct {
		name string
		data string
		want error
	}{
		{
			name: "unknown fields",
			data: strings.TrimSuffix(validResponse, "}") + `,"extra":true}`,
			want: ErrInvalidMessage,
		},
		{
			name: "duplicate keys",
			data: `{"protocolVersion":1,"protocolVersion":1,"requestId":"` + requestID + `","status":"running","daemonVersion":"dev"}`,
			want: ErrInvalidMessage,
		},
		{
			name: "case-insensitive field alias",
			data: strings.Replace(validResponse, `"status"`, `"Status"`, 1),
			want: ErrInvalidMessage,
		},
		{
			name: "wrong request id",
			data: strings.Replace(validResponse, requestID, strings.Repeat("f", 32), 1),
			want: ErrInvalidMessage,
		},
		{
			name: "unsupported status",
			data: strings.Replace(validResponse, `"running"`, `"untrusted"`, 1),
			want: ErrInvalidMessage,
		},
		{
			name: "trailing value",
			data: validResponse + `{}`,
			want: ErrInvalidMessage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeResponse([]byte(test.data), requestID); !errors.Is(err, test.want) {
				t.Fatalf("decodeResponse() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSecurity_RequiresMatchingEffectiveUID(t *testing.T) {
	currentUID := effectiveUID()
	if err := authorizePeer(currentUID, currentUID); err != nil {
		t.Fatalf("authorizePeer(current UID) error = %v", err)
	}

	otherUID := currentUID + 1
	if err := authorizePeer(otherUID, currentUID); !errors.Is(err, ErrUnauthorizedPeer) {
		t.Fatalf("authorizePeer(other UID) error = %v, want ErrUnauthorizedPeer", err)
	}
}

func TestSecurity_RejectsInsecureStateDirectory(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "insecure")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatalf("Chmod(state dir) error = %v", err)
	}

	_, err := Listen(stateDir, "test-version")
	if !errors.Is(err, ErrInsecureStateDir) {
		t.Fatalf("Listen() error = %v, want ErrInsecureStateDir", err)
	}
}

func TestSecurity_RejectsSymlinkStateDirectory(t *testing.T) {
	requireLinux(t)
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("Mkdir(target) error = %v", err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	if _, err := Listen(link, "test-version"); !errors.Is(err, ErrInsecureStateDir) {
		t.Fatalf("Listen(symlink state dir) error = %v, want ErrInsecureStateDir", err)
	}
}

func TestSecurity_RejectsWritableUntrustedParent(t *testing.T) {
	requireLinux(t)
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("Mkdir(parent) error = %v", err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatalf("Chmod(parent) error = %v", err)
	}

	stateDir := filepath.Join(parent, "run")
	if _, err := Listen(stateDir, "test-version"); !errors.Is(err, ErrInsecureStateDir) {
		t.Fatalf("Listen(insecure parent) error = %v, want ErrInsecureStateDir", err)
	}
	if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("Listen() created state directory under insecure parent (Lstat error = %v)", err)
	}
}

func TestSecurity_PreservesNonSocketPath(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	want := []byte("do not remove this file")
	if err := os.WriteFile(socketPath, want, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Listen(stateDir, "test-version")
	if !errors.Is(err, ErrSocketPathOccupied) {
		t.Fatalf("Listen() error = %v, want ErrSocketPathOccupied", err)
	}
	got, readErr := os.ReadFile(socketPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("socket-path file changed to %q, want %q", got, want)
	}
}

func TestListenRejectsInvalidVersionBeforeCreatingStateDir(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if _, err := Listen(stateDir, "dev\nversion"); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("Listen() error = %v, want ErrInvalidMessage", err)
	}
	if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("Listen() created state directory for invalid version (Lstat error = %v)", err)
	}
}

func TestListenRejectsOverlongSocketPathBeforeCreatingStateDir(t *testing.T) {
	requireLinux(t)
	parent := t.TempDir()
	stateDir := filepath.Join(parent, strings.Repeat("d", 90))
	if len(filepath.Join(stateDir, "scoprad.sock")) <= maxSocketPathBytes {
		t.Skip("temporary directory path is shorter than the Unix socket limit")
	}

	_, err := Listen(stateDir, "test-version")
	if !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("Listen() error = %v, want ErrSocketPathTooLong", err)
	}
	if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("Listen() created overlong state directory (Lstat error = %v)", err)
	}
}

func TestListenRejectsAlreadyRunningDaemon(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	first, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("first Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := Listen(stateDir, "test-version"); !errors.Is(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("second Listen() error = %v, want ErrDaemonAlreadyRunning", err)
	}
}

func TestSecurity_CloseDoesNotRemoveReplacedSocketPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("Remove(socket) error = %v", err)
	}
	want := []byte("replacement")
	if err := os.WriteFile(socketPath, want, 0o600); err != nil {
		t.Fatalf("WriteFile(replacement) error = %v", err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	got, err := os.ReadFile(socketPath)
	if err != nil {
		t.Fatalf("replacement path was removed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("replacement contents = %q, want %q", got, want)
	}
}

func TestSecurity_RemovesOnlyOwnedStaleSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatalf("Chmod(stale socket) error = %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() stale listener error = %v", err)
	}

	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() with stale socket error = %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
}

func TestListenAndServeStatusLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	stateInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("Stat(state dir) error = %v", err)
	}
	if got := stateInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("state directory mode = %#o, want 0700", got)
	}
	socketInfo, err := os.Lstat(filepath.Join(stateDir, "scoprad.sock"))
	if err != nil {
		t.Fatalf("Lstat(socket) error = %v", err)
	}
	if got := socketInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %#o, want 0600", got)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	defer func() {
		cancel()
		_ = daemon.Close()
	}()

	requestCtx, requestCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer requestCancel()
	response, err := Status(requestCtx, stateDir)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if response.Status != "running" || response.DaemonVersion != "test-version" {
		t.Fatalf("Status() response = %+v, want running test-version", response)
	}
	if response.ProtocolVersion != protocolVersion || len(response.RequestID) != 32 {
		t.Fatalf("Status() response has invalid protocol metadata: %+v", response)
	}
	if err := daemon.Serve(ctx); !errors.Is(err, ErrDaemonAlreadyServing) {
		t.Fatalf("second Serve() error = %v, want ErrDaemonAlreadyServing", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() after cancellation error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not stop after context cancellation")
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "scoprad.sock")); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after shutdown (Lstat error = %v)", err)
	}
}

func TestSecurity_RejectsMalformedCommandAndKeepsStatusService(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = daemon.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop")
		}
	})

	conn, err := net.DialTimeout("unix", filepath.Join(stateDir, "scoprad.sock"), time.Second)
	if err != nil {
		t.Fatalf("DialTimeout() error = %v", err)
	}
	if err := writeFrame(conn, []byte(`{"protocolVersion":1,"requestId":"0123456789abcdef0123456789abcdef","command":"execute"}`)); err != nil {
		t.Fatalf("writeFrame(invalid command) error = %v", err)
	}
	if _, err := readFrame(conn); err == nil {
		t.Fatal("server returned a response to an unsupported command")
	}
	_ = conn.Close()

	requestCtx, requestCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer requestCancel()
	response, err := Status(requestCtx, stateDir)
	if err != nil {
		t.Fatalf("Status() after malformed command error = %v", err)
	}
	if response.Status != "running" {
		t.Fatalf("Status() after malformed command = %q, want running", response.Status)
	}
}

func TestStatusStopsWaitingWhenContextIsCanceled(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	socketPath := filepath.Join(stateDir, socketName)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatalf("Chmod(socket) error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- connection
	}()

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := Status(ctx, stateDir)
		result <- err
	}()
	var serverConnection *net.UnixConn
	select {
	case serverConnection = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("AcceptUnix() error = %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Status() did not connect to the local socket")
	}
	defer serverConnection.Close()

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Status() after cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Status() did not stop promptly after context cancellation")
	}
}

func TestConcurrentStatusClients(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	defer func() {
		cancel()
		_ = daemon.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop")
		}
	}()

	const clientCount = 8
	testContext := t.Context()
	var clients sync.WaitGroup
	clientErrors := make(chan error, clientCount)
	for range clientCount {
		clients.Add(1)
		go func() {
			defer clients.Done()
			requestCtx, requestCancel := context.WithTimeout(testContext, 2*time.Second)
			defer requestCancel()
			response, err := Status(requestCtx, stateDir)
			if err == nil && response.Status != "running" {
				err = fmt.Errorf("status = %q, want %q", response.Status, "running")
			}
			clientErrors <- err
		}()
	}
	clients.Wait()
	close(clientErrors)
	for err := range clientErrors {
		if err != nil {
			t.Errorf("concurrent Status() error = %v", err)
		}
	}
}

func TestStatusDoesNotCreateMissingStateDirectory(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "missing", "run")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := Status(ctx, stateDir); !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("Status() error = %v, want ErrDaemonUnavailable", err)
	}
	if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("Status() created state directory (Lstat error = %v)", err)
	}
}

func TestStatusRejectsInsecureStateDirectory(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "insecure")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if _, err := Status(t.Context(), stateDir); !errors.Is(err, ErrInsecureStateDir) {
		t.Fatalf("Status() error = %v, want ErrInsecureStateDir", err)
	}
}

func TestSecurity_StatusRejectsInsecureSocket(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	socketPath := filepath.Join(stateDir, socketName)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	listener.SetUnlinkOnClose(false)
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()
	if err := os.Chmod(socketPath, 0o666); err != nil {
		t.Fatalf("Chmod(socket) error = %v", err)
	}

	if _, err := Status(t.Context(), stateDir); !errors.Is(err, ErrInsecureSocket) {
		t.Fatalf("Status() error = %v, want ErrInsecureSocket", err)
	}
}

func TestDefaultStateDirIsAbsolute(t *testing.T) {
	stateDir, err := DefaultStateDir()
	if err != nil {
		t.Fatalf("DefaultStateDir() error = %v", err)
	}
	if !filepath.IsAbs(stateDir) {
		t.Fatalf("DefaultStateDir() = %q, want an absolute path", stateDir)
	}
}

func TestResolveStateDirDefaultsToUserCacheDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CACHE_HOME selects the user cache directory on Linux")
	}
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	got, err := ResolveStateDir("")
	if err != nil {
		t.Fatalf("ResolveStateDir(\"\") error = %v", err)
	}
	if want := filepath.Join(cacheDir, "scopra", "run"); got != want {
		t.Fatalf("ResolveStateDir(\"\") = %q, want %q", got, want)
	}
}

func TestResolveStateDirReturnsCleanAbsolutePath(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	got, err := ResolveStateDir("nested/../run")
	if err != nil {
		t.Fatalf("ResolveStateDir(relative) error = %v", err)
	}
	if want := filepath.Join(workingDir, "run"); got != want {
		t.Fatalf("ResolveStateDir(relative) = %q, want %q", got, want)
	}
}

func TestCheckStateDirAcceptsPrivateDirectory(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}

	if err := CheckStateDir(stateDir); err != nil {
		t.Fatalf("CheckStateDir(private dir) error = %v, want nil", err)
	}
}

func TestSecurity_CheckStateDirDoesNotCreateMissingDirectory(t *testing.T) {
	requireLinux(t)
	parent := filepath.Join(t.TempDir(), "missing")
	stateDir := filepath.Join(parent, "run")

	if err := CheckStateDir(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckStateDir(missing dir) error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckStateDir() created part of the state directory (Lstat error = %v)", err)
	}
}

func TestSecurity_CheckStateDirRejectsInsecureDirectoryWithoutRepairingIt(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	if err := os.Chmod(stateDir, 0o750); err != nil {
		t.Fatalf("Chmod(state dir) error = %v", err)
	}

	if err := CheckStateDir(stateDir); !errors.Is(err, ErrInsecureStateDir) {
		t.Fatalf("CheckStateDir(group-readable dir) error = %v, want ErrInsecureStateDir", err)
	}
	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("Stat(state dir) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("state directory mode after CheckStateDir() = %#o, want unchanged 0750", got)
	}
}

func TestCheckStateDirRejectsOverlongSocketPath(t *testing.T) {
	requireLinux(t)
	stateDir := filepath.Join(t.TempDir(), strings.Repeat("d", 90))
	if len(filepath.Join(stateDir, socketName)) <= maxSocketPathBytes {
		t.Skip("temporary directory path is shorter than the Unix socket limit")
	}
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}

	if err := CheckStateDir(stateDir); !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("CheckStateDir(overlong dir) error = %v, want ErrSocketPathTooLong", err)
	}
}

func FuzzDecodeRequest(f *testing.F) {
	f.Add(validStatusRequest)
	f.Add(`{"protocolVersion":`)
	f.Add(`[]`)

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = decodeRequest([]byte(input))
	})
}

type limitedWriter struct {
	buffer   bytes.Buffer
	maxWrite int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.maxWrite == 0 {
		return 0, nil
	}
	if len(data) > w.maxWrite {
		data = data[:w.maxWrite]
	}
	return w.buffer.Write(data)
}

func frameHeader(length int) []byte {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(length))
	return header[:]
}

func TestResponseJSONContract(t *testing.T) {
	response := StatusResponse{
		ProtocolVersion: protocolVersion,
		RequestID:       "0123456789abcdef0123456789abcdef",
		Status:          "running",
		DaemonVersion:   "test-version",
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, key := range []string{"protocolVersion", "requestId", "status", "daemonVersion"} {
		if _, ok := got[key]; !ok {
			t.Errorf("response JSON missing field %q: %s", key, encoded)
		}
	}
}

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("peer credential support is Linux-only in this increment")
	}
}
