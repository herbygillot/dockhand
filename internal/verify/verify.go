// Package verify answers questions about a plan that only building can
// answer. It owns the vocabulary — what is being asked, of what, and
// what came back — and none of the machinery; a provider package such
// as verify/tart supplies an environment that can answer.
//
// Everything here is asynchronous, including the backends that look
// synchronous. A tart VM builds a small port in about fifteen seconds
// and a large one in ten minutes, which is long enough that modelling
// it as a function call would be a lie the first time someone
// interrupts a run. The shape is therefore submit-then-poll for every
// provider, and a Job is deliberately a plain serializable value: the
// process that submitted the work is not necessarily the process that
// collects it.
package verify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
)

// Proposition is one of the independent questions verification answers
// (D4). They are not a ladder: a faithful edit can produce a port that
// does not build, and a wrong edit can produce one that builds fine
// against a cached distfile, so a provider says which it can answer
// rather than implying the rest.
type Proposition int

const (
	// PortViability asks whether the port builds at all.
	PortViability Proposition = iota
	// DeclarationCompleteness asks whether it builds somewhere that
	// carries nothing but what the port declared.
	DeclarationCompleteness
	// EditFidelity asks whether the edit says what it meant to. It is
	// answered by re-reading rather than by building, and is listed
	// here so the enumeration matches the design's.
	EditFidelity
)

func (p Proposition) String() string {
	switch p {
	case PortViability:
		return "port viability"
	case DeclarationCompleteness:
		return "declaration completeness"
	case EditFidelity:
		return "edit fidelity"
	}
	return "unknown proposition"
}

// Capabilities is what a provider can and cannot do. A requirement it
// cannot meet is reported rather than quietly downgraded: a plan that
// asked for a pristine environment and got a warm one has been answered
// with a different question.
type Capabilities struct {
	Name string
	// Propositions lists only the questions this provider actually
	// implements.
	Propositions []Proposition
	// Pristine reports whether the environment carries nothing from a
	// previous verification.
	Pristine bool
	// Interactive reports whether a failed run leaves an environment
	// the user can enter (D7's handle).
	Interactive bool
	// Platforms are the macOS releases this provider can build for.
	// There is more than one because providers serve more than one: a
	// CI backend offers whatever runner labels exist, and a VM backend
	// offers whatever base images have been provisioned. Instantiating
	// the provider once per platform would model neither.
	Platforms []platform.Release
	// Concurrent is how many verifications may run at once. For a
	// macOS guest this is Apple's licence limit, not the machine's.
	Concurrent int
	// Evidence is the provider's own phrase for what a pass proves —
	// "built in a pristine VM" for a VM backend, and something weaker
	// for a backend whose runners carry whatever the last job left. It
	// is the provider's to word because only the provider knows what
	// its environment guarantees, and it is a sentence rather than a
	// flag because it is stamped into the record and read by a person.
	//
	// It is the claim and not the finished line: a renderer states it
	// and composes around it — the run that was tested says so, the run
	// that was linted carries a clause in front — so a caller holding
	// this string holds what the environment proves, not the words a
	// reader will meet.
	Evidence string
	// InstalledManifest says the provider can describe an installation
	// from inside the environment that made it — the Manifester
	// capability, declared up front.
	//
	// It is a capability and not a Proposition because the propositions
	// are what a build PROVES about a port, and a manifest proves
	// nothing: it is an observation, which a judgment later weighs.
	//
	// Declaring it up front is what lets Request.Manifest be decided at
	// submit, which is the only moment it can be — the walk happens in
	// the environment while the build is there to be walked. The
	// Manifester type assertion at settle is the second gate, and the
	// two are deliberately separate: a provider reconfigured between
	// them produces a request nobody can answer, and a caller that
	// checked only one would report that as an ABI result rather than as
	// a check that was unavailable.
	InstalledManifest bool
	// Xcode says which bases carry a full Xcode installation, for the
	// ports that set use_xcode.
	//
	// An entry is something the provider knows; a release with no entry
	// is one it has not been told about, which is not the same fact as
	// a base it knows has none. The distinction is the whole point of a
	// map rather than a set: the honest answer today is discovered by
	// asking a booted worker, so a missing entry must keep meaning
	// "ask" — read as "no Xcode" it would refuse every use_xcode port
	// on every base, including the ones that would have built.
	Xcode map[platform.Release]bool
}

// Answers reports whether the provider claims a proposition.
func (c Capabilities) Answers(p Proposition) bool {
	for _, a := range c.Propositions {
		if a == p {
			return true
		}
	}
	return false
}

// Supports reports whether the provider can build for a release. The
// zero Release asks for the provider's default, which any provider with
// a platform at all can meet.
func (c Capabilities) Supports(r platform.Release) bool {
	if r.IsZero() {
		return len(c.Platforms) > 0
	}
	for _, p := range c.Platforms {
		if p == r {
			return true
		}
	}
	return false
}

// Request is one verification: build these ports, from these portdirs,
// under this variant frame.
type Request struct {
	// Ports are what to install — port or subport names, as `port`
	// itself would be given them.
	//
	// Ports[0] is the headline: the port the verification is about, the
	// one a refusal names and the one the record is filed under. The
	// rest ride along in the same environment, in the order they are to
	// be built, because a cohort that must be built together is one
	// build and not several — a request that could name only one port
	// would force a caller with a cohort into one environment per
	// member, which is the cost the cohort exists to avoid.
	//
	// Every caller passes one today, and one is what every provider
	// builds. An empty slice, or an empty name at the head, is a
	// malformed request rather than a request for nothing.
	Ports []string
	// Portdirs are the directories the plan touched, on the host. They
	// are staged ahead of the environment's own ports tree, so the port
	// under test is the edited one and everything else is the tree's.
	// Each must be a <category>/<port> directory: the indexer walks
	// categories, so a portdir alone indexes nothing.
	Portdirs []string
	// Baseline are the same directories as they stood before the change
	// — the merge base's copies, staged on the host — for a provider
	// that can measure what the change is leaving.
	//
	// It is a separate list rather than a flag because the environment's
	// own ports tree cannot answer the question. A guest is provisioned
	// once and its tree is frozen at that moment; it may hold a newer
	// version than the branch started from, an older one, or the port
	// may not be in it at all. The honest before is the merge-base
	// portdir, which only the caller holding the repository can produce.
	//
	// Empty means take no baseline here, which is both "there is nothing
	// to compare against" and how a caller that already holds a banked
	// measurement asks. A provider that cannot take one says so by name
	// rather than reporting an empty comparison.
	Baseline []string
	// Banked says the caller already holds a measurement for this
	// Portfile blob on this platform, so the environment must not spend
	// a download taking one.
	//
	// The provider never carries the banked value itself: a manifest
	// banked in this repository is the caller's fact, and a provider
	// that stored one would be keeping records. What it does with this
	// is record BaselineBanked and skip the install, so the answer that
	// comes back names the source the caller will supply the value for
	// instead of claiming there was none.
	Banked bool
	// Variants is the frame to build under. The zero value is the
	// default frame, which is not the same as "no variants" — a port's
	// default_variants still apply.
	Variants info.VariantSet
	// Platform is the macOS release to build on. The zero value takes
	// the provider's default, which is what a caller who does not care
	// means and what a single-platform provider has anyway. A release
	// the provider does not serve is refused rather than substituted: a
	// build on Sonoma is not evidence about Sequoia.
	Platform platform.Release
	// Owner names the checkout this work belongs to — purely
	// informational, for cross-repo worker attribution; empty is fine.
	Owner string
	// NeedsXcode says the port sets use_xcode: the environment must
	// answer xcodebuild, and one that cannot should refuse before the
	// build starts rather than fail forty minutes in.
	NeedsXcode bool
	// Test also runs the port's test suite (`port test`) after the
	// install succeeds, in the same environment. Additive on purpose,
	// the same shape as mpbb (install-port and test-port are separate
	// steps): `port test` builds but never destroots or activates, so
	// it cannot stand in for the install that verification is.
	Test bool
	// Manifest also collects what the install laid down — its files and
	// the libraries among them — from the environment that built it.
	//
	// It is asked for rather than always gathered because it costs a
	// walk of the installed port inside the guest, and a caller that
	// only wants to know whether the port builds should not pay for a
	// comparison it will not read.
	//
	// The request is where the asking belongs even though a Manifester
	// hands the answer back afterwards: the walk happens in the
	// environment while the build is there to be walked, so a caller
	// that had not said so up front would be asking a provider for
	// something it was never told to collect.
	Manifest bool
	// FromSource names ports whose binary archives must be ignored.
	// A version bump does not need this: the new version yields an
	// archive name that does not exist yet, so MacPorts builds from
	// source on its own. A re-derivation at an unchanged version does,
	// because the archive that matches predates the change and
	// verifying against it would verify nothing.
	FromSource []string
}

// Job identifies submitted work. It is a value, not a handle: writing
// it beside the plan and polling it from a later process is the point,
// because the work outlives the process that started it either way.
type Job struct {
	Provider string    `json:"provider"`
	ID       string    `json:"id"`
	Started  time.Time `json:"started"`
}

// State is where a job is.
type State int

const (
	// Running means the environment is still working.
	Running State = iota
	// Passed means the port built.
	Passed
	// Failed means it did not, which is a finding about the port.
	Failed
	// Errored means the environment could not answer, which is a fact
	// about the machine and never a finding about the port.
	Errored
)

func (s State) String() string {
	switch s {
	case Running:
		return "running"
	case Passed:
		return "passed"
	case Failed:
		return "failed"
	case Errored:
		return "errored"
	}
	return "unknown"
}

// Terminal reports whether a state will not change again.
func (s State) Terminal() bool { return s != Running }

// Status is a job's state, with the detail that exists once it is
// terminal.
type Status struct {
	State State
	// Detail explains an Errored state — why the environment could not
	// answer.
	Detail string
	// Handle names the environment the job ran in, for a provider that
	// can hold one, and is empty for a provider that cannot. It is
	// populated for a finished job whatever the verdict: a failure is
	// the obvious thing to go and look at, but a pass is worth entering
	// too — to see what landed, or to run something else against it.
	// The environment is held until the job is released.
	Handle string
	// Subject names the port a terminal state is about, when the
	// provider knows which. A cohort stops at its first failure, and
	// that failure is a finding about one member: a status that only
	// said "failed" would leave the caller to attribute it, which is
	// the guess that lands a working port's name on a broken one's
	// verdict.
	//
	// It is empty when the provider cannot say, which is every provider
	// today — one environment builds the request's headline, so naming
	// it again in the answer would add nothing. A caller reads the
	// request when this is empty, and SubjectLog splits the log the
	// same way for the same reason.
	Subject string
}

var (
	// ErrUnsupported reports that a provider cannot meet a request:
	// a proposition it does not answer, a platform it is not.
	ErrUnsupported = errors.New("verify: provider cannot meet this request")
	// ErrNoEnvironment reports that the machine cannot supply the
	// environment — the provider exists and has nothing to run on. It
	// is a doctor-shaped fact, never a finding about a port, and its
	// remedy is provisioning.
	ErrNoEnvironment = errors.New("verify: no environment available")
	// ErrNoProvider reports that the machine has no verify provider at
	// all. It is deliberately not ErrNoEnvironment: a machine with no
	// provider cannot verify, so verification quietly leaves the
	// contract (bump warns and proceeds, promote warns and allows),
	// where a provider with no base images is asked to provision.
	// Both exit in the machine band; only the remedy differs.
	//
	// It is a sentinel to branch on and not a sentence to print:
	// NoProvider builds the refusal a user reads, and says why the two
	// are not the same words.
	ErrNoProvider = errors.New("verify: no verify provider")
	// ErrUnknownJob reports a job the provider does not recognize,
	// which is what a stale job file looks like.
	ErrUnknownJob = errors.New("verify: unknown job")
)

// NoProvider reports a machine with no verify provider at all, in its
// own words and then the caller's remedy.
//
// The sentence used to be ErrNoEnvironment's, borrowed so the two
// refusals could not drift apart in a release note. They are meant to
// differ now: the two exit in different codes with different remedies —
// a tool to install against a base to provision — and a refusal that
// answers 33 while opening with 34's noun tells a script one thing and
// the person reading it another. The sentinel split was the first half
// of that distinction; these words are the half a human reads.
func NoProvider(detail string) error { return &noProvider{detail: detail} }

// noProvider is NoProvider's error. Its words and its identity are both
// ErrNoProvider's, and nothing else — a caller asking whether this
// machine merely wants provisioning must hear no, and so must a reader.
type noProvider struct{ detail string }

func (e *noProvider) Error() string { return ErrNoProvider.Error() + ": " + e.detail }

func (e *noProvider) Is(target error) bool { return target == ErrNoProvider }

// Verifier is an environment that can answer propositions about a port.
//
// Submit starts work and returns as soon as the work is running, not
// when it finishes.
//
// Poll never blocks, and never changes anything: polling a finished job
// twice must answer the same way twice. That rules out the tempting
// shortcut of releasing an environment the moment it passes, which
// turns the second poll of a successful job into "no such job".
//
// Release discards whatever the job is still holding. It is the
// caller's decision because only the caller knows it is finished — and
// on a provider with a hard limit on concurrent environments, a job
// that is never released is a slot that never comes back.
type Verifier interface {
	Capabilities() Capabilities
	Submit(ctx context.Context, req Request) (Job, error)
	Poll(ctx context.Context, job Job) (Status, error)
	// Log is the build's output so far, fetched deliberately: for a
	// local VM it is a read, for a CI provider it is a download, and
	// either way it must never ride along on Poll — polling is cheap
	// or the whole submit-and-poll shape stops being usable.
	Log(ctx context.Context, job Job) (string, error)
	Release(ctx context.Context, job Job) error
}

// CapacityError is a submission refused for want of a slot: every
// concurrent-VM licence is spoken for, counted live at admission. It
// is a machine fact — the deferred-branch flow absorbs it exactly as
// it absorbs a missing environment — and it exists as a type because
// the alternative was discovering a full machine through a
// two-minute agent timeout.
type CapacityError struct {
	Busy, Cap int
	// Synchronous says someone is waiting on this answer: the --verify
	// gate, `verify <portdir>`, an exec. Nothing is queued in that case
	// and nobody will come back for it, which is a different outcome
	// from the same refusal met by a submit that defers.
	//
	// The provider cannot fill this in — admission counts slots and has
	// no idea who is asking — so it is stamped by the caller that knows
	// it is standing there.
	Synchronous bool
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("all %d verification slots are busy (%d VMs running); `dockhand status` starts it when one frees", e.Cap, e.Busy)
}

// DockhandExit: a full machine met by a submit is pending — the run is
// deferred and status starts it when a slot frees, so nothing is
// wrong and the caller should ask again. Met by someone waiting, the
// same fact is the machine refusing the ask, because there is no
// deferred run to come back for.
func (e *CapacityError) DockhandExit() int {
	if e.Synchronous {
		return exitcode.VerifierBusy
	}
	return exitcode.VerifyQueued
}

// Code names the refusal for a machine, and says which of the two it
// was: the twin's reason is what a script reads when the band alone
// does not say whether anything is still coming.
func (e *CapacityError) Code() string {
	if e.Synchronous {
		return "verifier-busy"
	}
	return "verify-queued"
}

// Executor is the optional capability of reaching inside a live
// environment: run one command and return its output. tart implements
// it (the guest agent is right there); a CI provider whose
// environments are remote runners generally cannot, and a caller that
// needs it type-asserts — the graceful refusal for a provider without
// it is the caller's to phrase. This exists because the debug verbs
// were calling the tart package directly on any job ID, which is
// silently wrong the day a job's provider is not tart.
type Executor interface {
	Exec(ctx context.Context, job Job, argv ...string) (string, error)
}

// InteractiveShell is the optional capability of opening a human
// shell inside an environment, wired to the process's own terminal.
type InteractiveShell interface {
	Shell(ctx context.Context, job Job) error
}

// Worker is one environment a provider is running, named the way the
// provider names it. Owner is the checkout that started it, when
// anything says: attribution is informational everywhere it is
// written, so an unattributed worker is a worker rather than an error.
type Worker struct {
	Name  string
	Owner string
}

// WorkerLister is the optional capability of naming every environment
// the provider is running right now, whoever started it and whether or
// not any record here accounts for it. It exists because a provider
// with a hard cap on concurrent environments turns one forgotten
// worker into a slot nobody gets back, and the audit that finds it was
// reaching past this package into a named backend — which is silently
// wrong the day the job's provider is not that backend.
//
// The listing is the provider's whole answer; deciding which of those
// workers is unaccounted for needs records the provider does not have,
// and stays the caller's. A caller that needs this type-asserts, and
// one that meets a provider without it has learned nothing about that
// provider's workers rather than learning there are none.
type WorkerLister interface {
	Workers(ctx context.Context) ([]Worker, error)
}

// Manifester is the optional capability of saying what an installation
// actually looks like from inside the environment that made it: the
// files the port laid down, the libraries among them, and the same
// picture of whatever it is being compared against. Nothing implements
// it yet: verifytest.Fake is the tree's only Manifester, and tart is
// where the real one belongs because the guest is still holding the
// install — a provider whose environment is gone by the time anyone
// asks cannot. A caller that needs it type-asserts, and the graceful
// refusal for a provider without it is the caller's to phrase.
//
// It is a capability of its own rather than a use of Executor because
// what comes back is a value and not a terminal's worth of text:
// asking through Exec would put the parsing of one provider's output
// in every caller, which is how a debug verb ends up knowing what tart
// prints.
//
// The manifests are the provider's whole answer. Whether a difference
// between two of them is a regression or the point of the change needs
// the plan, which the provider does not have, and stays the caller's.
type Manifester interface {
	Manifests(ctx context.Context, job Job) (Manifests, error)
}

// Prober is the optional capability of running an installed port's own
// binaries in the environment and reporting what they said — the
// cheapest evidence that a build which succeeded also produced
// something that runs.
//
// It names a port because a job may have built several and each is
// probed as itself, and it answers in lines that carry the argv beside
// the output because a probe's output is only evidence to a reader who
// can see what produced it.
//
// A caller that needs it type-asserts, and one that meets a provider
// without it has learned nothing about the port's binaries rather than
// learning they are broken.
type Prober interface {
	Probe(ctx context.Context, job Job, port string) ([]ProbeLine, error)
}

// Await polls until the job is terminal or the context ends. It is a
// helper rather than a method so that no provider has to implement
// blocking, and so a caller that wants to supervise several jobs is not
// pushed into one goroutine per job.
func Await(ctx context.Context, v Verifier, job Job, every time.Duration) (Status, error) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		st, err := v.Poll(ctx, job)
		if err != nil || st.State.Terminal() {
			return st, err
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-t.C:
		}
	}
}
