package record

import (
	"time"

	"github.com/herbygillot/dockhand/internal/verify"
)

// RunKey names one subject's verdict on one platform: "port@release".
//
// The two names are joinable because neither can carry an "@" — a port
// name is [A-Za-z0-9._+-] and a release name is Apple's marketing word
// — so the key is unambiguous without quoting or escaping.
//
// It is used from day one, while every change still has exactly one
// subject and the key looks like redundant decoration. That is the
// point: a note written today with a bare release key would have to be
// re-keyed the day a cohort lands, and re-keying notes is the event
// this schema exists to avoid having twice.
func RunKey(port, release string) string { return port + "@" + release }

// runPort is RunKey's subject half, read back. It is here, beside the
// join it undoes, because the two are one fact: neither name can carry
// an "@", so the first one is the separator and a key with none names
// nobody.
func runPort(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == '@' {
			return key[:i]
		}
	}
	return ""
}

// JobRecord is one submitted environment: the job the provider handed
// back, the environment it is holding, and what this checkout knows
// about who owns it.
//
// It is keyed by release, one per platform, and every subject in the
// change shares it. Everything here is a property of the guest rather
// than of a verdict — which is why Released is here and not on a run.
// A cohort of nine on one platform is one guest; releasing it nine
// times, or releasing it after the first member finished, is the bug
// the two-map split exists to make unspellable.
type JobRecord struct {
	Job verify.Job `json:"job"`
	// Handle names the kept environment, for a provider that can hold
	// one. It is machine-local by nature: the name means nothing on any
	// other machine, and a note carrying one is describing something
	// only this host can enter.
	Handle string `json:"handle,omitempty"`
	// Test says the submission included the port's test suite after the
	// install. Promote's checklist vouches only for what a note
	// remembers, and the test is asked for per environment.
	Test bool `json:"test,omitempty"`
	// TreeAsOf is when the environment's own ports tree was last
	// updated, as the guest reported it. A build against a tree that is
	// weeks old can fail for reasons that have nothing to do with the
	// change, and a reader with no age on the tree cannot tell the two
	// apart.
	TreeAsOf time.Time `json:"tree_as_of,omitzero"`
	// Claim is the session that owns this submission, taken before the
	// guest is started. A pointer because unclaimed and claimed-by-
	// nobody are different facts, and the absent case is the common one.
	Claim *Claim `json:"claim,omitempty"`
	// Released says the environment has been given back. It is one flag
	// for the whole job because the guest is one guest: it goes back
	// when every run on it is terminal and none failed, and a failure
	// keeps it so a person can go and look.
	Released bool `json:"released,omitempty"`
}

// Claim is who owns a submission and since when.
//
// It is recorded so that two sessions cannot both start a guest for
// the same platform of the same change: the claim goes down first,
// under the notes lock, and the run reads `submitting` until the
// provider answers. A state that read terminal in that window is
// exactly how a peer starts the second guest.
type Claim struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

// Run is one subject's verdict on one platform.
//
// Everything about the environment — the job, the handle, whether it
// was released — lives on the JobRecord that this run's platform names.
// What is here is what was concluded about one port.
type Run struct {
	State RunState `json:"state"`
	// Platform is the release this run is about, spelled the way the
	// job is keyed. It carries no omitempty: the run key holds it too,
	// and a run whose platform is missing where the key says otherwise
	// is a corruption worth seeing rather than an omission worth hiding.
	Platform string `json:"platform"`
	// Detail explains the state in the words a person reads — why the
	// environment could not answer, which slot limit was hit.
	Detail string `json:"detail,omitempty"`
	// Blamed names the subject whose failure this one inherited. A
	// member whose prerequisite failed is skipped rather than built,
	// and it is blocked rather than disproven; naming the prerequisite
	// is the difference between "untested" and "untested because of
	// libwidget".
	Blamed string `json:"blamed,omitempty"`
	// Evidence is what a pass proves, in the provider's own words —
	// "built in a pristine VM", or something weaker from a backend whose
	// runners carry whatever the last job left. It is stamped from the
	// provider's capabilities as the run settles rather than looked up
	// at render time, because the claim belongs to the environment that
	// was actually used and providers get reconfigured.
	Evidence string `json:"evidence,omitempty"`
	// Linted says the run led with `port lint`. The note remembers
	// rather than the code assuming, so a verdict recorded before lint
	// existed stays honest.
	Linted bool `json:"linted,omitempty"`
	// Lint is what lint actually said, read from the log as the run
	// settles: "clean", or "2 warnings". It exists because the PR body
	// vouches per checked box, and a checked lint box with no
	// corroborating evidence was the one dishonest claim in it —
	// field-caught on the first post-lint batch.
	Lint string `json:"lint,omitempty"`
	// FromSource says the port's binary archive was ignored and the
	// build ran from source. A version bump does not need it: the new
	// version's archive does not exist yet. A re-derivation at an
	// unchanged version does, because the archive that matches predates
	// the change, and a pass earned against it verified nothing.
	FromSource bool `json:"from_source,omitempty"`
	// Manifest is what the install laid down, collected from inside the
	// environment that built it.
	Manifest *verify.Manifest `json:"manifest,omitempty"`
	// Baseline is the same picture of what the change is measured
	// against. Both are pointers because both absences are real and mean
	// different things: a port never installed has no baseline, and a
	// build that did not get far enough to install produced nothing to
	// measure.
	Baseline *verify.Manifest `json:"baseline,omitempty"`
	// BaselineSource says where the baseline came from — a binary
	// archive, a banked manifest, the machine's own install. The same
	// difference means different things depending on the answer, and a
	// reader that could not tell would report a stale baseline's age as
	// this change's doing.
	BaselineSource string `json:"baseline_source,omitempty"`
	// Links are the link-proof lines: which installed files bind to
	// which library, in the words a reader can check.
	//
	// It carries no omitempty on purpose, and it is a slice where the
	// provider's own answer is a map. The provider gathers every install
	// name mapped to its dependents, because the whole installation is
	// only present at once inside the environment; what the note keeps
	// is the conclusion drawn from it, already attributed and already
	// worded. Written without omitempty, an empty list says "we looked
	// and nothing links" where a missing key says "nobody looked" —
	// which a map re-sorted by encoding/json could not have said either.
	Links []string `json:"links"`
	// Probes are the port's own binaries run in the environment and
	// what they said: the cheapest evidence that a build which succeeded
	// also produced something that runs. Each line carries the argv
	// beside the output, because output with no visible provenance is
	// not evidence.
	Probes []verify.ProbeLine `json:"probes,omitempty"`
}
