//go:build !linux

package identity

import "os"

// Scopra's hosted machine identity is currently Linux-only. On other platforms
// it fails closed rather than weakening ownership or link-count checks.
func secureKeyFile(os.FileInfo) bool      { return false }
func secureConfigFile(os.FileInfo) bool   { return false }
func ownedByCurrentUser(os.FileInfo) bool { return false }
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
