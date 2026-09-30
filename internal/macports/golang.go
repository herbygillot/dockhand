package macports

import "go/version"

// The Go PortGroup's go.toolchain_min gates a port on systems whose Go is
// too old. Which value is right depends on how the port builds, and the
// PortGroup's own guidance is followed here: with go.offline_build no the
// build runs in module mode, Go enforces go.mod's go directive, and the
// directive is exactly the minimum; in GOPATH mode the directive is only
// an upper bound on what the source needs, and says nothing of the
// minimum.

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
