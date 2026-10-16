//go:build linux

package workspace

import (
	"fmt"
	"os"
	"syscall"
)

func platformSupported() bool { return true }

// inspectRoot checks that a canonical path names a directory owned by the
// effective user and returns its fingerprint. Lstat ensures that a symlink
// swapped in after resolution is reported rather than followed.
func inspectRoot(path string) (Fingerprint, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return Fingerprint{}, fmt.Errorf("%w: %q", ErrNotDirectory, path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Fingerprint{}, ErrUnsupportedPlatform
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return Fingerprint{}, fmt.Errorf("%w: %q", ErrNotOwned, path)
	}
	return Fingerprint{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}
