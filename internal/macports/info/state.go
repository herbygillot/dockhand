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
	// Distfiles and Checksums are port options rather than PortInfo
	// fields, read from the port's worker interpreter. Checksums keeps the
	// declared list's raw shape (type/value alternation, possibly
	// distfile-keyed); structure waits for a consumer.
	Distfiles []string
	Checksums []string
	Depends   Depends
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
