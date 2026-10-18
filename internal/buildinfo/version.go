// Package buildinfo exposes safe build metadata to Scopra executables.
package buildinfo

import "runtime/debug"

const fallbackVersion = "dev"

// VersionOverride is stamped into release binaries with
// -ldflags "-X github.com/SynergyFlowLabs/scopra/internal/buildinfo.VersionOverride=vX".
// A checkout built by hand leaves it empty and reports its module version.
var VersionOverride string

// Version returns the main module version, or dev when the binary was built
// from an untagged checkout or without Go build metadata.
func Version() string {
	if validVersionToken(VersionOverride) {
		return VersionOverride
	}
	info, ok := debug.ReadBuildInfo()
	return versionFromBuildInfo(info, ok)
}

func versionFromBuildInfo(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return fallbackVersion
	}
	if !validVersionToken(info.Main.Version) {
		return fallbackVersion
	}
	return info.Main.Version
}

func validVersionToken(version string) bool {
	if version == "" || len(version) > 128 {
		return false
	}
	for index := 0; index < len(version); index++ {
		character := version[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}
