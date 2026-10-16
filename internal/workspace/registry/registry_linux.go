//go:build linux

package registry

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// checkDirectory accepts only a real directory owned by the effective user
// with no group or other access.
func checkDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect registry directory: %w", err)
	}
	if !info.IsDir() || !ownerOnly(info) {
		return fmt.Errorf("%w: %q must be a directory owned by the current user "+
			"without group or other access", ErrInsecureRegistry, dir)
	}
	return nil
}

// readRegistryFile reads at most maxFileBytes of the registry file. It opens
// the file without following symlinks or blocking on FIFOs and checks the
// opened inode, avoiding a check-then-open race. found is false when the file
// does not exist.
func readRegistryFile(path string) (data []byte, found bool, err error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if errors.Is(err, syscall.ELOOP) {
		return nil, false, fmt.Errorf("%w: the registry file must not be a symlink",
			ErrInsecureRegistry)
	}
	if err != nil {
		return nil, false, fmt.Errorf("open registry file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("inspect registry file: %w", err)
	}
	if !info.Mode().IsRegular() || !ownerOnly(info) {
		return nil, false, fmt.Errorf("%w: the registry file must be a regular file owned by the "+
			"current user without group or other access", ErrInsecureRegistry)
	}
	data, err = io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read registry file: %w", err)
	}
	if len(data) > maxFileBytes {
		return nil, false, fmt.Errorf("%w: the file exceeds %d bytes",
			ErrCorruptRegistry, maxFileBytes)
	}
	return data, true, nil
}

// ownerOnly reports whether info is owned by the effective user and grants no
// group or other access.
func ownerOnly(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0o077 == 0
}

// syncDirectory makes a completed rename in dir durable.
func syncDirectory(dir string) error {
	directory, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open registry directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync registry directory: %w", err)
	}
	return nil
}
