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
	// Arch is the slice this row was read from, empty where the file
	// carried only one and the environment named no architecture.
	//
	// A universal file is several libraries in one path, and they can
	// disagree: a lipo of a 2.0.0 x86_64 slice onto a 3.0.0 arm64 slice
	// announces two different install names under one name in the
	// filesystem, and that has been built and captured rather than
	// imagined. Collapsing the slices to one would invent a measurement;
	// a row per slice lets the disagreement be seen and said.
	Arch string `json:"arch"`
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

// The three answers to "where did the baseline come from". They are
// constants rather than a provider's free text because a reader has to
// be able to tell them apart mechanically — an ABI comparison against a
// banked measurement and one against a freshly unpacked archive are
// different claims, and a comparison against nothing is not a claim at
// all.
const (
	// BaselineArchive is the honest before: the merge-base Portfile
	// staged and installed binary-only, so what was measured is the
	// version the change is leaving rather than whatever the
	// environment's own frozen tree happens to hold.
	BaselineArchive = "archive"
	// BaselineBanked is a measurement already taken for exactly this
	// Portfile blob on exactly this platform, kept rather than repeated.
	// It is a stronger claim than an archive, because it was measured
	// here rather than unpacked from a publication.
	BaselineBanked = "banked"
	// BaselineNone is no baseline, which is a refusal and not a zero.
	// Every use of it carries a BaselineReason naming why, because the
	// finding it produces says the check was unavailable and a reader
	// must be told which unavailability this was.
	BaselineNone = "none"
)

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
	//
	// One of the three constants below. It is never empty when a
	// provider was asked for a manifest at all: "we did not look" and
	// "we looked and there was nothing" are different answers, and only
	// the named ones are answers.
	BaselineSource string
	// BaselineReason says why there is no baseline, in the environment's
	// own words, and is empty when there is one.
	//
	// It exists because "none" alone is the shape of a guess. A port
	// that did not exist at the merge base, an archive that was never
	// published, and a guest whose capture was cut off are three
	// different facts with three different remedies, and a finding that
	// says only "unavailable" leaves a reader to pick one.
	BaselineReason string
	// Installed is what this verification produced.
	Installed *Manifest
	// Links is who binds to what, per SUBJECT: the dependent's own port
	// name, then a library's install name, then the files that dependent
	// installed which record it.
	//
	// The outer key is the whole point. A pull request says "gdal links
	// against libwidget.3.dylib" per member, and a map that had already
	// flattened every member's bindings into one set could only say that
	// somebody did — the file paths do not name the port that installed
	// them, and nothing downstream can map one back. So the attribution
	// is made where it still exists, in the environment, one capture per
	// subject.
	//
	// It is gathered there because that is the only place the whole
	// installation is present at once, and the inner key is an install
	// name rather than a path because the install name is what the
	// dependents actually recorded.
	//
	// A record's Run.Links is a slice and stays one. The two are not one
	// field spelled twice: this is the observation, whole, so the
	// caller can ask about any library; the note keeps the conclusion
	// drawn about the libraries the finding is actually about, already
	// attributed and already worded as the lines a reader checks.
	// Copying the map onto the note would store the question again
	// instead of the answer.
	Links map[string]map[string][]string
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
