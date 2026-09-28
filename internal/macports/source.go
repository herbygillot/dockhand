package macports

// Facts about MacPorts that every layer shares.
const (
	// DefaultBaseVersion is the MacPorts Base release Dockhand installs and expects.
	DefaultBaseVersion = "2.12.6"
	// DefaultPrefix is where MacPorts installs on hosts and in guests.
	DefaultPrefix = "/opt/local"
	// TclShell is the MacPorts Tcl interpreter under the prefix.
	TclShell = "port-tclsh"
)
