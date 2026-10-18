//go:build linux

package main

import (
	"os"
	"syscall"
)

// servicePlatformSupported reports that Linux has systemd user services.
func servicePlatformSupported() bool { return true }

// safeServiceAncestor reports whether info is a real directory that other
// users cannot write to, or a root-owned sticky directory such as /tmp.
func safeServiceAncestor(info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	if info.Mode().Perm()&0o022 == 0 {
		return true
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode()&os.ModeSticky != 0
}

// ownedServiceDir reports whether info is owned by the effective user.
func ownedServiceDir(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
