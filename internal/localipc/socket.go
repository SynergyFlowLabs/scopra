// Package localipc provides bounded, same-user Unix-socket communication for
// Scopra's local daemon and CLI: Mio's status (ADR-003) and, once enabled, its
// workspace commands (ADR-006).
package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	socketName               = "scoprad.sock"
	maxSocketPathBytes       = 107
	maxConcurrentConnections = 16
	connectionTimeout        = 2 * time.Second
	staleSocketProbeTimeout  = 200 * time.Millisecond
)

var (
	// ErrInsecureStateDir reports a state directory with unsafe ownership or permissions.
	ErrInsecureStateDir = errors.New("localipc: insecure state directory")
	// ErrSocketPathOccupied reports a non-stale or unsafe occupant at the socket path.
	ErrSocketPathOccupied = errors.New("localipc: socket path is occupied")
	// ErrSocketPathTooLong reports a Unix socket path beyond the platform limit.
	ErrSocketPathTooLong = errors.New("localipc: Unix socket path exceeds platform limit")
	// ErrInsecureSocket reports a socket path with unsafe type, ownership, or permissions.
	ErrInsecureSocket = errors.New("localipc: insecure socket")
	// ErrDaemonAlreadyRunning reports an active listener at the configured socket path.
	ErrDaemonAlreadyRunning = errors.New("localipc: daemon is already running")
	// ErrDaemonUnavailable reports that no responsive daemon is available at the path.
	ErrDaemonUnavailable = errors.New("localipc: daemon is unavailable")
	// ErrUnauthorizedPeer reports a Unix peer whose UID does not match the local process.
	ErrUnauthorizedPeer = errors.New("localipc: peer UID does not match effective UID")
	// ErrUnsupportedPlatform reports that authenticated IPC is unavailable on the platform.
	ErrUnsupportedPlatform = errors.New("localipc: peer credentials are unsupported on this platform")
	// ErrInvalidContext reports a nil context argument.
	ErrInvalidContext = errors.New("localipc: context must not be nil")
	// ErrDaemonAlreadyServing reports a repeated call to Daemon.Serve.
	ErrDaemonAlreadyServing = errors.New("localipc: daemon Serve may only be called once")
)

// Daemon is Mio's foreground local IPC service. It serves status, workspace
// registration commands once EnableWorkspaces is called (ADR-006), and local
// execution.run once EnableExecutions is called (ADR-011). A Daemon holds an
// exclusive lock on its state directory until it can no longer act, so at
// most one Mio serves a state directory at a time.
type Daemon struct {
	listener      *net.UnixListener
	stateDir      string
	socketPath    string
	socketInfo    os.FileInfo
	daemonVersion string
	lock          *os.File
	workspaces    WorkspaceRegistry // nil until EnableWorkspaces
	executor      Executor          // nil until EnableExecutions
	// frameWriteTimeout bounds writing each reply frame of a run.
	frameWriteTimeout time.Duration
	runs              chan struct{} // one token per execution in progress

	closeOnce sync.Once
	closeErr  error
	serveMu   sync.Mutex
	served    bool
	lockOnce  sync.Once
	lockErr   error
}

// DefaultStateDir returns Mio's default state directory, which holds its socket
// and lock file and must survive restarts and cache cleaning. Following the XDG
// Base Directory specification, it is $XDG_STATE_HOME/scopra, or
// $HOME/.local/state/scopra when XDG_STATE_HOME is unset or empty. A relative
// location fails closed with ErrInsecureStateDir instead of being resolved
// against the working directory. The directory is not created.
func DefaultStateDir() (string, error) {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		if !filepath.IsAbs(stateHome) {
			return "", fmt.Errorf("%w: $XDG_STATE_HOME must be an absolute path", ErrInsecureStateDir)
		}
		return filepath.Join(stateHome, "scopra"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user state directory: %w", err)
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: home directory must be an absolute path", ErrInsecureStateDir)
	}
	return filepath.Join(home, ".local", "state", "scopra"), nil
}

// PlatformSupported reports whether authenticated local IPC is available on
// this operating system. Where it is not, Listen, Status, and CheckStateDir
// fail closed with ErrUnsupportedPlatform.
func PlatformSupported() bool { return platformSupported() }

// ResolveStateDir returns the absolute, cleaned state directory that Listen and
// Status use for stateDir, substituting DefaultStateDir when stateDir is empty.
// It neither inspects nor creates the directory.
func ResolveStateDir(stateDir string) (string, error) { return resolveStateDir(stateDir) }

// CheckStateDir applies Listen's state-directory checks without creating,
// modifying, removing, or locking anything. A nil error means the directory
// exists, is private to the effective user, has no unsafe ancestors, leaves
// room for Mio's socket path, and holds no unsafe lock file. An error matching
// os.ErrNotExist means the directory has not been created yet. CheckStateDir
// never opens the lock file, so it cannot make a concurrent daemon start fail.
func CheckStateDir(stateDir string) error {
	if !platformSupported() {
		return ErrUnsupportedPlatform
	}
	stateDir, err := resolveStateDir(stateDir)
	if err != nil {
		return err
	}
	if _, err := socketPathFor(stateDir); err != nil {
		return err
	}
	if _, err := validateStateDir(stateDir); err != nil {
		return err
	}
	return checkLockFile(stateDir)
}

// Listen creates a private Unix socket for a foreground Mio daemon.
// On Linux, both endpoints authenticate their peer with SO_PEERCRED.
func Listen(stateDir, daemonVersion string) (*Daemon, error) {
	if !platformSupported() {
		return nil, ErrUnsupportedPlatform
	}
	if !validDaemonVersion(daemonVersion) {
		return nil, fmt.Errorf("%w: invalid daemon version", ErrInvalidMessage)
	}

	stateDir, err := resolveStateDir(stateDir)
	if err != nil {
		return nil, err
	}
	socketPath, err := socketPathFor(stateDir)
	if err != nil {
		return nil, err
	}
	if _, err := prepareStateDir(stateDir); err != nil {
		return nil, err
	}
	// Lock before touching the socket so that stale-socket recovery cannot race
	// with another starting daemon.
	lock, err := lockStateDir(stateDir)
	if err != nil {
		return nil, err
	}
	started := false
	defer func() {
		if !started {
			_ = lock.Close()
		}
	}()
	if err := removeStaleSocket(socketPath); err != nil {
		return nil, err
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on local socket: %w", err)
	}
	listener.SetUnlinkOnClose(false)
	boundInfo, err := os.Lstat(socketPath)
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("inspect newly bound socket: %w", err)
	}
	if boundInfo.Mode()&os.ModeSocket == 0 || !fileOwnedBy(boundInfo, effectiveUID()) {
		_ = listener.Close()
		return nil, ErrInsecureSocket
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		removeSocketAfterFailedListen(listener, socketPath, boundInfo)
		return nil, fmt.Errorf("secure local socket permissions: %w", err)
	}

	socketInfo, err := os.Lstat(socketPath)
	if err != nil {
		removeSocketAfterFailedListen(listener, socketPath, boundInfo)
		return nil, fmt.Errorf("inspect local socket: %w", err)
	}
	if !os.SameFile(boundInfo, socketInfo) {
		removeSocketAfterFailedListen(listener, socketPath, boundInfo)
		return nil, ErrSocketPathOccupied
	}
	if err := validateSocketInfo(socketInfo); err != nil {
		removeSocketAfterFailedListen(listener, socketPath, boundInfo)
		return nil, err
	}

	started = true
	return &Daemon{
		listener:          listener,
		stateDir:          stateDir,
		socketPath:        socketPath,
		socketInfo:        socketInfo,
		daemonVersion:     daemonVersion,
		lock:              lock,
		frameWriteTimeout: defaultFrameWriteTimeout,
		runs:              make(chan struct{}, maxConcurrentRuns),
	}, nil
}

// StateDir returns the absolute state directory that the daemon serves and
// holds locked. The lock lasts until Close or, once Serve has started, until
// Serve returns, so a caller may keep single-writer state in this directory
// in the meantime.
func (d *Daemon) StateDir() string {
	if d == nil {
		return ""
	}
	return d.stateDir
}

// Serve accepts bounded, one-request status connections until ctx is canceled
// or the listener is closed. The socket is removed on return only if its inode
// is still the one created by Listen.
func (d *Daemon) Serve(ctx context.Context) (serveErr error) {
	if ctx == nil {
		return ErrInvalidContext
	}
	if d == nil || d.listener == nil {
		return fmt.Errorf("%w: daemon is not listening", ErrDaemonUnavailable)
	}

	d.serveMu.Lock()
	if d.served {
		d.serveMu.Unlock()
		return ErrDaemonAlreadyServing
	}
	d.served = true
	d.serveMu.Unlock()

	if ctx.Err() != nil {
		closeErr := d.Close()
		if err := d.releaseLock(); closeErr == nil {
			closeErr = err
		}
		return closeErr
	}

	closeWatch := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = d.Close()
		case <-closeWatch:
		}
	}()
	connections := make(chan struct{}, maxConcurrentConnections)
	var handlers sync.WaitGroup
	defer func() {
		close(closeWatch)
		if err := d.Close(); serveErr == nil && err != nil {
			serveErr = err
		}
		handlers.Wait()
		<-watcherDone
		// No request can still act on the state directory; hand it over.
		if err := d.releaseLock(); serveErr == nil && err != nil {
			serveErr = err
		}
	}()

	for {
		connection, err := d.listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept local IPC connection: %w", err)
		}
		if ctx.Err() != nil {
			_ = connection.Close()
			return nil
		}

		select {
		case connections <- struct{}{}:
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer func() { <-connections }()
				d.handleConnection(ctx, connection)
			}()
		default:
			_ = connection.Close()
		}
	}
}

// Close releases the listener and removes only the socket inode created by
// Listen. It is safe to call repeatedly and during Serve shutdown. If Serve
// was never called, Close also releases the state-directory lock; otherwise
// Serve releases it after in-flight requests finish.
func (d *Daemon) Close() error {
	if d == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		if d.listener != nil {
			if err := d.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				d.closeErr = fmt.Errorf("close local socket: %w", err)
			}
		}
		currentInfo, err := os.Lstat(d.socketPath)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			if d.closeErr == nil {
				d.closeErr = fmt.Errorf("inspect local socket during shutdown: %w", err)
			}
			return
		}
		if !os.SameFile(d.socketInfo, currentInfo) {
			return
		}
		if err := os.Remove(d.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) && d.closeErr == nil {
			d.closeErr = fmt.Errorf("remove local socket: %w", err)
		}
	})
	if d.serveStarted() {
		return d.closeErr
	}
	if err := d.releaseLock(); err != nil && d.closeErr == nil {
		return err
	}
	return d.closeErr
}

func (d *Daemon) serveStarted() bool {
	d.serveMu.Lock()
	defer d.serveMu.Unlock()
	return d.served
}

// Status queries the read-only Mio status command over the authenticated local
// socket. It never falls back to an unauthenticated transport.
func Status(ctx context.Context, stateDir string) (StatusResponse, error) {
	encoded, requestID, err := roundTrip(ctx, stateDir, request{Command: statusCommand}, "status")
	if err != nil {
		return StatusResponse{}, err
	}
	return decodeResponse(encoded, requestID)
}

// roundTrip sends one request to Mio over the socket in stateDir and returns
// the encoded reply with the request ID that the reply must carry. The request
// is validated before the filesystem or the socket is touched.
func roundTrip(ctx context.Context, stateDir string, message request, operation string) ([]byte, string, error) {
	if ctx == nil {
		return nil, "", ErrInvalidContext
	}
	if !platformSupported() {
		return nil, "", ErrUnsupportedPlatform
	}
	requestID, err := newRequestID()
	if err != nil {
		return nil, "", err
	}
	message.ProtocolVersion = protocolVersion
	message.RequestID = requestID
	encodedRequest, err := encodeRequest(message)
	if err != nil {
		return nil, "", err
	}

	unixConnection, err := dialMio(ctx, stateDir)
	if err != nil {
		return nil, "", err
	}
	defer unixConnection.Close()
	stopCancellation := context.AfterFunc(ctx, func() { _ = unixConnection.Close() })
	defer stopCancellation()

	if err := setConnectionDeadline(ctx, unixConnection); err != nil {
		return nil, "", contextualError(ctx, "set IPC deadline", err)
	}
	if err := writeFrame(unixConnection, encodedRequest); err != nil {
		return nil, "", contextualError(ctx, "write "+operation+" request", err)
	}
	encodedResponse, err := readFrame(unixConnection)
	if err != nil {
		return nil, "", contextualError(ctx, "read "+operation+" response", err)
	}
	return encodedResponse, requestID, nil
}

// dialMio connects to Mio's socket in stateDir after checking the state
// directory and the socket, and returns the connection once the peer is
// authenticated as the effective UID. It never falls back to an
// unauthenticated transport.
func dialMio(ctx context.Context, stateDir string) (*net.UnixConn, error) {
	stateDir, err := resolveStateDir(stateDir)
	if err != nil {
		return nil, err
	}
	socketPath, err := socketPathFor(stateDir)
	if err != nil {
		return nil, err
	}
	if _, err := validateStateDir(stateDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrDaemonUnavailable
		}
		return nil, err
	}
	socketInfo, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrDaemonUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("inspect daemon socket: %w", err)
	}
	if err := validateSocketInfo(socketInfo); err != nil {
		return nil, err
	}

	dialer := net.Dialer{Timeout: connectionTimeout}
	connection, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isStaleSocketProbeError(err) {
			return nil, ErrDaemonUnavailable
		}
		return nil, fmt.Errorf("connect to Mio: %w", err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return nil, ErrUnsupportedPlatform
	}
	peerUID, err := peerUID(unixConnection)
	if err != nil {
		_ = unixConnection.Close()
		return nil, contextualError(ctx, "authenticate Mio peer", err)
	}
	if err := authorizePeer(peerUID, effectiveUID()); err != nil {
		_ = unixConnection.Close()
		return nil, err
	}
	return unixConnection, nil
}

// handleConnection answers the one request on connection. Only reading the
// request is bounded by connectionTimeout; an execution.run reply then lasts
// until the run ends, and ctx, which ends with Serve, cancels the run.
func (d *Daemon) handleConnection(ctx context.Context, connection *net.UnixConn) {
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(connectionTimeout)); err != nil {
		return
	}
	peerUID, err := peerUID(connection)
	if err != nil || authorizePeer(peerUID, effectiveUID()) != nil {
		return
	}
	encodedRequest, err := readFrame(connection)
	if err != nil {
		return
	}
	message, err := decodeRequest(encodedRequest)
	if err != nil {
		return
	}
	if message.Command == runCommand {
		d.serveRun(ctx, connection, message, peerUID)
		return
	}
	encodedResponse, err := d.reply(message)
	if err != nil {
		return
	}
	_ = writeFrame(connection, encodedResponse)
}

// reply returns the encoded reply to a decoded request, or an error when the
// request must go unanswered.
func (d *Daemon) reply(message request) ([]byte, error) {
	if message.Command == statusCommand {
		return encodeResponse(StatusResponse{
			ProtocolVersion: protocolVersion,
			RequestID:       message.RequestID,
			Status:          statusRunning,
			DaemonVersion:   d.daemonVersion,
		})
	}
	if d.workspaces == nil {
		return nil, fmt.Errorf("%w: workspace commands are not enabled", ErrInvalidMessage)
	}
	return d.replyToWorkspaceCommand(message)
}

func removeStaleSocket(path string) error {
	oldInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing socket path: %w", err)
	}
	if err := validateSocketInfo(oldInfo); err != nil {
		return ErrSocketPathOccupied
	}

	connection, probeErr := net.DialTimeout("unix", path, staleSocketProbeTimeout)
	if probeErr == nil {
		_ = connection.Close()
		return ErrDaemonAlreadyRunning
	}
	if !isStaleSocketProbeError(probeErr) {
		return fmt.Errorf("%w: cannot verify existing path", ErrSocketPathOccupied)
	}

	currentInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recheck stale socket path: %w", err)
	}
	if !os.SameFile(oldInfo, currentInfo) {
		return fmt.Errorf("%w: socket inode changed during stale check", ErrSocketPathOccupied)
	}
	if err := validateSocketInfo(currentInfo); err != nil {
		return ErrSocketPathOccupied
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

func validateSocketInfo(info os.FileInfo) error {
	if info == nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 || !fileOwnedBy(info, effectiveUID()) {
		return ErrInsecureSocket
	}
	return nil
}

func resolveStateDir(stateDir string) (string, error) {
	if stateDir == "" {
		var err error
		stateDir, err = DefaultStateDir()
		if err != nil {
			return "", err
		}
	}
	absolutePath, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("resolve state directory: %w", err)
	}
	return filepath.Clean(absolutePath), nil
}

func socketPathFor(stateDir string) (string, error) {
	path := filepath.Join(stateDir, socketName)
	if len([]byte(path)) > maxSocketPathBytes {
		return "", ErrSocketPathTooLong
	}
	return path, nil
}

func setConnectionDeadline(ctx context.Context, connection *net.UnixConn) error {
	deadline := time.Now().Add(connectionTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	return connection.SetDeadline(deadline)
}

func contextualError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return fmt.Errorf("%s: %w", operation, contextErr)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func authorizePeer(peerUID, expectedUID uint32) error {
	if peerUID != expectedUID {
		return ErrUnauthorizedPeer
	}
	return nil
}

func removeSocketAfterFailedListen(listener *net.UnixListener, path string, expectedInfo os.FileInfo) {
	if listener != nil {
		_ = listener.Close()
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || expectedInfo == nil || !os.SameFile(expectedInfo, currentInfo) {
		return
	}
	_ = os.Remove(path)
}
