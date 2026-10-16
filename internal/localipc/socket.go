// Package localipc provides bounded, same-user Unix-socket communication for
// Scopra's local daemon and CLI. It currently exposes status only.
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

// Daemon is the foreground local status service. It intentionally exposes no
// execution or workspace-mutating operation.
type Daemon struct {
	listener      *net.UnixListener
	socketPath    string
	socketInfo    os.FileInfo
	daemonVersion string

	closeOnce sync.Once
	closeErr  error
	serveMu   sync.Mutex
	served    bool
}

// DefaultStateDir returns the per-user cache location used for Mio's local
// runtime socket. The returned directory is not created by this function.
func DefaultStateDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	if !filepath.IsAbs(cacheDir) {
		return "", fmt.Errorf("%w: user cache directory must be absolute", ErrInsecureStateDir)
	}
	return filepath.Join(cacheDir, "scopra", "run"), nil
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

	return &Daemon{
		listener:      listener,
		socketPath:    socketPath,
		socketInfo:    socketInfo,
		daemonVersion: daemonVersion,
	}, nil
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
		return d.Close()
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
				d.handleConnection(connection)
			}()
		default:
			_ = connection.Close()
		}
	}
}

// Close releases the listener and removes only the socket inode created by
// Listen. It is safe to call repeatedly and during Serve shutdown.
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
	return d.closeErr
}

// Status queries the read-only Mio status command over the authenticated local
// socket. It never falls back to an unauthenticated transport.
func Status(ctx context.Context, stateDir string) (StatusResponse, error) {
	if ctx == nil {
		return StatusResponse{}, ErrInvalidContext
	}
	if !platformSupported() {
		return StatusResponse{}, ErrUnsupportedPlatform
	}

	stateDir, err := resolveStateDir(stateDir)
	if err != nil {
		return StatusResponse{}, err
	}
	socketPath, err := socketPathFor(stateDir)
	if err != nil {
		return StatusResponse{}, err
	}
	if _, err := validateStateDir(stateDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StatusResponse{}, ErrDaemonUnavailable
		}
		return StatusResponse{}, err
	}
	socketInfo, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return StatusResponse{}, ErrDaemonUnavailable
	}
	if err != nil {
		return StatusResponse{}, fmt.Errorf("inspect daemon socket: %w", err)
	}
	if err := validateSocketInfo(socketInfo); err != nil {
		return StatusResponse{}, err
	}

	dialer := net.Dialer{Timeout: connectionTimeout}
	connection, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isStaleSocketProbeError(err) {
			return StatusResponse{}, ErrDaemonUnavailable
		}
		return StatusResponse{}, fmt.Errorf("connect to Mio: %w", err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return StatusResponse{}, ErrUnsupportedPlatform
	}
	defer unixConnection.Close()
	stopCancellation := context.AfterFunc(ctx, func() { _ = unixConnection.Close() })
	defer stopCancellation()

	peerUID, err := peerUID(unixConnection)
	if err != nil {
		return StatusResponse{}, contextualError(ctx, "authenticate Mio peer", err)
	}
	if err := authorizePeer(peerUID, effectiveUID()); err != nil {
		return StatusResponse{}, err
	}
	if err := setConnectionDeadline(ctx, unixConnection); err != nil {
		return StatusResponse{}, contextualError(ctx, "set IPC deadline", err)
	}

	requestID, err := newRequestID()
	if err != nil {
		return StatusResponse{}, err
	}
	encodedRequest, err := encodeRequest(request{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		Command:         statusCommand,
	})
	if err != nil {
		return StatusResponse{}, err
	}
	if err := writeFrame(unixConnection, encodedRequest); err != nil {
		return StatusResponse{}, contextualError(ctx, "write status request", err)
	}
	encodedResponse, err := readFrame(unixConnection)
	if err != nil {
		return StatusResponse{}, contextualError(ctx, "read status response", err)
	}
	response, err := decodeResponse(encodedResponse, requestID)
	if err != nil {
		return StatusResponse{}, err
	}
	return response, nil
}

func (d *Daemon) handleConnection(connection *net.UnixConn) {
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
	encodedResponse, err := encodeResponse(StatusResponse{
		ProtocolVersion: protocolVersion,
		RequestID:       message.RequestID,
		Status:          statusRunning,
		DaemonVersion:   d.daemonVersion,
	})
	if err != nil {
		return
	}
	_ = writeFrame(connection, encodedResponse)
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
