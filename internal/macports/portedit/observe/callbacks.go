package observe

import (
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// A PortGroup callback whose host reads shape the build alone, read as a
// fact about the PortGroup at one version, as toolchainProbe rests on one
// about Base's compiler selection (#110, architecture's call of
// 2026-11-11): a closed read whose stack is the callback, with no
// Portfile frame on it, is explained where the Portfile reads the
// callback's option only in build positions. The next callback a survey
// turns up is a row and a reading, not new code; a version of the
// PortGroup not read here is unexplained.
type buildCallback struct {
	// Group and Version are the PortGroup, as its file names them
	// (java-1.0.tcl), and Callback the procedure it registers with
	// port::register_callback.
	Group, Version, Callback string
	// Option is what the callback writes that a Portfile may read.
	Option string
	// ReadAt is the macports-ports commit the fact was read at.
	ReadAt string
}

// buildCallbacks are the callbacks read so far. java's java_set_env, run
// as the PortGroup's callback (java-1.0.tcl:53), puts what find_java_home
// finds in configure.env, build.env, test.env, destroot.env and java.home
// (:211-219); find_java_home adds a depends_ line and sets
// java_version_not_found, which nothing outside the PortGroup reads, and
// inside it only pre-fetch and that procedure do.
var buildCallbacks = []buildCallback{
	{Group: "java", Version: "1.0", Callback: "java::java_set_env", Option: "java.home", ReadAt: "744df9d1"},
}

// callbackProbe is the callback a host read was made under, where it is
// one buildCallbacks names and no Portfile frame is on its stack.
func callbackProbe(declaration macports.Declaration) (buildCallback, bool) {
	run := false
	var group string
	for _, frame := range declaration.Frames {
		if strings.HasSuffix(frame.File, "/Portfile") {
			return buildCallback{}, false
		}
		if strings.Contains(frame.Command, "port::run_callbacks") {
			run = true
		}
		if dir, file := filepath.Split(frame.File); strings.HasSuffix(filepath.ToSlash(dir), "/"+macports.PortGroupDirectory+"/") && group == "" {
			group = strings.TrimSuffix(file, ".tcl")
		}
	}
	if !run || group == "" {
		return buildCallback{}, false
	}
	for _, callback := range buildCallbacks {
		if group == callback.Group+"-"+callback.Version {
			return callback, true
		}
	}
	return buildCallback{}, false
}

// optionReadsBenign reports whether a Portfile reads an option only in
// build positions, the test toolchainReadsBenign makes for the compiler's.
func optionReadsBenign(src []byte, option string) bool {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return false
	}
	read := func(name string) bool { return name == option }
	return classifyReads(src, script, &dimension{read: read, label: "the " + option + " a PortGroup callback finds on the host", tainted: map[string]bool{}}) == nil
}
