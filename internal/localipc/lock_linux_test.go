//go:build linux

package localipc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestSecurity_ListenRefusesSecondDaemonWhenSocketIsMissing(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "run")
	first, err := Listen(stateDir, "first")
	if err != nil {
		t.Fatalf("first Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	// A cache cleaner, or a racing start, can unlink the live daemon's socket.
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("Remove(socket) error = %v", err)
	}

	second, err := Listen(stateDir, "second")
	if err == nil {
		_ = second.Close()
		t.Fatal("second Listen() succeeded while the first daemon is still running")
	}
	if !errors.Is(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("second Listen() error = %v, want ErrDaemonAlreadyRunning", err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused Listen() touched the socket path (Lstat error = %v)", err)
	}
}

func TestSecurity_ConcurrentStartsAgainstStaleSocketYieldOneDaemon(t *testing.T) {
	const trials, starters = 100, 8
	for trial := range trials {
		stateDir := makeStateDir(t)
		makeStaleSocket(t, filepath.Join(stateDir, "scoprad.sock"))

		daemons, errs := startConcurrently(stateDir, starters)
		for _, daemon := range daemons {
			t.Cleanup(func() { _ = daemon.Close() })
		}
		if len(daemons) != 1 {
			t.Fatalf("trial %d: %d daemons started, want exactly 1 (errors: %v)", trial, len(daemons), errs)
		}
		for _, err := range errs {
			if !errors.Is(err, ErrDaemonAlreadyRunning) {
				t.Fatalf("trial %d: losing start error = %v, want ErrDaemonAlreadyRunning", trial, err)
			}
		}
		for _, daemon := range daemons {
			_ = daemon.Close()
		}
	}
}

func TestSecurity_ListenRefusesUnsafeLockFile(t *testing.T) {
	for _, occupant := range unsafeLockFiles {
		t.Run(occupant.name, func(t *testing.T) {
			stateDir := makeStateDir(t)
			verifyUnchanged := occupant.plant(t, filepath.Join(stateDir, "scoprad.lock"))

			daemon, err := listenWithin(t, stateDir, 5*time.Second)
			if err == nil {
				_ = daemon.Close()
				t.Fatal("Listen() succeeded with an unsafe lock file")
			}
			if !errors.Is(err, ErrInsecureStateDir) {
				t.Fatalf("Listen() error = %v, want ErrInsecureStateDir", err)
			}
			verifyUnchanged(t)
			if _, err := os.Lstat(filepath.Join(stateDir, "scoprad.sock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Listen() created a socket despite the unsafe lock file (Lstat error = %v)", err)
			}
		})
	}
}

func TestListenCreatesOwnerOnlyLockFileAndKeepsItAfterClose(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	lockPath := filepath.Join(stateDir, "scoprad.lock")
	requireOwnerOnlyRegularFile(t, lockPath)

	if err := daemon.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// Deleting the lock file would let one process lock the old inode while
	// another creates and locks a new file, so it must outlive the daemon.
	requireOwnerOnlyRegularFile(t, lockPath)
}

func TestSecurity_DaemonHoldsLockUntilInFlightRequestsFinish(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "run")
	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	served := make(chan error, 1)
	go func() { served <- daemon.Serve(context.Background()) }()

	socketAddress := &net.UnixAddr{Name: filepath.Join(stateDir, "scoprad.sock"), Net: "unix"}
	stalled, err := net.DialUnix("unix", nil, socketAddress)
	if err != nil {
		t.Fatalf("DialUnix() error = %v", err)
	}
	defer stalled.Close()
	// Mio accepts connections in order, so a completed status round trip on a
	// later connection proves the stalled connection already has a handler.
	if _, err := Status(t.Context(), stateDir); err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if err := daemon.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	second, err := Listen(stateDir, "second")
	if err == nil {
		_ = second.Close()
		t.Fatal("Listen() succeeded while the closing daemon was still serving a request")
	}
	if !errors.Is(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("Listen() during shutdown error = %v, want ErrDaemonAlreadyRunning", err)
	}

	_ = stalled.Close()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not return after the in-flight request ended")
	}
	next, err := Listen(stateDir, "next")
	if err != nil {
		t.Fatalf("Listen() after the daemon stopped serving error = %v", err)
	}
	_ = next.Close()
}

func TestListenReacquiresLockAfterClose(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "run")
	first, err := Listen(stateDir, "first")
	if err != nil {
		t.Fatalf("first Listen() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := Listen(stateDir, "second")
	if err != nil {
		t.Fatalf("Listen() after Close() error = %v, want the released lock to be reacquired", err)
	}
	_ = second.Close()
}

func TestListenReleasesLockWhenStartupFails(t *testing.T) {
	stateDir := makeStateDir(t)
	socketPath := filepath.Join(stateDir, "scoprad.sock")
	// A live listener that does not hold the lock (an unrelated process or a
	// daemon from before the lock existed) makes startup fail after locking.
	occupant, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	occupant.SetUnlinkOnClose(false)
	t.Cleanup(func() { _ = occupant.Close() })
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatalf("Chmod(socket) error = %v", err)
	}
	if _, err := Listen(stateDir, "test-version"); !errors.Is(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("Listen() with a live occupant error = %v, want ErrDaemonAlreadyRunning", err)
	}
	if err := occupant.Close(); err != nil {
		t.Fatalf("Close(occupant) error = %v", err)
	}

	daemon, err := Listen(stateDir, "test-version")
	if err != nil {
		t.Fatalf("Listen() after a failed start error = %v, want the lock to have been released", err)
	}
	_ = daemon.Close()
}

func TestSecurity_CheckStateDirRejectsUnsafeLockFile(t *testing.T) {
	for _, occupant := range unsafeLockFiles {
		t.Run(occupant.name, func(t *testing.T) {
			stateDir := makeStateDir(t)
			verifyUnchanged := occupant.plant(t, filepath.Join(stateDir, "scoprad.lock"))

			if err := CheckStateDir(stateDir); !errors.Is(err, ErrInsecureStateDir) {
				t.Fatalf("CheckStateDir() error = %v, want ErrInsecureStateDir", err)
			}
			verifyUnchanged(t)
		})
	}
}

func TestSecurity_CheckStateDirDoesNotCreateLockFile(t *testing.T) {
	stateDir := makeStateDir(t)

	if err := CheckStateDir(stateDir); err != nil {
		t.Fatalf("CheckStateDir(private dir) error = %v, want nil", err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "scoprad.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckStateDir() created the lock file (Lstat error = %v)", err)
	}
}

// unsafeLockFiles plants occupants that Mio must never accept as its lock
// file. Each plant returns a check that the occupant was left unchanged.
var unsafeLockFiles = []struct {
	name  string
	plant func(t *testing.T, lockPath string) (verifyUnchanged func(t *testing.T))
}{
	{
		name: "dangling symlink",
		plant: func(t *testing.T, lockPath string) func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "target")
			if err := os.Symlink(target, lockPath); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
			return func(t *testing.T) {
				if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("symlink target was created (Lstat error = %v)", err)
				}
			}
		},
	},
	{
		name: "group-readable file",
		plant: func(t *testing.T, lockPath string) func(t *testing.T) {
			if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if err := os.Chmod(lockPath, 0o640); err != nil {
				t.Fatalf("Chmod() error = %v", err)
			}
			return func(t *testing.T) {
				info, err := os.Lstat(lockPath)
				if err != nil || info.Mode().Perm() != 0o640 {
					t.Fatalf("lock file was modified (mode = %v, error = %v)", info, err)
				}
			}
		},
	},
	{
		name: "directory",
		plant: func(t *testing.T, lockPath string) func(t *testing.T) {
			if err := os.Mkdir(lockPath, 0o700); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			return func(t *testing.T) {
				if info, err := os.Lstat(lockPath); err != nil || !info.IsDir() {
					t.Fatalf("lock directory was modified (error = %v)", err)
				}
			}
		},
	},
	{
		name: "FIFO",
		plant: func(t *testing.T, lockPath string) func(t *testing.T) {
			if err := syscall.Mkfifo(lockPath, 0o600); err != nil {
				t.Fatalf("Mkfifo() error = %v", err)
			}
			return func(t *testing.T) {
				if info, err := os.Lstat(lockPath); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("lock FIFO was modified (error = %v)", err)
				}
			}
		},
	},
}

// startConcurrently releases starters calls to Listen at once. It returns the
// daemons that started and the errors from the starts that did not.
func startConcurrently(stateDir string, starters int) ([]*Daemon, []error) {
	var (
		mu      sync.Mutex
		daemons []*Daemon
		errs    []error
		wg      sync.WaitGroup
	)
	gate := make(chan struct{})
	for range starters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			daemon, err := Listen(stateDir, "test-version")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			daemons = append(daemons, daemon)
		}()
	}
	close(gate)
	wg.Wait()
	return daemons, errs
}

// listenWithin calls Listen but fails the test instead of hanging if Listen
// blocks, for example on a FIFO planted at the lock path.
func listenWithin(t *testing.T, stateDir string, timeout time.Duration) (*Daemon, error) {
	t.Helper()
	type result struct {
		daemon *Daemon
		err    error
	}
	done := make(chan result, 1)
	go func() {
		daemon, err := Listen(stateDir, "test-version")
		done <- result{daemon: daemon, err: err}
	}()
	select {
	case got := <-done:
		return got.daemon, got.err
	case <-time.After(timeout):
		t.Fatalf("Listen() did not return within %v", timeout)
		return nil, nil
	}
}

func makeStateDir(t *testing.T) string {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir(state dir) error = %v", err)
	}
	return stateDir
}

func makeStaleSocket(t *testing.T, socketPath string) {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatalf("Chmod(stale socket) error = %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("Close(stale listener) error = %v", err)
	}
}

func requireOwnerOnlyRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat(%s) error = %v", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !fileOwnedBy(info, effectiveUID()) {
		t.Fatalf("%s mode = %v, want an owner-only regular file owned by the current user",
			filepath.Base(path), info.Mode())
	}
}
