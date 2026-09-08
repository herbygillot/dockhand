package info

// Values is the evaluated metadata of one subport under one variant set:
// everything one evaluation yields, in three parts that differ by WHAT
// KIND OF FACT they are.
//
// THE RULE THAT DECIDES WHICH PART A FIELD BELONGS IN is a single
// question — what would it MEAN for this field to differ between two
// evaluations?
//
//   - Semantic: a difference IS the change. These are what the port
//     claims about itself, and they are the fields a prediction is
//     about. Every one of them is compared, by a table generated from
//     the struct, so "is this tracked?" is answered by which struct the
//     field is declared in and never by reading a table to see whether
//     someone remembered it.
//   - Context: a difference is the EVALUATION'S OWN ADDRESS and not the
//     port's. A shadow is a copy of a portdir in a temporary directory,
//     so a field that names where the evaluation ran differs between a
//     portdir and its shadow always, and legitimately. Comparing these
//     would make every shadow prediction fail on a fact about temporary
//     directories.
//   - Observed: a difference is real, and nothing predicts it.
//     Configuration a Portfile declares about how it is built or
//     maintained, read because reading an option off an already-open
//     port costs microseconds, recorded because planners use it, and
//     never compared because it moves for reasons no intent declares.
//
// The three are EMBEDDED rather than named, so a caller still writes
// vals.Version and vals.Vendored and never has to know which part a
// field is in to read one — and, MEASURED on the toolchain this module
// declares (go 1.27), still writes info.Values{Version: "1.0"} to build
// one: promoted fields became legal keys in composite literals in that
// language version, which is why splitting a struct every package in
// the tree constructs cost no call site at all. Both spellings work;
// info.Values{Semantic: info.Semantic{...}} is the one that says which
// kind of fact is being asserted, and is worth preferring where the
// answer is the point.
//
// The split exists because the alternative had no answer. The
// comparison used to be a hand-kept table beside a flat struct, and
// whether a field was tracked was a property of whether anyone had
// remembered to add a row — while three groups of fields were
// deliberately left out of it with a comment saying so. Now the
// omission is a build failure and the exclusions are a type (D8).
type Values struct {
	Semantic
	Context
	Observed
}

// Context is what was true AROUND an evaluation rather than about the
// port: the frame it ran in.
//
// Nothing here is compared, and the reason is measurable rather than a
// matter of taste. MacPorts derives filespath from the port's own
// directory — base's portmain.tcl says
// `default filespath {[file join $portpath [join $filesdir]]}` — so it
// is an absolute path into the portdir being evaluated. Every
// prediction in dockhand is made by shadowing a Portfile into a
// temporary directory and evaluating the copy, which means this field
// differs between the two sides of every single diff, always, for a
// reason that has nothing to do with the change. A field like that in
// Semantic would not catch a bug; it would refuse every plan.
type Context struct {
	// Filespath is where the port keeps its patches and auxiliary files.
	// A planner that must read a patch out of the port's own directory
	// needs it, and reads it off the REAL evaluation rather than a
	// shadow's — a shadow's answer is a temporary directory that will
	// not exist by the time anything acts on it.
	Filespath string
}

// Observed is what an evaluation SAW alongside the state: configuration
// a Portfile declares about how it is built or maintained.
//
// It is recorded and never compared. Each field here would refuse
// correct plans if it were Semantic, and the reasons are specific:
//
//   - Worksrcdir defaults to $distname, which defaults to
//     ${name}-${version} (base's portmain.tcl), so it moves on EVERY
//     version bump — a bump would have to declare it in MayChange to
//     survive its own success.
//   - Patchfiles and PatchPreArgs are the patch phase's configuration.
//     A bump relocates the patches' CONTENT and does not touch the
//     option, so comparing it proves nothing an intent asked for.
//   - Livecheck is how the port says it should be checked for updates,
//     which is a fact about maintenance and not about the port's state.
//   - Vendored moves whenever a generator regenerates its own block,
//     which is a thing a bump does deliberately and by the block's
//     rules, not a change to what the port claims. dockhand owns the
//     block boundary and nothing inside it (D6).
//
// That is the whole of what used to be a comment on a flat struct
// saying which fields "stay out of fieldTable". It is a type now, and
// the exclusion is a declaration a reader can see rather than an
// absence a reader has to notice.
type Observed struct {
	// Worksrcdir is the directory the fetched source extracts into — a
	// relative name, not a path, so unlike Filespath it is the same
	// under a shadow. A planner that must read a file out of a distfile
	// needs it to find the file.
	Worksrcdir string
	// Patchfiles is which patches the port applies.
	Patchfiles []string
	// PatchPreArgs is what the patch phase hands patch(1) before the
	// file — "-p0" unless the port says otherwise — and it is here
	// because the one thing a planner needs from it is the strip level:
	// a hunk header's path is meaningless until the -pN says how many
	// of its leading components the patch phase will discard. Kept as
	// the option's own text rather than parsed here, so what is stored
	// is what MacPorts saw; eval.StripLevel reads the number out.
	PatchPreArgs string

	// Livecheck and Vendored are configuration: what a Portfile
	// declares about how it is maintained, rather than what it is.
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
	// CargoCratesGithub is the cargo block's second form, for crates the
	// port takes from a GitHub branch rather than the registry. It
	// supplies distfiles the same way, and the cargo family regenerates
	// it alongside cargo.crates from the one Cargo.lock: a git source
	// the new version introduces lands here, and one it drops leaves.
	CargoCratesGithub string
}

// Any reports whether the port carries a vendored dependency block.
func (v Vendored) Any() bool {
	return v.GoVendors != "" || v.CargoCrates != "" || v.CargoCratesGithub != ""
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
