//go:build !linux

package localipc

import "os"

func lockStateDir(string) (*os.File, error) { return nil, ErrUnsupportedPlatform }
