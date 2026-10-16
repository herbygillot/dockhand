package macports

import (
	"os/exec"
	"path/filepath"
)

// Facts about MacPorts that every layer shares.
const (
	// DefaultBaseVersion is the MacPorts Base release Dockhand installs and expects.
	DefaultBaseVersion = "2.12.6"
	// DefaultPrefix is where MacPorts installs on hosts and in guests.
	DefaultPrefix = "/opt/local"
	// TclShell is the MacPorts Tcl interpreter under the prefix.
	TclShell = "port-tclsh"
)

// FindTclsh is this Mac's MacPorts Tcl interpreter: port-tclsh on PATH,
// else under the default prefix; empty where there's neither. The
// evaluator, setup's report, and the comparison of an image's MacPorts
// with this Mac's all read Portfiles with it.
func FindTclsh() string {
	for _, name := range []string{TclShell, filepath.Join(DefaultPrefix, "bin", TclShell)} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
