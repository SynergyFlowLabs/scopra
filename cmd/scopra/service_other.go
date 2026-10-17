//go:build !linux

package main

import "os"

// servicePlatformSupported reports that other platforms have no systemd user
// services; the service commands fail closed there.
func servicePlatformSupported() bool { return false }

func safeServiceAncestor(os.FileInfo) bool { return false }

func ownedServiceDir(os.FileInfo) bool { return false }
