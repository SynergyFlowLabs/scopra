//go:build linux

package localipc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockStateDir takes Mio's single-daemon lock: a non-blocking exclusive flock
// on the state directory's lock file. The file is opened read-only, without
// following symlinks, blocking on FIFOs, or truncating, and Go opens it
// close-on-exec. Closing the returned file releases the lock.
func lockStateDir(stateDir string) (*os.File, error) {
	const flags = os.O_RDONLY | os.O_CREATE | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	file, err := os.OpenFile(filepath.Join(stateDir, lockFileName), flags, 0o600)
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EISDIR) {
		return nil, fmt.Errorf("%w: lock file must be a regular file", ErrInsecureStateDir)
	}
	if err != nil {
		return nil, fmt.Errorf("open state directory lock: %w", err)
	}
	if err := lockOpenedFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// lockOpenedFile validates the opened inode itself, avoiding a check-then-open
// race, and then takes the exclusive lock without waiting.
func lockOpenedFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect state directory lock: %w", err)
	}
	if err := validateLockFileInfo(info); err != nil {
		return err
	}

	rawFile, err := file.SyscallConn()
	if err != nil {
		return fmt.Errorf("access state directory lock: %w", err)
	}
	var lockErr error
	if err := rawFile.Control(func(fd uintptr) {
		lockErr = syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
	}); err != nil {
		return fmt.Errorf("access state directory lock: %w", err)
	}
	if errors.Is(lockErr, syscall.EWOULDBLOCK) {
		return fmt.Errorf("%w: another process holds the state directory lock",
			ErrDaemonAlreadyRunning)
	}
	if lockErr != nil {
		return fmt.Errorf("lock state directory: %w", lockErr)
	}
	return nil
}
