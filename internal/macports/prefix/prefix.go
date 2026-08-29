// Package prefix knows a MacPorts installation as an instance: the
// installed system rooted at a directory, as distinct from the ports
// tree and from MacPorts base the software. Several can coexist on one
// machine — the conventional /opt/local next to a test prefix — and an
// evaluator, a build, or a probe always runs against exactly one of
// them. Machine-side probes belong here: what is installed and where,
// and eventually what the installation says about itself (configured
// sources, version).
package prefix

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
)

// The binaries an installation provides under <prefix>/bin.
const (
	// TclShellName is the file name of MacPorts' Tcl shell.
	TclShellName = "port-tclsh"

	// CommandName is the file name of the port client.
	CommandName = "port"
)

// ErrNotInstalled reports that no MacPorts installation could be found.
var ErrNotInstalled = errors.New("prefix: no MacPorts installation found")

// Prefix is a MacPorts installation rooted at a directory. New and Find
// return validated installations; a directly-cast Prefix is just a path
// with the layout methods, which is all path construction needs.
type Prefix string

// PortTclsh returns the path of the installation's Tcl shell.
func (p Prefix) PortTclsh() string {
	return filepath.Join(string(p), "bin", TclShellName)
}

// Port returns the path of the installation's port client.
func (p Prefix) Port() string {
	return filepath.Join(string(p), "bin", CommandName)
}

// lookPath is indirected for hermetic tests.
var lookPath = exec.LookPath

// New returns the installation rooted at dir, validating that one
// actually lives there — any directory works, not just the conventional
// prefix. A stated prefix is never fallen back from.
func New(dir string) (Prefix, error) {
	p := Prefix(dir)
	if _, err := os.Stat(p.PortTclsh()); err != nil {
		return "", fmt.Errorf("%w (no %s under %s)", ErrNotInstalled, TclShellName, dir)
	}
	return p, nil
}

// Find discovers an installation: port-tclsh on PATH, whose location
// implies the prefix, else the conventional default prefix.
func Find() (Prefix, error) {
	return find(macports.DefaultPrefix)
}

func find(defaultPrefix string) (Prefix, error) {
	if path, err := lookPath(TclShellName); err == nil {
		return Prefix(filepath.Dir(filepath.Dir(path))), nil
	}
	if p, err := New(defaultPrefix); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%w (no %s on PATH or under %s)", ErrNotInstalled, TclShellName, defaultPrefix)
}
