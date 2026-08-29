// Package macports holds core MacPorts facts: the names, paths, and defaults
// that MacPorts itself defines. Subpackages build machinery on these facts
// (tree resolves targets, eval evaluates them, base probes the
// installation); this package stays declarative.
//
// Everything here is a convention or a default, not a probe result. What is
// actually true of a given machine — where its prefix is, which tools are
// present — is discovered at runtime (see base), never assumed from these
// values.
package macports

const (
	// DefaultPrefix is the conventional MacPorts installation prefix.
	// Individual installations can be configured elsewhere, which is why
	// consumers take a prefix rather than reading this directly.
	DefaultPrefix = "/opt/local"

	// PortfileName is the file name of a port's definition within its
	// portdir.
	PortfileName = "Portfile"

	// PortGroupDir is the path of the PortGroup directory relative to
	// the root of a ports tree.
	PortGroupDir = "_resources/port1.0/group"

	// IndexFile is the file name of the generated PortIndex at the root
	// of a ports tree.
	IndexFile = "PortIndex"

	// IndexQuickFile is the file name of the PortIndex's lookup
	// accelerator alongside it.
	IndexQuickFile = "PortIndex.quick"

	// TclShellName is the file name of MacPorts' Tcl shell, installed
	// under an installation prefix's bin directory.
	TclShellName = "port-tclsh"

	// CommandName is the file name of the port client, alongside it.
	CommandName = "port"
)
