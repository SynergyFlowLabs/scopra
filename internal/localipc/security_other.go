//go:build !linux

package localipc

import (
	"net"
	"os"
)

func platformSupported() bool { return false }

func effectiveUID() uint32 { return 0 }

func fileOwnedBy(os.FileInfo, uint32) bool { return false }

func peerUID(*net.UnixConn) (uint32, error) { return 0, ErrUnsupportedPlatform }

func isStaleSocketProbeError(error) bool { return false }

func prepareStateDir(string) (string, error) { return "", ErrUnsupportedPlatform }

func validateStateDir(string) (string, error) { return "", ErrUnsupportedPlatform }
