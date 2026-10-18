package hollow

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// documentationSample is Podman's own `podman info --format json` example
// (see testdata/README.md): Podman 4.0.0, rootless, cgroups v2, and no
// delegated controllers.
func documentationSample(t testing.TB) []byte {
	t.Helper()
	sample, err := os.ReadFile("testdata/podman-info-v4.0.0-docs.json")
	if err != nil {
		t.Fatalf("read the sample: %v", err)
	}
	return sample
}

// variant returns the documentation sample with one field changed. path is a
// list of object keys; a nil value deletes the field.
func variant(t testing.TB, value any, path ...string) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(documentationSample(t), &document); err != nil {
		t.Fatalf("decode the sample: %v", err)
	}
	object := document
	for _, key := range path[:len(path)-1] {
		child, ok := object[key].(map[string]any)
		if !ok {
			t.Fatalf("the sample has no object at %q", key)
		}
		object = child
	}
	last := path[len(path)-1]
	if value == nil {
		delete(object, last)
	} else {
		object[last] = value
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode the variant: %v", err)
	}
	return encoded
}

var delegatedControllers = []any{"cpuset", "cpu", "io", "memory", "pids"}

func TestParsePodmanInfo_DocumentationSample(t *testing.T) {
	info, err := ParsePodmanInfo(documentationSample(t))
	if err != nil {
		t.Fatalf("ParsePodmanInfo() error = %v", err)
	}
	want := PodmanInfo{Version: "4.0.0", Rootless: true, CgroupVersion: "v2", CgroupControllers: []string{}}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("ParsePodmanInfo() = %+v, want %+v", info, want)
	}
	err = info.Check()
	if !errors.Is(err, ErrContainmentUnavailable) || !strings.Contains(err.Error(), "cpu, memory, pids") {
		t.Fatalf("Check() error = %v, want ErrContainmentUnavailable naming cpu, memory, pids", err)
	}
}

func TestParsePodmanInfo_AcceptsADelegatingHost(t *testing.T) {
	info, err := ParsePodmanInfo(variant(t, delegatedControllers, "host", "cgroupControllers"))
	if err != nil {
		t.Fatalf("ParsePodmanInfo() error = %v", err)
	}
	if err := info.Check(); err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
}

func TestPodmanInfo_Check(t *testing.T) {
	good := PodmanInfo{Version: "4.0.0", Rootless: true, CgroupVersion: "v2", CgroupControllers: []string{"cpuset", "cpu", "io", "memory", "pids"}}
	tests := []struct {
		name   string
		change func(*PodmanInfo)
		want   []string // empty: accepted
	}{
		{name: "minimum version", change: func(*PodmanInfo) {}},
		{name: "current version", change: func(i *PodmanInfo) { i.Version = "6.1.4" }},
		{name: "two-digit major version", change: func(i *PodmanInfo) { i.Version = "10.0.0" }},
		{name: "release candidate suffix", change: func(i *PodmanInfo) { i.Version = "5.2.0-rc1" }},
		{name: "development suffix", change: func(i *PodmanInfo) { i.Version = "4.9.4-dev" }},
		{name: "major and minor only", change: func(i *PodmanInfo) { i.Version = "4.0" }},
		{name: "exactly the needed controllers", change: func(i *PodmanInfo) { i.CgroupControllers = []string{"pids", "memory", "cpu"} }},
		{name: "Podman 3.4", change: func(i *PodmanInfo) { i.Version = "3.4.4" }, want: []string{`"3.4.4"`, "4.0"}},
		{name: "Podman 3.99", change: func(i *PodmanInfo) { i.Version = "3.99.0" }, want: []string{`"3.99.0"`}},
		{name: "empty version", change: func(i *PodmanInfo) { i.Version = "" }, want: []string{`""`}},
		{name: "leading v", change: func(i *PodmanInfo) { i.Version = "v4.0.0" }, want: []string{`"v4.0.0"`}},
		{name: "major only", change: func(i *PodmanInfo) { i.Version = "4" }, want: []string{`"4"`}},
		{name: "overlong major", change: func(i *PodmanInfo) { i.Version = "99999999999999999999.0.0" }, want: []string{"99999999999999999999"}},
		{name: "control characters in the version", change: func(i *PodmanInfo) { i.Version = "4.0.0\x1b[2J" }, want: []string{`\x1b[2J`}},
		{name: "root", change: func(i *PodmanInfo) { i.Rootless = false }, want: []string{"root"}},
		{name: "cgroups v1", change: func(i *PodmanInfo) { i.CgroupVersion = "v1" }, want: []string{`"v1"`}},
		{name: "no cpu controller", change: func(i *PodmanInfo) { i.CgroupControllers = []string{"memory", "pids"} }, want: []string{"not delegated: cpu"}},
		{name: "no memory controller", change: func(i *PodmanInfo) { i.CgroupControllers = []string{"cpu", "pids"} }, want: []string{"not delegated: memory"}},
		{name: "no pids controller", change: func(i *PodmanInfo) { i.CgroupControllers = []string{"cpu", "memory"} }, want: []string{"not delegated: pids"}},
		{name: "controller names are exact", change: func(i *PodmanInfo) { i.CgroupControllers = []string{"CPU", "memory ", "pids2"} }, want: []string{"cpu, memory, pids"}},
		{name: "everything wrong", change: func(i *PodmanInfo) { *i = PodmanInfo{Version: "3.4.4", CgroupVersion: "v1"} },
			want: []string{`"3.4.4"`, "root", `"v1"`, "cpu, memory, pids"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := good
			info.CgroupControllers = slices.Clone(good.CgroupControllers)
			test.change(&info)
			err := info.Check()
			if len(test.want) == 0 {
				if err != nil {
					t.Fatalf("Check() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrContainmentUnavailable) {
				t.Fatalf("Check() error = %v, want ErrContainmentUnavailable", err)
			}
			for _, want := range test.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Check() error = %q, want it to mention %s", err, want)
				}
			}
			if strings.ContainsRune(err.Error(), '\x1b') {
				t.Errorf("Check() error = %q contains a raw control character", err)
			}
		})
	}
}

// TestAtLeastVersion checks the comparison with a minimum whose minor
// version is not 0, as a future minimum such as 4.4 would have.
func TestAtLeastVersion(t *testing.T) {
	for version, want := range map[string]bool{
		"4.4.0": true, "4.4": true, "4.10.1": true, "5.0.0": true, "10.0": true, "4.4.0-rc1": true,
		"4.3.9": false, "4.3": false, "3.99.99": false, "4.x": false, "4.": false, ".4": false, "": false,
	} {
		if got := atLeastVersion(version, 4, 4); got != want {
			t.Errorf("atLeastVersion(%q, 4, 4) = %v, want %v", version, got, want)
		}
	}
}

func TestSecurity_ParsePodmanInfoFailsClosed(t *testing.T) {
	sample := documentationSample(t)
	tests := []struct {
		name   string
		output []byte
		want   string
	}{
		{name: "empty", output: nil, want: "JSON"},
		{name: "not JSON", output: []byte("Error: cannot connect to Podman"), want: "JSON"},
		{name: "null", output: []byte("null"), want: "host"},
		{name: "array", output: []byte("[]"), want: "JSON"},
		{name: "empty object", output: []byte("{}"), want: "host"},
		{name: "trailing data", output: append(append([]byte{}, sample...), []byte(" {}")...), want: "trailing"},
		{name: "oversized", output: append(append([]byte{}, sample...), make([]byte, 1<<20)...), want: "1 MiB"},
		{name: "no host", output: variant(t, nil, "host"), want: "host"},
		{name: "no security", output: variant(t, nil, "host", "security"), want: "host.security.rootless"},
		{name: "no rootless flag", output: variant(t, nil, "host", "security", "rootless"), want: "host.security.rootless"},
		{name: "rootless as a string", output: variant(t, "true", "host", "security", "rootless"), want: "JSON"},
		{name: "no cgroup version", output: variant(t, nil, "host", "cgroupVersion"), want: "host.cgroupVersion"},
		{name: "no controllers", output: variant(t, nil, "host", "cgroupControllers"), want: "host.cgroupControllers"},
		{name: "controllers as a string", output: variant(t, "cpu memory pids", "host", "cgroupControllers"), want: "JSON"},
		{name: "no version object", output: variant(t, nil, "version"), want: "version.Version"},
		{name: "no version string", output: variant(t, nil, "version", "Version"), want: "version.Version"},
		{name: "version as a number", output: variant(t, 4, "version", "Version"), want: "JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info, err := ParsePodmanInfo(test.output)
			if !errors.Is(err, ErrContainmentUnavailable) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParsePodmanInfo() error = %v, want ErrContainmentUnavailable mentioning %q", err, test.want)
			}
			if !reflect.DeepEqual(info, PodmanInfo{}) {
				t.Fatalf("ParsePodmanInfo() returned a partial value %+v", info)
			}
		})
	}
}

func FuzzParsePodmanInfo(f *testing.F) {
	f.Add(documentationSample(f))
	f.Add(variant(f, delegatedControllers, "host", "cgroupControllers"))
	f.Add([]byte(`{"host":{"cgroupVersion":"v2","cgroupControllers":["cpu","memory","pids"],"security":{"rootless":true}},"version":{"Version":"4.0.0"}}`))
	f.Fuzz(func(t *testing.T, output []byte) {
		info, err := ParsePodmanInfo(output)
		if err != nil {
			if !errors.Is(err, ErrContainmentUnavailable) {
				t.Fatalf("ParsePodmanInfo() error %v does not wrap ErrContainmentUnavailable", err)
			}
			return
		}
		if err := info.Check(); err != nil {
			if !errors.Is(err, ErrContainmentUnavailable) {
				t.Fatalf("Check() error %v does not wrap ErrContainmentUnavailable", err)
			}
			return
		}
		// An accepted host meets every requirement, whatever the input was.
		major, _, _ := strings.Cut(info.Version, ".")
		number, err := strconv.Atoi(major)
		if !info.Rootless || info.CgroupVersion != "v2" || major == "" || strings.Trim(major, "0123456789") != "" || err != nil || number < 4 {
			t.Fatalf("Check() accepted %+v", info)
		}
		for _, controller := range []string{"cpu", "memory", "pids"} {
			if !slices.Contains(info.CgroupControllers, controller) {
				t.Fatalf("Check() accepted %+v without the %s controller", info, controller)
			}
		}
	})
}

func TestDelegationMissing(t *testing.T) {
	tests := []struct {
		name string
		info PodmanInfo
		want bool
	}{
		{
			name: "delegation is the only gap",
			info: PodmanInfo{Version: "4.9.3", Rootless: true, CgroupVersion: "v2", CgroupControllers: []string{"memory", "pids"}},
			want: true,
		},
		{
			name: "everything is present",
			info: PodmanInfo{Version: "4.9.3", Rootless: true, CgroupVersion: "v2", CgroupControllers: []string{"cpu", "memory", "pids"}},
		},
		{
			name: "podman is too old",
			info: PodmanInfo{Version: "3.4.4", Rootless: true, CgroupVersion: "v2", CgroupControllers: []string{"memory"}},
		},
		{
			name: "podman runs as root",
			info: PodmanInfo{Version: "4.9.3", Rootless: false, CgroupVersion: "v2", CgroupControllers: []string{"memory"}},
		},
		{
			name: "cgroups are v1",
			info: PodmanInfo{Version: "4.9.3", Rootless: true, CgroupVersion: "v1", CgroupControllers: []string{"memory"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.info.DelegationMissing(); got != test.want {
				t.Fatalf("DelegationMissing() = %v, want %v", got, test.want)
			}
		})
	}
}
