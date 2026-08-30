package info

// Values is the evaluated metadata of one subport under one variant set.
type Values struct {
	Name       string
	Version    string
	Revision   string
	Epoch      string
	Categories []string
	// License holds the license field's top-level elements. An alternation
	// group ({LGPL-2.1 GPL-2}) arrives as one space-joined element; finer
	// modeling waits until something needs license semantics.
	License []string
	// Maintainers holds the maintainers field's elements; a braced entry
	// ({@alice example.com:alice}) arrives as one element.
	Maintainers []string
	Platforms   []string
	// Description, Homepage and LongDescription are prose a port states
	// about itself. They are compared like any other state: a bump that
	// silently rewrites a description did more than it was asked to.
	Description     string
	Homepage        string
	LongDescription string
	// Distfiles and Checksums are port options rather than PortInfo
	// fields, read from the port's worker interpreter. Checksums keeps the
	// declared list's raw shape (type/value alternation, possibly
	// distfile-keyed); structure waits for a consumer.
	Distfiles []string
	Checksums []string
	Depends   Depends

	// Worksrcdir, Filespath and Patchfiles describe what becomes of the
	// source once it is fetched: the directory it extracts into, where
	// the port keeps its patches, and which it applies. A planner that
	// must read a file out of a distfile needs the first to find it, and
	// the rest to know whether a patch rewrites that file before any
	// build sees it. Like Livecheck and Vendored they are configuration
	// rather than state, and stay out of fieldTable.
	Worksrcdir string
	Filespath  string
	Patchfiles []string

	// Livecheck and Vendored are configuration: what a Portfile
	// declares about how it is maintained, rather than what it is.
	// They ride along in the same evaluation because reading an option
	// off an already-open port costs microseconds — and they are
	// deliberately absent from fieldTable, so they never appear in a
	// Delta or count against an intent's acceptance. What a Values
	// holds is everything one evaluation yields; what fieldTable holds
	// is what counts as state to compare.
	Livecheck Livecheck
	Vendored  Vendored
}

// Livecheck is a port's declared update-checking configuration.
type Livecheck struct {
	Type    string
	URL     string
	Regex   string
	Version string
}

// Vendored holds the dependency blocks a generator owns, as text.
// dockhand owns the block boundary and nothing inside it (D6), so
// these stay opaque: their presence is a fact, their content is the
// generator's business.
type Vendored struct {
	GoVendors   string
	CargoCrates string
	// CargoCratesGithub is the cargo block's second form, for crates
	// taken from a git revision rather than the registry. Two ports in
	// the tree use it; it supplies distfiles the same way, but no
	// generator writes it, so an intent that regenerates cargo.crates
	// must still refuse a port carrying this.
	CargoCratesGithub string
}

// Any reports whether the port carries a vendored dependency block.
func (v Vendored) Any() bool {
	return v.GoVendors != "" || v.CargoCrates != "" || v.CargoCratesGithub != ""
}

// Depends holds a context's dependency declarations, one list of depspecs
// ("port:zlib", "bin:git:git", "path:...") per phase. Depspecs stay raw
// strings until a consumer needs their structure.
type Depends struct {
	Fetch   []string
	Extract []string
	Patch   []string
	Build   []string
	Lib     []string
	Run     []string
	Test    []string
}

// Snapshot is the evaluated state of one Portfile under one variant frame:
// metadata per evaluation context — the top-level port and each of its
// subports. Per D13, a snapshot is always total: every context the Portfile
// defines is present, or the snapshot's construction failed. The scope
// follows the scope of mutation — an edit touches one file, so one file's
// contexts are what fidelity must see whole. Nothing tree-scale is a
// Snapshot; relationships between ports are built from several of these,
// never measured as one.
type Snapshot map[SubportKey]Values
