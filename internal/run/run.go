// Package run owns the verification lifecycle: what to build, how to
// ask for it, and — in exactly one function — what the answer means.
package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/herbygillot/dockhand/internal/artifact"
	"github.com/herbygillot/dockhand/internal/dependents"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Member is one port in a submission and the staged directory it will
// be built from.
type Member struct {
	Port    string
	Portdir string // staged, absolute
	Forced  string // the sibling deactivated to seat a withheld member
	// Names is the port and its subports — every name a build log can
	// blame that belongs to this member. It is record.Subject.Names,
	// carried through Roster, and it is here because the JUDGE needs it:
	// a cohort log that fails on py312-foo must map to the member that
	// owns it, and a reader matching on Port alone finds no member and
	// blames a stranger — turning this change's own failure into somebody
	// else's and handing back the environment that would have proved it.
	//
	// The sketch's Member declared the three fields above and no Names,
	// and Judge reads its roster off Spec and from nowhere else, so
	// without this the one interpreter could not make the distinction
	// record.Subject.Names exists for. It is written as [Port] even for a
	// port with no subports at all, because the empty slice already means
	// something else: a reader cannot otherwise tell "this port has no
	// subports" from "nobody ever asked".
	Names []string
}

// Spec is what to verify: the content by identity, the roster, the
// resolved platform and the asks. It is a Spec and not a Request
// because internal/verify already has a Request — the value sent to the
// provider — and two Requests meaning different things one import apart
// is exactly the weak identity this design is about. A spec is what you
// want verified; a request is what you send.
type Spec struct {
	Content    record.ContentID
	Roster     []Member
	Withheld   []Withheld
	Platform   platform.Release
	Test       bool
	FromSource []string
	Requires   [][]string
	KeepEnv    bool
	Trace      bool
}

// Withheld is one port kept out of a guest's roster. ONE meaning, the
// durable record.Withheld run state; admission uses NotAdmitted.
type Withheld struct {
	Port string
	Why  string
}

// ID resolves every label in a spec to an immutable identity before it
// is computed, so that "the latest base image" cannot silently change
// what a proof covers.
//
// WHICH FIELDS IT COVERS, which a draft left unsaid and which the
// automatic-adoption ruling forces: everything that changes WHAT WAS
// VERIFIED, and nothing that changes HOW A CALLER WATCHED IT.
//
//	in:  Content, Roster, Withheld, Platform, Test, FromSource, Requires
//	out: KeepEnv, Trace
//
// The line is not stylistic. A SpecID is what Adoptable matches on, so
// putting a per-invocation flag in it would mean `bump --verify --trace`
// could not adopt the attempt that `bump --verify` left running — two
// identical builds treated as different questions because one caller
// wanted to watch. Leaving them out means the adoption is correct and
// the WATCHING has to be answered separately, which it can be: see
// Adoptable.
//
// MEMBER.PORTDIR IS OUT, AND THAT RESOLVES A CONTRADICTION THE DESIGN
// CARRIED IN TWO PLACES. The list above says the Roster is in; EnqueueIn
// says the id is computed "from RECORD-DERIVABLE inputs ... and the
// shipped Spec's staged Roster (a filesystem fact) is outside it". Both
// are right about a different half of a Member. A member's PORT, its
// Names and the sibling a person forced it over are read off
// record.Change.Subjects and off an accepted cohort finding — a drain
// re-deriving the spec from the attempt hours later computes them again
// identically — while Portdir is a path under a temporary root that a
// re-plan mints fresh on every start, and no two processes would ever
// agree on it. So the roster participates by identity and not by
// location: a cohort that seats different members is a different
// question, and the same cohort staged twice is not.
//
// It is a digest and not a joined string, and every field goes in
// length-prefixed, so no value's content can forge another's boundary.
func (s Spec) ID() record.SpecID {
	h := sha256.New()
	field := func(v string) {
		_, _ = h.Write([]byte(strconv.Itoa(len(v))))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(v))
	}
	field("dockhand/spec/1")
	field(string(s.Content))
	field(s.Platform.Name)
	field(strconv.FormatBool(s.Test))
	field(strconv.Itoa(len(s.Roster)))
	for _, m := range s.Roster {
		field(m.Port)
		field(m.Forced)
		field(strconv.Itoa(len(m.Names)))
		for _, n := range m.Names {
			field(n)
		}
	}
	field(strconv.Itoa(len(s.Withheld)))
	for _, w := range s.Withheld {
		field(w.Port)
		field(w.Why)
	}
	field(strconv.Itoa(len(s.FromSource)))
	for _, p := range s.FromSource {
		field(p)
	}
	field(strconv.Itoa(len(s.Requires)))
	for _, edges := range s.Requires {
		field(strconv.Itoa(len(edges)))
		for _, e := range edges {
			field(e)
		}
	}
	return record.SpecID(hex.EncodeToString(h.Sum(nil)))
}

// Preflight is what was read out of a staged Portfile before any VM
// booted: the platform frame, and whether the port declares that it
// cannot build here. Both roads consult it, which is the fix for a
// synchronous gate that today boots a VM to discover known_fail.
type Preflight struct {
	// Read is rule 7 applied to this struct, and the design's own worked
	// example of the rule was this very type — declared, in the same
	// pass, without it. KnownFail == false otherwise means both "the port
	// does not declare known_fail" and "the staged Portfile could not be
	// read", and run.Plan consults it precisely to decline before booting
	// a VM: an unreadable Portfile would then spend the VM and come back
	// FAILED, which is the defect the preflight stage exists to close,
	// reached through an I/O failure instead of through a forgotten call.
	Read       bool
	Err        error
	KnownFail  bool
	Reason     string
	NeedsXcode bool
}

// Evidence is everything gathered about one submitted job before
// anything is decided. Gathering it does no judging; judging it does
// no I/O. The two are separate functions because today they are the
// same switch statement written twice, once in the gate and once in
// settle, and the two disagree.
type Evidence struct {
	Lease    record.LeaseID
	Platform platform.Release
	Spec     Spec
	Status   verify.Status
	Vanished bool
	Log      string
	LogRead  bool
	Members  []verify.MemberState
	// Unchecked is the members whose preflight COULD NOT BE READ, port to
	// reason, carried from record.Attempt.Unchecked.
	//
	// It replaces a `Preflight map[string]Preflight` that nothing ever
	// filled and nothing ever read — the whole preflight was declared
	// here as though a judge could re-derive it, when the staged tree it
	// was read from is dropped at the end of the pass that started the
	// build. What survives is the FAILURE, because it is the only part
	// with an unpaid cost: a preflight that answered has already declined
	// its member or asked for Xcode, and one that did not has had no
	// effect on anything and is a question nobody asked.
	//
	// Judge puts it beside a verdict, which is where run.Plan's doc
	// always said it would be and where nothing in the tree was putting
	// it.
	Unchecked map[string]string
	// Unavailable is the checks this settlement ASKED FOR AND DID NOT
	// GET, in sentences a person reads.
	//
	// Every gathering below absorbs its own refusal, and each absorption
	// is right on its own: an environment that built the port and then
	// could not describe it is a missing observation and never a verdict
	// about the port. Stacked, they were indistinguishable from a clean
	// run — a completely broken analysis path and a healthy one produced
	// the same finding, which is what makes "no finding is not a finding
	// of none" (rule 7) true in the code and false to the reader.
	//
	// So the refusals are still absorbed and are no longer silent: they
	// travel here and Finish writes them onto the proposal's criterion,
	// which is the durable sentence a person meets.
	Unavailable []string
	Prior       map[string]record.Run
	// Interrupt is a cancellation or supersession the caller is asking
	// the judge to READ. It is evidence and not a verdict: Judge returns
	// every member Canceled (or Superseded) with Disposition ReleaseQuietly
	// when it is set, so a stopped build gets the same one interpretation
	// as a finished one, and no run.Cancel effect function has to write a
	// second kind of verdict. Nil is "nobody interrupted", and only that.
	Interrupt *record.Interrupt
	// Manifests and Probes are the GUEST half of what a cohort proposal
	// needs, gathered by Observe WHILE THE LEASE IS HELD — per member,
	// the installed manifest, its baseline, and what the probe binaries
	// printed — so that Judge can write them onto record.Run (which
	// already carries Manifest, Baseline and Probes) and darwin/abi can
	// compare them without a second visit to the guest. An adversarial
	// pass found the Accept road READING the abi-dependents finding off
	// the change record and nothing in the spine WRITING it: shipped
	// settle gathers these inside its own function
	// (internal/engine/settle.go findCohort), and the design had split
	// observe from judge without moving the gathering to the observer's
	// side. The LOCAL half — reverse-index rows, in-flight branches,
	// maintainer cues — is NOT here: it is Finish's propose step's, so
	// that Judge stays the one interpreter of provider evidence and does
	// not become the cohort proposer as well (rule 2).
	Manifests map[string]Manifests
	Probes    map[string][]artifact.Probe
	At        time.Time
	// Claim is the provider's own phrase for what a pass proves —
	// verify.Capabilities.Evidence, "built in a pristine VM" for a VM
	// backend and something weaker for one whose runners carry whatever
	// the last job left.
	//
	// It is on the evidence because record.Run.Evidence must be stamped
	// AS THE RUN SETTLES rather than looked up when the record is
	// rendered — the claim belongs to the environment that was actually
	// used, and providers get reconfigured — and Judge holds no provider
	// to ask. Observe does, while it is polling, so it reads the phrase
	// there and carries it here. The sketch's Evidence had no field for
	// it and record.Run has had the field since schema 3, so the value
	// had a consumer, a producer and no way across.
	Claim string
}

// Manifests is one member's before-and-after, as the guest reported
// them. Baseline is nil when the guest had no baseline build to compare
// against, which darwin/abi answers Unavailable and never Unchanged.
type Manifests struct {
	Baseline  *artifact.Manifest
	Candidate *artifact.Manifest
	Source    string
	// Reason is why there is no baseline, in the environment's own
	// words, and empty when there is one. It rides beside Source for the
	// reason verify.Manifests keeps the two apart: "none" alone is the
	// shape of a guess, and a port that did not exist at the merge base,
	// an archive that was never published and a capture that was cut off
	// are three facts with three remedies. abi.Input has a field for it
	// and the propose step fills that field from here.
	Reason string
}

// Local is the LOCAL half of a cohort proposal's inputs, and it is a
// consumer-owned seam like Stager for the same reason: the propose step
// in Finish needs the reverse index (portindex) and the maintainer's
// Portfile cues (macports/portnote), and run holds neither an index nor
// an evaluator. app implements it; Status and the waiting judge carry
// it, because they call Finish and Finish proposes. A backend that
// cannot answer — no index built yet — returns an error and the propose
// step records nothing, which is rule 7's answer: no finding is not a
// finding of "no dependents".
type Local interface {
	Dependents(ctx context.Context, port string) ([]portindex.Dependent, []portindex.Unread, error)
	// Instructions reads the maintainer's cues out of a Portfile AT A
	// COMMIT, and the commit is the point.
	//
	// It used to take a host path, and the path it was given on every
	// settlement that goes through the frozen roster is EMPTY — run.Roster
	// seats members with a port and names and no portdir — so
	// os.ReadFile(portdir + "/Portfile") read whatever Portfile happened
	// to be under the process's working directory, and the error was
	// discarded. A cue is a fact about the port as this change left it,
	// which only the commit can answer.
	Instructions(ctx context.Context, sha, portdir string) ([]dependents.Instruction, error)
}

// Stager materializes the portdirs an attempt will build from the commit
// the attempt names, and reads their preflight. It is the consumer-owned
// seam through which the RE-PLAN the drain ruling chose reaches run: the
// queue carries an identity (Attempt.Sha, Change.Subjects) and a
// question (Spec), never a filesystem path, and Start asks this value to
// turn the identity into a staged directory at the moment of starting —
// on a person's bump, on cycle's drain and on a later verify alike. app
// implements it over tempdir, git and the evaluator; run holds none of
// those, which is why it is an interface here rather than three fields.
//
// A branchless snapshot (MintedVia Adopted from a working tree) is staged
// by the same call: its Sha is a pinned commit, and staging a commit
// does not care whether a branch names it. That is what makes the
// working-tree road one road rather than a special case that could not
// queue.
type Stager interface {
	// Stage takes the RELEASE the attempt is bound for, because the
	// preflight it reads is per-platform: known_fail and use_xcode are
	// per-platform Portfile options, and a frame is a wrong answer rather
	// than a missing one.
	//
	// It was a value on the implementation until this signature widened,
	// and the two callers that build one stager for more than one attempt
	// both got it wrong in the only way that shape can. `verify --on all`
	// passed releases[0] and preflighted a three-release matrix under one
	// of them; the drain passed the ZERO release on purpose — its own
	// comment named this signature as the fix — so every attempt a
	// dispatcher started was preflighted under the HOST's frame, for a
	// guest bound somewhere else. The cost was bounded (a VM spent
	// discovering a known_fail that could have been read for free, never
	// a wrong verdict), and it was paid on every drain of every pass.
	//
	// The zero Release stays meaningful: it is "no frame", which lets an
	// evaluator answer under its own default, and it is what a caller with
	// genuinely no platform in hand passes.
	Stage(ctx context.Context, sha string, subjects []record.Subject, on platform.Release) (roster []Member, pre map[string]Preflight, err error)
	// Baseline materializes the SAME subjects as they stood at another
	// commit — the merge base — so a provider that can measure what a
	// change is leaving has a before to compare against.
	//
	// IT IS A SECOND METHOD AND NOT A SECOND RETURN VALUE OF Stage,
	// because the two are asked at different moments and one of them can
	// honestly decline. A change with no recorded base has no before, and
	// a provider that cannot take one says so by name; both are an empty
	// slice here rather than a failure, which is what lets the ordinary
	// road stay unchanged while the measured one gets its input.
	//
	// The seam had NO ROOM FOR THIS AT ALL, which is why
	// verify.Request.Baseline — a documented field the provider honours —
	// was set by nothing in the tree, and every ABI comparison downstream
	// ran with no before and no after. Adding a call site was not the
	// missing piece; the shape was.
	Baseline(ctx context.Context, sha string, subjects []record.Subject) (portdirs []string, err error)
}

// Disposition is what should become of the environment. It is a
// decision the judge makes and the application performs.
//
// THE RULE IS ONE SENTENCE: keep the environment whenever it can still
// answer a question somebody will ask.
//
//	passed              nobody has a question       release
//	                    unless --keep-env asked     keep (D27)
//	failed              why did it fail?            keep
//	faulted             why did the runner not      keep — and the guest
//	                    work?                       is HEALTHY, so it can
//	                                                actually be asked
//	errored, present    how far did it get?         keep — the disk
//	                                                outlives the guest
//	errored, vanished   nothing can answer          nothing to release
//	canceled            a person stopped it         release quietly
//	blocked             about a port outside        release quietly
//	                    this change
//
// It is written down because it WAS NOT, and so the cases were decided
// one at a time and drifted. That is how unbuilt came to release a
// guest that had PASSED — healthy, reachable, its slot free anyway —
// holding the only proof that dockhand's own cohort runner had skipped
// a member. Nothing decided that. No rule said otherwise.
type Disposition uint8

const (
	Keep Disposition = iota
	ReleaseAndReport
	ReleaseQuietly
)

// Judgment is the single interpretation of one job's evidence.
type Judgment struct {
	Runs map[string]record.Run // by member port
	// Findings are what the interpretation itself supports, appended
	// beside the verdicts it was reached with.
	//
	// IT HAS NO PRODUCER IN THIS PACKAGE TODAY, and saying so is more
	// honest than a paragraph implying otherwise. The one finding a
	// settlement makes is the cohort proposal, and that is deliberately
	// NOT the judge's: it needs the reverse index and the maintainer's
	// Portfile cues, which arrive through Local in Finish's propose step
	// and are written by change.ProposeIn in the settle Amend (rule 2 —
	// Judge as the cohort proposer would be two judgments in one
	// function). The field is kept because the shape is ruled and because
	// a finding drawn from PROVIDER EVIDENCE ALONE belongs here and
	// nowhere else; SettleIn appends whatever it holds, so a producer
	// added later needs no second wiring.
	Findings []record.Finding
	// Disposition is what should become of the environment, for the WHOLE
	// guest: one environment holds every member, so the answers across
	// the members are folded into one here rather than left for a caller
	// to fold. Keep wins over either release — a failure's debug handle
	// and a --keep-env pass both keep it, and a sibling that passed
	// cannot take it away — and a release that is worth reporting wins
	// over a quiet one.
	Disposition Disposition
	// Blamed names the ports OUTSIDE the change that this job's evidence
	// blamed, in build order and named once: one under each member whose
	// dependency broke, because the runner goes on past such a member and
	// the next member's dependency can break too.
	//
	// It is returned rather than looked up, and that is the whole reason
	// it exists. Whether a blamed port has a maintainer is a fact about
	// the TREE, which the one pure interpreter may not go and read — the
	// shipped tree answered it with a filesystem glob run between two
	// halves of the settlement (engine.nomaintainerDep), feeding the
	// answer back into the judgment as CohortInput.Nomaintainer. Evidence
	// carries no such field, so the annotation moves out of the verdict
	// and into the report: the judge names the strangers, and the caller
	// that may look says whatever it finds out about them.
	Blamed []string
}
