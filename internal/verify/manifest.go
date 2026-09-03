package verify

// Dylib is one shared library an installed port carries, as the linker
// records it rather than as the filesystem shows it.
//
// The three recorded fields are what a dependent port is actually bound
// to. An install name that moves breaks every dependent at load time,
// on a machine that is not the one that built anything; a compatibility
// version that goes backwards breaks them the same way. Both are
// invisible in a file listing and both are why a listing alone is not a
// manifest.
type Dylib struct {
	// Path is where the library sits in the installation.
	Path string
	// InstallName is what the library announces itself as — what a
	// dependent links against, which is not always where it was found.
	InstallName string
	// CompatVersion is the compatibility version a dependent must
	// satisfy.
	CompatVersion string
	// CurrentVersion is the library's own version, which may move
	// freely as long as CompatVersion does not.
	CurrentVersion string
}

// Manifest is one installation seen from outside: which port, at which
// version, on which platform, the files it owns and the libraries among
// them.
//
// Platform is the environment's own word for itself, copied down rather
// than resolved into a platform.Release, because a manifest is a report
// of what was observed and an environment naming a release this repo's
// table cannot is still telling the truth.
//
// It carries no method on purpose. Comparing two manifests is a
// judgment — a file that vanished may be a regression or the point of
// the change — and judgments are made where the plan is, not here.
type Manifest struct {
	Port     string
	Version  string
	Platform string
	// Files are the paths the port owns, as the package manager lists
	// them.
	Files []string
	// Dylibs are the shared libraries among those files, with what the
	// linker recorded in each.
	Dylibs []Dylib
}

// Manifests is a comparison's two sides and the bindings that make a
// difference between them matter.
//
// The pointers are nil-able because both absences are real and mean
// different things: a port that has never been installed has no
// baseline to be measured against, and a build that did not get far
// enough to install produced nothing to measure.
type Manifests struct {
	// Baseline is the installation the change is measured against.
	Baseline *Manifest
	// BaselineSource says where that baseline came from — a binary
	// archive, an earlier build, the machine's own install. The same
	// difference means different things depending on the answer, and a
	// caller that could not tell would report a stale baseline's age as
	// this change's doing.
	BaselineSource string
	// Installed is what this verification produced.
	Installed *Manifest
	// Links maps a library's install name to the installed files that
	// link against it: the who-would-break side of a dylib change.
	//
	// It is gathered in the environment because that is the only place
	// the whole installation is present at once, and it is keyed by
	// install name rather than by path because the install name is what
	// the dependents actually recorded.
	Links map[string][]string
}

// ProbeLine is one thing a probe ran and what came back.
//
// Argv is the command as it was run, spelled the way a reader could run
// it again, because output with no visible provenance is not evidence —
// a version string proves something only when the line above it says
// which binary was asked and how.
type ProbeLine struct {
	Binary string
	Argv   string
	Output string
}
