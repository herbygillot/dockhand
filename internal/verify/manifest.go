package verify

import "github.com/herbygillot/dockhand/internal/artifact"

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

// Manifests is a comparison's two sides and the bindings that make a
// difference between them matter.
//
// The two sides are artifact values because the installation they
// describe is a fact about the build and not about this contract: the
// record stores them and the analyses read them, and neither should
// have to import a provider interface to name one. What is verify's
// own here is the surrounding answer — where the baseline came from,
// why there is none, and who was seen binding to what — because only a
// provider is in a position to say.
//
// The pointers are nil-able because both absences are real and mean
// different things: a port that has never been installed has no
// baseline to be measured against, and a build that did not get far
// enough to install produced nothing to measure.
//
// This type carries no json tags, unlike the artifact values it holds.
// It never reaches a note: it is one provider's answer to one question,
// taken apart by the caller into the fields a run records.
type Manifests struct {
	// Baseline is the installation the change is measured against.
	Baseline *artifact.Manifest
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
	Installed *artifact.Manifest
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
