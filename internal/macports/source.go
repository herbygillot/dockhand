package macports

// Facts about MacPorts and its ports repository that every layer shares.
const (
	// PortsRepository names the upstream ports repository on GitHub; PortsRepositoryURL is its clone URL.
	PortsRepository    = "macports/macports-ports"
	PortsRepositoryURL = "https://github.com/macports/macports-ports.git"
	PortsBranch        = "master"
	// PortsWorkflowPath is the ports repository's CI workflow, which runs on pushes to other branches.
	PortsWorkflowPath = ".github/workflows/main.yml"
	// DefaultBaseVersion is the MacPorts Base release Dockhand installs and expects.
	DefaultBaseVersion = "2.12.6"
	// DefaultPrefix is where MacPorts installs on hosts and in guests.
	DefaultPrefix = "/opt/local"
	// TclShell is the MacPorts Tcl interpreter under the prefix.
	TclShell = "port-tclsh"
	// TracURL is the MacPorts issue tracker.
	TracURL = "https://trac.macports.org"
)

// TicketURL is how a commit cites a Trac ticket: the full URL, which the
// ports repository's pull request template asks for.
func TicketURL(number string) string {
	return TracURL + "/ticket/" + number
}
