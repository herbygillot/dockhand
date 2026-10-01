package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// go.offline_build is read as Tcl reads a boolean: false in any spelling is
// module mode; true, unset, or not a boolean isn't.
func TestModuleModeReadsOfflineBuildAsTclBoolean(t *testing.T) {
	t.Parallel()
	for value, module := range map[string]bool{"no": true, "No": true, "off": true, "0": true, "yes": false, "true": false, "maybe": false} {
		info := PortInfo{Options: map[string]string{"go.package": "example.com/fixture", "go.offline_build": value}}
		require.Equal(t, module, info.GoModuleMode(), value)
	}
	require.False(t, PortInfo{Options: map[string]string{"go.package": "example.com/fixture"}}.GoModuleMode())
	require.False(t, PortInfo{Options: map[string]string{"go.offline_build": "no"}}.GoModuleMode(), "not a Go PortGroup port")
}

// A minimum gates on a requirement of its series or an earlier one, as the
// Go PortGroup compares them; none, or one Go can't read, gates on
// nothing.
func TestAGoMinimumCoversItsSeries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		declared, required string
		covers             bool
	}{
		{"1.26", "1.26.8", true}, {"1.26.8", "1.26", true}, {"1.27", "1.26.8", true}, {"1.25", "1.26", false},
		{"", "1.24", false}, {"latest", "1.24", false},
	} {
		require.Equal(t, test.covers, GoToolchainCovers(test.declared, test.required), test.declared+" "+test.required)
	}
}

// A pin is go.bin naming one of the versioned toolchains, by their command
// or the go in their GOROOT, and a build or library dependency on its
// port: trivy's go-1.26 by both (the trivy run, #35083), vault's by its
// dependency alone. go.bin decides where it names a series, since the
// build runs it; otherwise the newest a dependency names does. The go
// port's ${prefix}/bin/go, a dependency on go itself, go-devel, and a
// go.bin the evaluation couldn't settle pin nothing.
func TestAGoPinIsWhatGoBinAndTheDependenciesName(t *testing.T) {
	t.Parallel()
	dependency := func(phase, port string) Dependency {
		return Dependency{Port: port, Phase: phase, Spec: "port:" + port}
	}
	for _, test := range []struct {
		name         string
		bin          string
		dependencies []Dependency
		want         GoPin
		pinned       bool
	}{
		{"trivy's", "/opt/local/bin/go-1.26", []Dependency{dependency("build", "go-1.26")}, GoPin{Series: "1.26", By: []string{"go.bin", "depends_build"}}, true},
		{"the GOROOT's go", "/opt/local/lib/go-1.26/bin/go", nil, GoPin{Series: "1.26", By: []string{"go.bin"}}, true},
		{"vault's, by its dependency alone", "/opt/local/bin/go", []Dependency{dependency("build", "go-1.26")}, GoPin{Series: "1.26", By: []string{"depends_build"}}, true},
		{"usql's, beside the go port", "/opt/local/bin/go", []Dependency{dependency("build", "go"), dependency("build", "go-1.26")}, GoPin{Series: "1.26", By: []string{"depends_build"}}, true},
		{"by a library dependency", "", []Dependency{dependency("lib", "go-1.25")}, GoPin{Series: "1.25", By: []string{"depends_lib"}}, true},
		{"go.bin over another dependency", "/opt/local/bin/go-1.26", []Dependency{dependency("build", "go-1.27")}, GoPin{Series: "1.26", By: []string{"go.bin"}}, true},
		{"the newest of the dependencies", "", []Dependency{dependency("build", "go-1.25"), dependency("lib", "go-1.26")}, GoPin{Series: "1.26", By: []string{"depends_lib"}}, true},
		{"the go port's", "/opt/local/bin/go", []Dependency{dependency("build", "go")}, GoPin{}, false},
		{"a run dependency", "/opt/local/bin/go", []Dependency{dependency("run", "go-1.26")}, GoPin{}, false},
		{"go-devel", "/opt/local/bin/go-devel", []Dependency{dependency("build", "go-devel")}, GoPin{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			info := PortInfo{Options: map[string]string{}, Dependencies: test.dependencies}
			if test.bin != "" {
				info.Options["go.bin"] = test.bin
			}
			pin, pinned := info.GoPinned()
			require.Equal(t, test.pinned, pinned)
			require.Equal(t, test.want, pin)
		})
	}
	unsettled := PortInfo{Options: map[string]string{}, OptionErrors: map[string]string{"go.bin": `can't read "prefix"`}}
	_, pinned := unsettled.GoPinned()
	require.False(t, pinned)
}

// A pin meets a requirement of its series, whatever the patch release, as
// a minimum does, and is said by its port and what declares it.
func TestAGoPinMeetsItsSeries(t *testing.T) {
	t.Parallel()
	pin := GoPin{Series: "1.26", By: []string{"go.bin", "depends_build"}}
	require.True(t, pin.Meets("1.26.8"))
	require.True(t, pin.Meets("1.25"))
	require.False(t, pin.Meets("1.27.0"))
	require.Equal(t, "go-1.26 (go.bin, depends_build)", pin.String())
}
