//go:build !linux

package workspace

func platformSupported() bool { return false }

func inspectRoot(string) (Fingerprint, error) { return Fingerprint{}, ErrUnsupportedPlatform }
