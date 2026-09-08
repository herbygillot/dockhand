package info

// Semantic is what a port CLAIMS ABOUT ITSELF, and it is the only part
// of an evaluation a prediction is about.
//
// Every field here is compared, field by field, by a table this struct
// GENERATES. The `field` tag on each one is the MacPorts option name it
// answers to — the vocabulary a plan's wire form speaks and the one
// info.Field.String returns — and the generator under internal/gen reads
// these declarations and writes semantic_compare_gen.go beside them: the
// Field constants, their names, and the accessor table Diff walks. Run
// it with `go generate ./internal/macports/info` after touching this
// file; `make check` runs it and fails on a dirty diff.
//
// THE POINT OF GENERATING IT is that a field added here without a
// comparison stops being possible. The generated file ends in an
// UNKEYED composite literal of every type in this file, and an unkeyed
// literal must give a value for every field: add one and the package
// does not build until the generator has run and given the new field a
// Field constant and a row in the table. That is the whole mechanism —
// Go offers no exhaustiveness over struct fields, so the enforcement is
// generation plus a literal that cannot survive an omission, and not a
// promise in a comment (D8).
//
// Adding a field here is therefore a decision and not a convenience: it
// becomes something every intent's MayChange set must account for, and
// a field that legitimately moves for reasons no intent declares will
// refuse plans that are correct. Values' own doc comment states the
// question that decides which of the three parts a new field belongs in.
//
//go:generate go run ./internal/gen
type Semantic struct {
	Name     string `field:"name"`
	Version  string `field:"version"`
	Revision string `field:"revision"`
	Epoch    string `field:"epoch"`

	Categories []string `field:"categories"`
	// License holds the license field's top-level elements. An alternation
	// group ({LGPL-2.1 GPL-2}) arrives as one space-joined element; finer
	// modeling waits until something needs license semantics.
	License []string `field:"license"`
	// Maintainers holds the maintainers field's elements; a braced entry
	// ({@alice example.com:alice}) arrives as one element.
	Maintainers []string `field:"maintainers"`
	Platforms   []string `field:"platforms"`

	// Description, Homepage and LongDescription are prose a port states
	// about itself. They are compared like any other state: a bump that
	// silently rewrites a description did more than it was asked to.
	Description     string `field:"description"`
	Homepage        string `field:"homepage"`
	LongDescription string `field:"long_description"`

	// Distfiles and Checksums are port options rather than PortInfo
	// fields, read from the port's worker interpreter. Checksums keeps the
	// declared list's raw shape (type/value alternation, possibly
	// distfile-keyed); structure waits for a consumer.
	Distfiles []string `field:"distfiles"`
	Checksums []string `field:"checksums"`

	// Depends is a group: its own fields are leaves, and each becomes a
	// Field of its own, named for the tag here joined to the tag there
	// — depends_lib, and FieldDependsLib.
	Depends Depends `field:"depends"`
}

// Depends holds a context's dependency declarations, one list of depspecs
// ("port:zlib", "bin:git:git", "path:...") per phase. Depspecs stay raw
// strings until a consumer needs their structure.
//
// It is a group inside Semantic rather than seven fields spelled out
// there because MacPorts names them as a family and every consumer asks
// for one phase at a time. The generator flattens it: seven Fields,
// seven table rows, and one more unkeyed literal so that an eighth
// phase cannot be added without a comparison either.
type Depends struct {
	Fetch   []string `field:"fetch"`
	Extract []string `field:"extract"`
	Patch   []string `field:"patch"`
	Build   []string `field:"build"`
	Lib     []string `field:"lib"`
	Run     []string `field:"run"`
	Test    []string `field:"test"`
}
