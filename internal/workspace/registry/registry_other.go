//go:build !linux

package registry

func checkDirectory(string) error { return ErrUnsupportedPlatform }

func readRegistryFile(string) ([]byte, bool, error) { return nil, false, ErrUnsupportedPlatform }

func syncDirectory(string) error { return ErrUnsupportedPlatform }
