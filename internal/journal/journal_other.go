//go:build !linux

package journal

import "os"

// platformSupported reports that other platforms have no Mio socket and no
// run journal; every journal operation fails closed there.
func platformSupported() bool { return false }

func fileOwnedBy(os.FileInfo) bool { return false }
