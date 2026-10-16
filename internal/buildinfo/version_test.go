package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{name: "missing build info", ok: false, want: "dev"},
		{name: "nil build info", info: nil, ok: true, want: "dev"},
		{name: "empty version", info: &debug.BuildInfo{Main: debug.Module{}}, ok: true, want: "dev"},
		{name: "development version", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true, want: "dev"},
		{name: "invalid token", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0\\n"}}, ok: true, want: "dev"},
		{name: "valid token", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, ok: true, want: "v1.2.3"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := versionFromBuildInfo(test.info, test.ok); got != test.want {
				t.Fatalf("versionFromBuildInfo() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidVersionToken(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "development", version: "dev", want: true},
		{name: "tagged version", version: "v1.2.3", want: true},
		{name: "pseudo version", version: "v0.0.0-20261016-abcdef+meta", want: true},
		{name: "empty", version: "", want: false},
		{name: "backslash escape", version: "v1.0.0\\n", want: false},
		{name: "over limit", version: string(make([]byte, 129)), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validVersionToken(test.version); got != test.want {
				t.Fatalf("validVersionToken(%q) = %t, want %t", test.version, got, test.want)
			}
		})
	}
}

func TestVersionReturnsBoundedPrintableToken(t *testing.T) {
	version := Version()
	if version == "" || len(version) > 128 {
		t.Fatalf("Version() = %q, want a non-empty token of at most 128 bytes", version)
	}
	for index := 0; index < len(version); index++ {
		character := version[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		t.Fatalf("Version() contains non-token byte %q in %q", character, version)
	}
}
