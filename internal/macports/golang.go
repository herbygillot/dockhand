package macports

import (
	"go/version"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The Go PortGroup's go.toolchain_min gates a port on systems whose Go is
// too old. Which value is right depends on how the port builds, and the
// PortGroup's own guidance is followed here: with go.offline_build no the
// build runs in module mode, Go enforces go.mod's go directive, and the
// directive is exactly the minimum; in GOPATH mode the directive is only
// an upper bound on what the source needs, and says nothing of the
// minimum.

// GoDomains are the hosts the Go PortGroup's go.setup fetches from, each
// through its own PortGroup; the PortGroup's toolchain check is the same
// pre-fetch hook for every one. gopkg.in and golang.org/x name GitHub
// repositories and set go.domain github.com.
var GoDomains = []string{"github.com", "gitlab.com", "bitbucket.org", "git.sr.ht", "codeberg.org", "gitea.com"}

// GoModuleMode reports a Go PortGroup port that builds in module mode:
// go.offline_build set and false, read as Tcl reads a boolean. An unset or
// unreadable value is not module mode.
func (p PortInfo) GoModuleMode() bool {
	if _, set := p.Options["go.offline_build"]; !set || p.Options["go.package"] == "" {
		return false
	}
	offline, err := p.Bool("go.offline_build")
	return err == nil && !offline
}

// GoToolchainCovers reports whether a declared go.toolchain_min gates on
// what a go directive requires: it's of the required release's series or
// a later one. The Go PortGroup compares only the series, since MacPorts
// ships the newest patch release of each series it packages, so 1.26
// already gates on what go.mod's 1.26.8 asks, and a patch release moving
// within the series leaves the Portfile alone. A declared value Go can't
// read, or none, covers nothing.
func GoToolchainCovers(declared, required string) bool {
	series := version.Lang("go" + declared)
	return series != "" && version.Compare(series, version.Lang("go"+required)) >= 0
}

// goToolchainPort is one of the versioned Go toolchains go_toolchain
// builds, as it names their ports and commands, go-1.26, with its series.
var goToolchainPort = regexp.MustCompile(`^go-(1\.[0-9]+)$`)

// GoPin is the Go release series a port pins its build to, rather than
// the one the go port provides, and the declarations that name it.
type GoPin struct {
	// Series is the series, as Go writes it: 1.26.
	Series string
	// By are the declarations that name it, as the Portfile writes them:
	// go.bin, depends_build, or depends_lib.
	By []string
}

// Port is the pinned toolchain's port: go-1.26.
func (pin GoPin) Port() string { return "go-" + pin.Series }

// String is the pin as a person reads it, with what declares it:
// go-1.26 (go.bin, depends_build).
func (pin GoPin) String() string { return pin.Port() + " (" + strings.Join(pin.By, ", ") + ")" }

// Meets reports whether the pinned series provides what a go directive
// requires. It's compared by series, as a minimum is (GoToolchainCovers),
// since go-1.26 is the newest 1.26 release MacPorts packages.
func (pin GoPin) Meets(required string) bool { return GoToolchainCovers(pin.Series, required) }

// GoPinned is the Go series a port pins its build to: a go.bin naming one
// of the versioned toolchains, ${prefix}/bin/go-1.26 or the go in its
// GOROOT, ${prefix}/lib/go-1.26/bin/go, and a build or library dependency
// on its port, port:go-1.26. trivy pinned 1.26 with both, for a function
// 0.74.0 used that Go 1.27 replaced, and 0.75.0's go.mod required 1.27.0,
// which a build with the pin can't meet (the trivy run, #35083). A port
// can pin with the dependency alone, putting the toolchain on PATH, as
// vault and usql do. go.bin decides where it names a series, since the
// build runs it; otherwise the newest series a dependency names does.
// False where the port pins none, as one building with the go port's
// ${prefix}/bin/go doesn't; a go.bin the evaluation couldn't settle names
// none.
func (p PortInfo) GoPinned() (GoPin, bool) {
	var pin GoPin
	if bin, _, err := p.option("go.bin"); err == nil {
		if series, ok := goBinSeries(bin); ok {
			pin = GoPin{Series: series, By: []string{"go.bin"}}
		}
	}
	for _, phase := range []string{"build", "lib"} {
		declaration := "depends_" + phase
		for _, dependency := range p.Dependencies {
			m := goToolchainPort.FindStringSubmatch(dependency.Port)
			switch {
			case dependency.Phase != phase || m == nil:
			case m[1] == pin.Series:
				if !slices.Contains(pin.By, declaration) {
					pin.By = append(pin.By, declaration)
				}
			case pin.Series == "" || pin.By[0] != "go.bin" && version.Compare("go"+m[1], "go"+pin.Series) > 0:
				pin = GoPin{Series: m[1], By: []string{declaration}}
			}
		}
	}
	return pin, pin.Series != ""
}

// goBinSeries is the series a go.bin names, where it's one of the
// versioned toolchains: their command, ${prefix}/bin/go-1.26, or the go in
// their GOROOT, ${prefix}/lib/go-1.26/bin/go, as go_toolchain installs
// them.
func goBinSeries(bin string) (string, bool) {
	name := path.Base(bin)
	if name == "go" && path.Base(path.Dir(bin)) == "bin" {
		name = path.Base(path.Dir(path.Dir(bin)))
	}
	m := goToolchainPort.FindStringSubmatch(name)
	if m == nil {
		return "", false
	}
	return m[1], true
}
