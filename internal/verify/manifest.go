package verify

// The json tags below are lowercase because these values are written
// into a verification note, whose every other key is lowercase; without
// tags their exported Go names would be the wire keys. None of them
// carries omitempty, and that is deliberate: a baseline and an
// installed manifest are read side by side, and a key that vanishes
// when its value is empty makes the two blocks misalign exactly where
// the difference is.

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
	Path string `json:"path"`
	// InstallName is what the library announces itself as — what a
	// dependent links against, which is not always where it was found.
	InstallName string `json:"install_name"`
	// CompatVersion is the compatibility version a dependent must
	// satisfy.
	CompatVersion string `json:"compat_version"`
	// CurrentVersion is the library's own version, which may move
	// freely as long as CompatVersion does not.
	CurrentVersion string `json:"current_version"`
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
	Port     string `json:"port"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	// Files are the paths the port owns, as the package manager lists
	// them.
	Files []string `json:"files"`
	// Dylibs are the shared libraries among those files, with what the
	// linker recorded in each.
	Dylibs []Dylib `json:"dylibs"`
}

// Manifests is a comparison's two sides and the bindings that make a
// difference between them matter.
//
// The pointers are nil-able because both absences are real and mean
// different things: a port that has never been installed has no
// baseline to be measured against, and a build that did not get far
// enough to install produced nothing to measure.
//
// This type carries no json tags, unlike the others in this file. It never
// reaches a note: it is one provider's answer to one question, taken
// apart by the caller into the fields a run records.
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
	//
	// A record's Run.Links is a slice and stays one. The two are not one
	// field spelled twice: this is the observation, whole, so the
	// caller can ask about any library; the note keeps the conclusion
	// drawn about the libraries the finding is actually about, already
	// attributed and already worded as the lines a reader checks.
	// Copying the map onto the note would store the question again
	// instead of the answer.
	Links map[string][]string
}

// ProbeLine is one thing a probe ran and what came back.
//
// Argv is the command as it was run, spelled the way a reader could run
// it again, because output with no visible provenance is not evidence —
// a version string proves something only when the line above it says
// which binary was asked and how.
type ProbeLine struct {
	Binary string `json:"binary"`
	Argv   string `json:"argv"`
	Output string `json:"output"`
}
