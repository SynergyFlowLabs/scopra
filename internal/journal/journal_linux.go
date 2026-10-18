//go:build linux

package journal

import (
	"os"
	"syscall"
)

// platformSupported reports that the journal is available on Linux, where Mio
// runs.
func platformSupported() bool { return true }

// fileOwnedBy reports whether info belongs to the effective user.
func fileOwnedBy(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
