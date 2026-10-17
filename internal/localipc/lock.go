package localipc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// lockFileName is Mio's single-daemon lock inside the state directory. The
// file is never deleted: unlinking it would let one process lock the old inode
// while another creates and locks a new file.
const lockFileName = "scoprad.lock"

// validateLockFileInfo accepts only a regular file owned by the effective user
// with no group or other access. Given Lstat results, it also rejects symlinks.
func validateLockFileInfo(info os.FileInfo) error {
	mode := info.Mode()
	if !mode.IsRegular() || mode.Perm()&0o077 != 0 || !fileOwnedBy(info, effectiveUID()) {
		return fmt.Errorf("%w: lock file must be a regular file owned by the current user "+
			"without group or other access", ErrInsecureStateDir)
	}
	return nil
}

// checkLockFile validates an existing lock file without creating, opening, or
// locking it, so read-only diagnostics cannot make a concurrent start fail.
func checkLockFile(stateDir string) error {
	info, err := os.Lstat(filepath.Join(stateDir, lockFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: inspect lock file: %v", ErrInsecureStateDir, err)
	}
	return validateLockFileInfo(info)
}

// releaseLock drops the state-directory lock once the daemon can no longer act
// on the directory. It is safe to call more than once.
func (d *Daemon) releaseLock() error {
	d.lockOnce.Do(func() {
		if d.lock == nil {
			return
		}
		if err := d.lock.Close(); err != nil {
			d.lockErr = fmt.Errorf("release state directory lock: %w", err)
		}
	})
	return d.lockErr
}
