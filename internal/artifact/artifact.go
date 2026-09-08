// Package artifact holds the installed facts a build leaves behind:
// what files a port put on disk, what shared libraries it installed,
// and what a probe binary printed.
//
// They live here rather than in verify because three unrelated parts of
// the tree need to name them. The verification provider observes them,
// in a guest that is still holding the install; the durable record
// stores them, because a run that has been collected is worth nothing
// if what it measured is gone; and the analyses read them back — the
// ABI comparison asks what moved between two of them, the dependents
// proposal asks who binds to what they publish. With the shapes owned
// by the provider package, the record and the analyses had to import a
// provider contract in order to name a fact, which is backwards: the
// fact is what got installed, and no provider owns that.
//
// So this package is a true leaf and must stay one. It imports nothing
// from this project — not verify, not record, not platform — and it
// decides nothing. These are the shapes only; what a change to one of
// them MEANS is a judgment, and judgments are made where the plan is.
package artifact

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
//
// The fields are Mach-O's, and this is only the shape: the reasoning
// about what a change to them means belongs to the ABI comparison, not
// here.
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

// Manifest is one installation seen from outside: which port, at which
// version, on which platform, the files it owns and the libraries among
// them.
//
// Platform is the environment's own word for itself, copied down rather
// than resolved into a platform.Release, because a manifest is a report
// of what was observed and an environment naming a release this repo's
// table cannot is still telling the truth. It is also what keeps this
// package a leaf.
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

// Probe is one thing an installed port's own binaries were asked to do
// and what came back.
//
// Argv is the command as it was run, spelled the way a reader could run
// it again, because output with no visible provenance is not evidence —
// a version string proves something only when the line above it says
// which binary was asked and how.
type Probe struct {
	Binary string `json:"binary"`
	Argv   string `json:"argv"`
	Output string `json:"output"`
}
