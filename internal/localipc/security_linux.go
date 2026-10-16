//go:build linux

package localipc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func platformSupported() bool { return true }

func effectiveUID() uint32 { return uint32(os.Geteuid()) }

func fileOwnedBy(info os.FileInfo, uid uint32) bool {
	if info == nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uid
}

func peerUID(connection *net.UnixConn) (uint32, error) {
	if connection == nil {
		return 0, ErrUnauthorizedPeer
	}
	rawConnection, err := connection.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("access Unix socket credentials: %w", err)
	}

	var uid uint32
	var socketErr error
	if err := rawConnection.Control(func(fd uintptr) {
		credentials, err := syscall.GetsockoptUcred(
			int(fd),
			syscall.SOL_SOCKET,
			syscall.SO_PEERCRED,
		)
		if err != nil {
			socketErr = err
			return
		}
		if credentials == nil {
			socketErr = ErrUnauthorizedPeer
			return
		}
		uid = credentials.Uid
	}); err != nil {
		return 0, fmt.Errorf("read Unix socket credentials: %w", err)
	}
	if socketErr != nil {
		return 0, fmt.Errorf("read Unix peer UID: %w", socketErr)
	}
	return uid, nil
}

func isStaleSocketProbeError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}

func prepareStateDir(path string) (string, error) {
	return secureStateDir(path, true)
}

func validateStateDir(path string) (string, error) {
	return secureStateDir(path, false)
}

func secureStateDir(path string, create bool) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve state directory: %w", err)
	}
	absolutePath = filepath.Clean(absolutePath)
	volume := filepath.VolumeName(absolutePath)
	root := volume + string(filepath.Separator)
	if absolutePath == root {
		return "", fmt.Errorf("%w: filesystem root cannot be a state directory", ErrInsecureStateDir)
	}

	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("%w: inspect filesystem root: %v", ErrInsecureStateDir, err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: filesystem root is not a real directory", ErrInsecureStateDir)
	}
	if err := validateSafeAncestor(rootInfo); err != nil {
		return "", err
	}

	parts := strings.Split(strings.TrimPrefix(absolutePath, root), string(filepath.Separator))
	current := root
	for index, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create {
			if mkdirErr := os.Mkdir(current, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return "", fmt.Errorf("create state directory: %w", mkdirErr)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			return "", fmt.Errorf("%w: inspect directory: %v", ErrInsecureStateDir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("%w: path component is not a real directory", ErrInsecureStateDir)
		}

		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return "", ErrUnsupportedPlatform
		}
		permissions := info.Mode().Perm()
		isFinalDirectory := index == len(parts)-1
		if isFinalDirectory {
			if owner.Uid != effectiveUID() || permissions != 0o700 {
				return "", fmt.Errorf("%w: final directory must be owned by the current user with mode 0700", ErrInsecureStateDir)
			}
			continue
		}
		if err := validateSafeAncestor(info); err != nil {
			return "", err
		}
	}
	return absolutePath, nil
}

func validateSafeAncestor(info os.FileInfo) error {
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrUnsupportedPlatform
	}
	if info.Mode().Perm()&0o022 != 0 && !(owner.Uid == 0 && info.Mode()&os.ModeSticky != 0) {
		return fmt.Errorf("%w: writable ancestor is not a root-owned sticky directory", ErrInsecureStateDir)
	}
	return nil
}
