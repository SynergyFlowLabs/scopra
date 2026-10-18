//go:build linux

package identity

import (
	"os"
	"syscall"
)

func secureKeyFile(info os.FileInfo) bool    { return secureOwnerFile(info, 0o600) }
func secureConfigFile(info os.FileInfo) bool { return secureOwnerFile(info, 0o600) }

func secureOwnerFile(info os.FileInfo, mode os.FileMode) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != mode ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}

func ownedByCurrentUser(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
