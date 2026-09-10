// Package macports holds core MacPorts facts: the names, paths, and
// defaults that MacPorts itself defines, and VerCmp — base's own
// version ordering, a fact of MacPorts rather than of any machine.
// Subpackages build machinery on these facts (tree resolves targets,
// eval evaluates them, prefix probes the installation).
//
// Nothing here is a probe result. What is actually true of a given
// machine — where its prefix is, which tools are present — is
// discovered at runtime (see prefix), never assumed from these values.
package macports

const (
	// DefaultPrefix is the conventional MacPorts installation prefix.
	// Individual installations can be configured elsewhere, which is why
	// consumers take a prefix rather than reading this directly.
	DefaultPrefix = "/opt/local"

	// ResourcesDir is the ports tree directory MacPorts reads its own
	// configuration out of — archive sites, mirror sites, port groups.
	// It is a fact about a TREE's layout, which is why it sits beside
	// PortfileName: two roads need it for reasons that have nothing to
	// do with each other. A staged tree without it can reach no binary
	// archive, because portarchivefetch resolves archive_sites.tcl under
	// the port's own tree with the fallback DISABLED. A shadow without
	// it silently resolves port groups against the installation's
	// default tree instead, because getportresourcepath's fallback is
	// enabled — the same absence, one loud and one quiet.
	ResourcesDir = "_resources"

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

	// CommandName is the file name of the port client, in an
	// installation prefix's bin directory beside port-tclsh — which is
	// named once, as tool.PortTclsh, because dockhand also drives it.
	CommandName = "port"

	// IndexCommandName is the name of the tool that builds a tree's
	// PortIndex, in the installation's bin directory.
	IndexCommandName = "portindex"

	// SourcesConfPath is the path, relative to an installation's
	// prefix, of the file listing the ports trees it reads.
	SourcesConfPath = "etc/macports/sources.conf"
)
