package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verdict"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The findings hook: what a settlement noticed that nobody asked
// about, and the gate that holds an unattended publication until a
// person has answered it.
//
// Every fact the judgments here weigh is gathered on this side of the
// line — the environment's own description of what it installed, the
// tree's reverse index, the other branches in flight — and handed over
// as values. That is not decoration: verdict.ABIDelta and
// verdict.DependentCohort are table-tested with no repository, no
// index and no worker behind them, and this file is the whole of what
// they would otherwise have had to go and read.
//
// Nothing here proposes anything on its own authority either. The
// question is asked only where somebody would read the answer — a port
// with no dependents is never measured, because the measurement's only
// consumer is the cohort decision — and the answer is a finding a human
// accepts or dismisses.

// CohortCap is how many members one proposal puts forward, with the
// rest named as a second cohort rather than dropped.
//
// It caps BUILDS and not edits, which is why it is this small. A
// cohort's members are installed one after another in one guest, and
// the guest stops at its first failure: measured against the real tree,
// gdal's 82 dependents collapse into 39 portdirs, and a proposal of all
// of them would be a day inside one environment where any member could
// leave the rest unbuilt. Eight plus the headline is an evening, and
// what is past the cap is named — a second cohort after this one lands
// is a plan a person can act on, where a truncated list is a dependent
// left broken with nothing said about it.
const CohortCap = 8

// stampFindings dates a finding set as it is appended.
//
// A judgment has no clock: a finding made from a Portfile's bytes says
// nothing about when it was made, and the moment worth recording is the
// moment the note learned it. A finding that already carries a stamp
// keeps it, so carrying one across a commit does not re-date a question
// a person has already been asked.
func stampFindings(in []record.Finding, at time.Time) []record.Finding {
	if len(in) == 0 {
		return nil
	}
	out := make([]record.Finding, len(in))
	copy(out, in)
	for i := range out {
		if out[i].At.IsZero() {
			out[i].At = at.UTC()
		}
	}
	return out
}

// mergeFindings appends the findings a pass produced to the ones a
// record already carries, and reports whether anything was added.
//
// A finding already on the note is not appended again and not
// overwritten. The disposition is the reason: a person who dismissed a
// proposal must not be asked it a second time because a later pass
// reached the same measurement, and re-stamping At would make an old
// question look new. Identity is the kind and the ports it names — two
// abi-change findings about one port are one finding measured twice.
func mergeFindings(into *[]record.Finding, add []record.Finding) bool {
	have := make(map[string]bool, len(*into))
	for _, f := range *into {
		have[findingKey(f)] = true
	}
	changed := false
	for _, f := range add {
		if have[findingKey(f)] {
			continue
		}
		have[findingKey(f)] = true
		*into = append(*into, f)
		changed = true
	}
	return changed
}

// findingKey is a finding's identity for the append: its kind and the
// ports it is about.
func findingKey(f record.Finding) string {
	return f.Kind + "\x00" + strings.Join(f.Ports, "\x00")
}

// AnyProposed reports whether a record still carries a question nobody
// has answered.
func AnyProposed(r record.Record) bool { return len(Proposals(r)) > 0 }

// Proposals are the findings still awaiting an answer, in the order the
// record carries them.
func Proposals(r record.Record) []record.Finding {
	var out []record.Finding
	for _, f := range r.Findings {
		if f.Disposition == record.Proposed {
			out = append(out, f)
		}
	}
	return out
}

// OpenProposalError is the machine gate: an unattended publication of a
// change that is still carrying a question.
//
// It refuses the machine and never the person, which is what its exit
// code was reserved for. A human running promote is looking at the
// proposal — status prints it under the branch — and publishing anyway
// is their answer; an unattended road has nobody to have read it, and a
// pull request opened over an unanswered "these four dependents need a
// revision bump" is a change the port's own maintainer has said is
// incomplete.
//
// The two answers are both verbs, and the message names them: running
// the cohort verb accepts the proposal, and `dockhand dismiss` records
// that a person looked and said no. Dismissal is an answer worth
// recording rather than an absence — a finding that vanished when
// declined would be proposed again on the next pass.
type OpenProposalError struct {
	Branch string
	// Kinds are the finding kinds still proposed, in the record's own
	// order. They are named rather than counted: "dependent-revbump" is
	// a thing a reader can look up in the note, and "1 proposal" is not.
	Kinds []string
}

func (e *OpenProposalError) Error() string {
	return fmt.Sprintf("%s carries %d unanswered finding(s) (%s): an unattended publication will not answer them — `dockhand bump-revision --for %s` accepts the proposal, `dockhand dismiss %s` records that you looked and said no",
		e.Branch, len(e.Kinds), strings.Join(e.Kinds, ", "), e.Branch, e.Branch)
}

// DockhandExit: the refused band's machine gate — an automatic
// publication a policy refused, where a human asking for the same thing
// would be allowed it.
func (e *OpenProposalError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *OpenProposalError) Code() string { return "open-proposal" }

// MachineDisabledError is the build-time gate: this build does not let
// any machine spend ring 3, whoever asked and whatever the evidence.
//
// It is not a policy about a change and it carries none of a change's
// facts, which is why it names no branch and no finding. What it says is
// a fact about the binary: the unattended publish road is built and
// tested, the trust ladder that would say how many pull requests an
// unattended pass may open and how fast has not been ruled on, and until
// it is, the road refuses. A machine that is refused here has done
// nothing wrong and needs to change nothing — a person publishes the
// same change with `dockhand promote`.
//
// The permission is a PARAMETER and never a package variable. A variable
// can be set by an init, by a test, or by a future composition that did
// not know it was granting anything; a parameter cannot be widened
// without changing every call site, which is the point.
type MachineDisabledError struct{}

func (e *MachineDisabledError) Error() string {
	return "this build does not permit unattended publication: the machine publish road is disabled at build time — `dockhand promote` publishes on a person's authority"
}

// DockhandExit: the refused band's machine gate — an automatic
// publication a policy refused, where a human asking for the same thing
// would be allowed it.
func (e *MachineDisabledError) DockhandExit() int { return exitcode.MachineGate }

// Code names the refusal for a machine.
func (e *MachineDisabledError) Code() string { return "machine-publish-disabled" }

// GateRing3 is the build-time gate over every publication a machine
// would make: nil for a person always, nil for a machine only on a build
// that granted the permission, and the refusal otherwise.
//
// Ring 3 is other people's attention, and it is spent by exactly two
// acts: pushing the fork branch and creating or editing the pull request
// against upstream. This gate sits inside both funnels — Engine.push and
// Engine.publishGh — so it dominates them from BELOW rather than
// standing beside them, and it is called again at the top of every
// publish road so that the refusal a machine gets is this one and not a
// complaint about its evidence.
//
// Deleting a fork branch is deliberately not gated by it. That is the
// sweep's whole job, it runs unattended today under `status` and
// `clean`, and it spends nothing of ring 3: the branch is the user's own
// and the pull request is what reviewers see. The gate is written as
// "a machine may not PUBLISH" and not "a machine may not push" for
// exactly that reason — the other wording stops `clean` working on a
// timer and buys nothing.
func GateRing3(by record.Driver, permitted bool) error {
	if by != record.Machine || permitted {
		return nil
	}
	return &MachineDisabledError{}
}

// GateMachinePublish is the gate itself: nil for a person, and the
// refusal for an unattended road that would publish an unanswered
// proposal.
//
// The invoker is a parameter rather than something inferred from which
// code path arrived, on Publication's own terms: how a change reached
// review is a fact worth being explicit about, and a gate that guessed
// it from a call site would be the one place a new caller could widen
// what the machine is allowed to do by accident.
func GateMachinePublish(r record.Record, branch string, by record.Driver) error {
	if by != record.Machine {
		return nil
	}
	open := Proposals(r)
	if len(open) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(open))
	for _, f := range open {
		kinds = append(kinds, f.Kind)
	}
	return &OpenProposalError{Branch: branch, Kinds: kinds}
}

// observed evidence, per subject: what the environment said it
// installed, what the change is measured against, and the bindings and
// probes taken while the guest was still holding it.
type installEvidence struct {
	Installed *verify.Manifest
	Baseline  *verify.Manifest
	Source    string
	Reason    string
	// Links are this subject's OWN link-proof lines, drawn once the
	// measurement has said which install names moved.
	//
	// Nil and empty are different answers and the note keeps them apart:
	// nil is nobody looked — a member the cohort never reached, or a run
	// taken before anything was measured — and empty is the sweep ran
	// over this member's own files and none of them records a name that
	// moved, which is the build-only-in-fact claim a reviewer is
	// entitled to see beside a revbump that was spent anyway.
	Links  []string
	Probes []verify.ProbeLine
}

// guestEvidence is one guest's whole answer: whether it was asked at
// all, the comparison it made, and each subject's own observations.
type guestEvidence struct {
	// Described says the environment was asked for a manifest and
	// answered. It is separate from an empty comparison because the two
	// have different remedies — a provider that cannot describe an
	// installation needs a different provider, and a build that
	// installed nothing needs fixing — and a finding that inferred one
	// from the other would pick one and be wrong half the time.
	Described bool
	Manifests verify.Manifests
	ByPort    map[string]installEvidence
}

// describe asks the environment what it installed, while it is still
// holding it.
//
// Before the release and never after: handing a guest back puts both
// sides of the comparison out of reach, along with the binaries a probe
// runs. And absorbed rather than returned — an environment that built
// the port and then could not describe it is a missing observation and
// never a verdict about the port, so the settlement records what it has
// and the finding says the check was unavailable. A Manifester failure
// that travelled would strand every run on that guest as running
// forever, which is the opposite of what the evidence is for.
//
// Both capabilities are optional and both are asked for separately. A
// provider that declares InstalledManifest and does not implement
// Manifester is refused by name here rather than discovered at the
// comparison: the declaration is what the submit read to ask for a
// manifest at all, and the two gates are deliberately apart because a
// provider reconfigured between them produces a request nobody can
// answer.
func (e *Engine) describe(ctx context.Context, prov verify.Verifier, job verify.Job, in verdict.CohortInput) guestEvidence {
	ev := guestEvidence{ByPort: map[string]installEvidence{}}
	if len(in.Subjects) == 0 {
		return ev
	}
	headline := in.Subjects[0].Port
	m, implements := prov.(verify.Manifester)
	switch {
	case !prov.Capabilities().InstalledManifest:
		// The provider never said it could, so the submit never asked. A
		// walk happens inside the environment while the build is there to
		// be walked, and asking now for something nobody collected would
		// answer with an empty comparison — which reads as every library
		// removed, the strongest false break there is.
	case !implements:
		fmt.Fprintf(e.Err, "warning: %s declares it can describe an installation and implements no Manifester; the ABI check is unavailable\n",
			providerName(prov))
	default:
		got, err := m.Manifests(ctx, job)
		switch {
		case err == nil:
			ev.Described, ev.Manifests = true, got
		case errors.Is(err, verify.ErrUnknownJob):
			// The guest is gone. Nothing to describe and nothing to say
			// about the port.
		default:
			fmt.Fprintf(e.Err, "warning: describing the installation on %s: %v\n", job.ID, err)
		}
	}
	// The comparison is the headline's: a Manifester answers for a job,
	// so what comes back is one port's two sides. The bindings are NOT
	// attributed here — they belong to the members that recorded them,
	// and which of the headline's install names they are worth reporting
	// against is not known until the measurement has said what moved.
	// findCohort does both, in that order.
	ev.ByPort[headline] = installEvidence{
		Installed: ev.Manifests.Installed,
		Baseline:  ev.Manifests.Baseline,
		Source:    ev.Manifests.BaselineSource,
		Reason:    ev.Manifests.BaselineReason,
	}

	if p, ok := prov.(verify.Prober); ok {
		for _, s := range in.Subjects {
			if in.Runs[s.Port].State != record.Running {
				// A member already settled is in the cohort so the log's
				// blame can find it, and out of the probing because this
				// pass is not writing its run.
				continue
			}
			lines, err := p.Probe(ctx, job, s.Port)
			if err != nil {
				continue
			}
			got := ev.ByPort[s.Port]
			got.Probes = lines
			ev.ByPort[s.Port] = got
		}
	}
	return ev
}

// providerName is a provider's own name for a warning, falling back to
// something a reader can still act on.
func providerName(prov verify.Verifier) string {
	if name := prov.Capabilities().Name; name != "" {
		return name
	}
	return "the verify provider"
}

// attach writes one subject's observations onto the run about to be
// recorded.
func attach(run record.Run, ev installEvidence) record.Run {
	run.Manifest, run.Baseline = ev.Installed, ev.Baseline
	run.BaselineSource, run.Links, run.Probes = ev.Source, ev.Links, ev.Probes
	return run
}

// findCohort measures what the change did to the headline's ABI and
// proposes the revision bumps it calls for.
//
// It is asked only where somebody would read the answer. A port nothing
// depends on is never measured — the measurement's one consumer is the
// cohort decision, and an abi-unchanged finding on every bump in the
// tree would be a note nobody reads and a schema's worth of noise. The
// dependents are therefore both the gate and the input, and they are
// read once.
//
// Everything else it gathers is what keeps the judgments pure: the
// declared dependents with their exclusion signals, the branches
// already carrying a change to one of them, and the instruction
// comments the plan quoted at mint. None of the three is something a
// judgment may go and look up, and all three change what the proposal
// says.
func (e *Engine) findCohort(ctx context.Context, repo *git.Repo, n record.Record, ev *guestEvidence) []record.Finding {
	head := n.Headline()
	if head.Port == "" {
		return nil
	}
	rows, unread, err := e.dependentsOf(head.Port)
	if err != nil {
		// The one place in this feature where nothing used to be said.
		//
		// A tree with no PortIndex answers here, and PortIndex is
		// generated by portindex(1) rather than carried in a clone — so on
		// a fresh checkout every bump settled with no finding, no proposal
		// and no warning, byte-identical to a leaf port that genuinely has
		// nothing depending on it. Two states, opposite remedies, one
		// silence: run portindex, or nothing to do. The resolver a few
		// files away already declines this exact error by name, and so
		// does this now.
		return []record.Finding{indexUnavailable(head, err)}
	}
	if len(rows) == 0 {
		return nil
	}
	abi := verdict.ABIDelta(verdict.ABIInput{
		Port: head.Port, Portdir: head.Portdir,
		Described: ev.Described,
		// The headline's own run says whether the installed side ignored
		// the archive, because "measured against what was published" and
		// "measured against what this branch built" are different claims
		// about the after side.
		FromSource: fromSourceOn(n, head.Port),
		Manifests:  ev.Manifests,
	})
	// The link proof is drawn here and not in describe, because it needs
	// this: a binding is worth reporting against the names that MOVED,
	// and only the measurement knows which those are.
	proveLinks(ev, abi)

	out := []record.Finding{abi.Finding()}
	if answered(n) {
		// The cohort's own verification settles against the same tip, so
		// this runs a second time over the same dependents. The proposal
		// has an answer on this record already — accepted by a commit that
		// is on the branch, or dismissed by a person — and asking again is
		// asking somebody twice. The measurement is still recorded: it is
		// a fact about the change and mergeFindings keeps whichever
		// reading arrived first.
		return out
	}
	carried := map[string]bool{}
	for _, p := range n.Ports() {
		carried[strings.ToLower(p)] = true
	}
	deps := make([]verdict.Dependent, 0, len(rows))
	inFlight := e.inFlight(ctx, repo, n.Sha)
	for _, r := range rows {
		deps = append(deps, verdict.Dependent{
			Port: r.Name, Portdir: r.Portdir, Keys: r.Keys,
			ReplacedBy: r.ReplacedBy, KnownFail: r.KnownFail,
			Nomaintainer: r.Nomaintainer,
			InFlight:     inFlight[strings.ToLower(r.Name)],
			Carried:      carried[strings.ToLower(r.Name)],
			Requires:     r.Requires,
			Conflicts:    r.Conflicts,
		})
	}
	short := make([]verdict.Unread, 0, len(unread))
	for _, u := range unread {
		short = append(short, verdict.Unread{Port: u.Port, Portdir: u.Portdir, Field: u.Field})
	}
	if f, ok := verdict.DependentCohort(abi, Instructions(n), deps, short, CohortCap).Finding(); ok {
		out = append(out, f)
	}
	return out
}

// indexUnavailable states a reverse index that could not be read, as
// the ABI check being unavailable for the reason it actually was.
//
// It is an abi-unavailable and not a kind of its own, because that is
// what happened: the measurement's one consumer is the cohort decision,
// and with no index there is no cohort decision to make. The remedy is
// the command that fixes it, named — an index is generated, not cloned.
func indexUnavailable(head record.Subject, err error) record.Finding {
	why := "ABI check unavailable: the ports tree's reverse index could not be read, so nothing could be said about what depends on " +
		head.Port + " — " + err.Error()
	if errors.Is(err, portindex.ErrNoIndex) {
		why += " (run `portindex` in the tree to generate one)"
	}
	return record.Finding{
		Kind:        string(verdict.ABIUnavailable),
		Ports:       []string{head.Port},
		Criterion:   why,
		Disposition: record.Accepted,
	}
}

// answered reports whether a person has already given this record's
// revbump proposal an answer.
func answered(n record.Record) bool {
	for _, f := range n.Findings {
		if f.Kind == render.KindCohort && f.Disposition != record.Proposed {
			return true
		}
	}
	return false
}

// proveLinks writes each subject's own link proof onto its evidence.
//
// Per subject, against the names that moved. Both halves are the
// finding this replaced: the provider's map used to be flattened across
// the whole cohort, so a member's bindings could only be attributed to
// the headline and the per-member line in a pull request was
// unreachable; and the proof was taken against every install name the
// headline publishes, so a dependent that linked only an UNMOVED
// library came back "linked" and was printed as the evidence for its
// own revision bump.
//
// A member the map has no entry for keeps nil — nobody looked. A member
// with an entry and no matching binding gets an empty list, which is
// the measurement that makes it build-only in fact.
func proveLinks(ev *guestEvidence, abi verdict.ABI) {
	if !ev.Described || abi.Verdict == verdict.ABIUnavailable {
		// A measurement that could not be made says nothing about which
		// names moved, so an empty proof against it would be the sentence
		// "links nothing that moved" written under a check that never
		// ran. Nil is the honest record: nobody could look.
		return
	}
	moved := abi.Broke()
	for port, bound := range ev.Manifests.Links {
		got := ev.ByPort[port]
		got.Links = verdict.LinkProof(port, moved, bound).Lines
		if got.Links == nil {
			// An empty list is an answer — the sweep ran over this member
			// and nothing it installed records a name that moved — and a
			// missing key would say nobody looked. The note's own field
			// carries no omitempty for exactly this.
			got.Links = []string{}
		}
		ev.ByPort[port] = got
	}
}

// Instructions are the instruction comments a record carries, as the
// cohort decision weighs them.
//
// The finding is where they are kept and this is the projection: the
// note is written at mint, when the Portfile's bytes are in hand, and
// read at settle, when the measurement is. Mapping here rather than
// re-reading the Portfile is what makes the quote the same quote — a
// second read would meet whatever the working tree says today, which is
// not what the change was planned against.
func Instructions(n record.Record) []verdict.Instruction {
	var out []verdict.Instruction
	for _, f := range n.Findings {
		if f.Kind != intent.FindingInstruction || f.Disposition == record.Dismissed {
			// A dismissed comment is one a person has answered. Weighing
			// it again would ask them twice.
			continue
		}
		q := verdict.Instruction{Source: f.Source, Quote: f.Quote}
		for _, c := range f.Candidates {
			q.Ports = append(q.Ports, c.Port)
		}
		out = append(out, q)
	}
	return out
}

// fromSourceOn reports whether any of a subject's runs ignored the
// binary archive. Per subject and over every platform, because the
// answer is a property of the change rather than of one guest, and a
// reader that stopped at the first run the map handed it would answer
// differently on different passes.
func fromSourceOn(n record.Record, port string) bool {
	for _, ref := range verdict.Runs(n) {
		if ref.Port == port && ref.Run.FromSource {
			return true
		}
	}
	return false
}

// dependentsOf reads the tree's reverse index for one port.
//
// A run with no tree wired answers with no dependents and no proposal:
// nothing was configured to look, which is the one absence with nothing
// to say. A tree whose index cannot be walked is an ERROR and travels
// as one — a partial reverse index is a cohort missing members with
// nothing said about it, and the index refuses rather than truncating
// for exactly that reason, so a caller that swallowed the refusal would
// undo the guarantee it exists for.
// It answers with the rows AND with the fields the index could not
// read, because both go into what the proposal says: a cohort built
// over a reverse index that dropped a dependency field may be short by
// exactly those ports, and a caller that took the rows alone would put
// the short list forward as a complete one.
//
// The two absences it tells apart are the two the caller has to say
// different things about. No tree wired, or a directory that is not a
// ports tree, means nobody was ever in a position to look — dockhand is
// running somewhere else, resolution says so loudly on its own, and a
// finding here would appear under every branch of every run outside a
// tree. A tree that opened and whose index will not walk is the other
// one: the question was asked and could not be answered, and that is
// what travels as an error.
func (e *Engine) dependentsOf(port string) ([]portindex.Dependent, []portindex.Unread, error) {
	if e.Tree == nil {
		return nil, nil, nil
	}
	t, err := e.Tree()
	if err != nil {
		return nil, nil, nil
	}
	rev, err := t.Dependents()
	if err != nil {
		return nil, nil, err
	}
	return rev.ByPort[strings.ToLower(port)], rev.Unread, nil
}

// inFlight maps every port another dockhand branch is already changing
// to that branch, keyed by folded name.
//
// Over every SUBJECT and not the headline alone. A dependent already
// carried as a member of somebody else's cohort is as in flight as one
// a branch is named for, and a scan that read only the headline would
// propose revbumping a port another branch is about to revbump — two
// revisions and a conflict at merge.
//
// A branch that cannot be read is stepped over rather than reported.
// The scan's answer is an exclusion reason, so one unreadable note
// costs one exclusion and never the proposal.
func (e *Engine) inFlight(ctx context.Context, repo *git.Repo, exceptSha string) map[string]string {
	out := map[string]string{}
	branches, err := repo.Branches(ctx, git.BranchNamespace)
	if err != nil {
		return out
	}
	l := e.Ledger(repo)
	for _, br := range branches {
		tip, err := repo.RevParse(ctx, br)
		if err != nil || tip == exceptSha {
			continue
		}
		n, err := l.Read(ctx, tip)
		if err != nil {
			continue
		}
		for _, p := range n.Ports() {
			if p == "" {
				continue
			}
			if _, seen := out[strings.ToLower(p)]; !seen {
				out[strings.ToLower(p)] = br
			}
		}
	}
	return out
}
