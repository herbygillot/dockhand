package run

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The cohort judge: one guest, one poll, one log, one record, N
// verdicts.
//
// A change with several members builds them all inside one environment
// and writes them all into one file, so the facts a settlement has to
// go on are shared and the verdicts are not. Two things split them.
// The runner's own framing of the log — a marker line before each
// member's output — says which part of the file is whose. And the
// runner's own record of each member — a state file per position,
// read back through verify.MemberStater — says what the runner did
// about it: built it and every command exited zero, built it and one
// did not, or never attempted it because a member it depends on had
// already failed. The record is what the log cannot give: a member
// skipped for a failed prerequisite prints nothing, and neither does a
// member a runner that died never reached, and only the record says
// which. The judge trusts it (maintainer's ruling, 2026-09-04).
//
// The runner does not stop at a failure. Every member is attempted
// unless a member it requires failed or was skipped, so a cohort can
// come back with several failures and several skips, each skip blamed
// on the prerequisite its own record names — and a member that does
// not depend on what broke is built and judged on its own section
// exactly as if nothing around it had gone wrong.
//
// Every verdict here is still judgeRun's, reached on one member's own
// section. That is deliberate and it is the only construction that
// makes a single subject provably unchanged: at one member there are no
// sections to cut and no record to read, the whole log is what every
// reader gets, and the cohort road is judgeRun with a map around it.

// Judge is the only place in dockhand that turns a provider status and
// a build log into run states. Every road that settles reaches it
// through Finish — cycle's settle stage, `status` where no dispatcher is
// resident, the judge under --wait, Cancel and the stale stage — so
// `bump --verify` and `bump` can no longer reach opposite conclusions
// about the same build.
//
// AN INTERRUPT IS EVIDENCE AND NOT A VERDICT. With one set, every member
// this attempt is judging comes back Canceled or Superseded with a quiet
// release, whatever the provider was in the middle of saying, so a
// stopped build gets the same one interpretation as a finished one and
// no run.Cancel effect function has to write a second kind of verdict.
// The interrupt is read here rather than written by the caller because
// there is one interpreter: a caller that wrote the state itself would
// be the second.
//
// WHAT IT DOES NOT JUDGE. A member whose prior run already carries a
// terminal verdict is in the roster so that the log's blame can find it
// — its name in a sibling's failure is a member's and not a stranger's —
// and out of the answer, because this attempt did not watch it and a
// verdict written over it would replace a fact with a guess. A member
// the submission withheld is out of both: the log is silent about it by
// construction, and every rule below reads silence as a fault.
func Judge(e Evidence) Judgment {
	j := Judgment{Runs: map[string]record.Run{}}
	roster := e.Spec.Roster
	if len(roster) == 0 {
		return j
	}
	var verdicts map[string]memberVerdict
	switch {
	case e.Interrupt != nil:
		verdicts = interrupted(e, roster)
	case e.Vanished || e.Status.State != verify.Failed:
		verdicts = uniform(e, roster)
	default:
		c := &cohortJudge{
			ev:       e,
			roster:   roster,
			readings: attribute(e, roster),
			out:      make(map[string]memberVerdict, len(roster)),
			done:     make([]bool, len(roster)),
			busy:     make([]bool, len(roster)),
		}
		for i := range roster {
			c.verdict(i)
		}
		verdicts = c.out
	}
	decided := false
	for _, m := range roster {
		v, ok := verdicts[m.Port]
		if !ok || !v.Settled || !judgeable(e.Prior[m.Port]) {
			continue
		}
		j.Runs[m.Port] = stamp(m.Port, v.Run, e)
		if !decided {
			j.Disposition, decided = v.Disposition, true
			continue
		}
		j.Disposition = fold(j.Disposition, v.Disposition)
	}
	j.Blamed = blamedStrangers(e, roster)
	return j
}

// memberVerdict is what the judge concluded about ONE member, before
// the answers are folded into the job's one Judgment.
//
// Settled says the evidence moved the member. A job still building
// settles nothing, and the caller must not write the run back on its
// account: an unchanged run written anyway is a state document rewritten
// for no reason, and at a resident dispatcher's cadence that is a commit
// per tick per attempt.
//
// Disposition is advice about the GUEST and not an act on it, and it is
// per member here because the readings are: one guest holds every
// member, so Judge folds these into the one answer the caller performs.
type memberVerdict struct {
	Settled     bool
	Run         record.Run
	Disposition Disposition
}

// runInput is everything one member's verdict turns on, read by the
// observer and handed over as values. There is no provider here, no
// repository and no context: a settlement is a decision about facts, and
// this is the whole fact set for one member.
//
// What is NOT here is anything about the environment. A guest is shared
// by every member in the attempt, so its handle, its claim and whether
// it has been given back belong to the job, and one member's judgment
// must not be able to write any of them: nine verdicts reached in one
// guest would otherwise each name it, and the eighth to be written would
// be the one that stood.
type runInput struct {
	// Run is the run as the attempt currently holds it. The judgment
	// modifies a copy, so the fields it does not touch — the ask, the
	// content, the manifests — come through unchanged.
	Run record.Run
	// Port is the port the RECORD names, which for a subport is its own
	// name and never the portdir's base. The blame reader compares
	// against it, so the portdir's base name here would blame the
	// subport's parent for its own failure.
	Port string
	// Vanished says the poll answered verify.ErrUnknownJob: the job is
	// one the provider no longer recognizes, which is what a worker
	// deleted out from under a record looks like.
	Vanished bool
	// Status is what the poll said, when it said anything.
	Status verify.Status
	// Log is this member's own section of the guest log, and LogRead says
	// it was actually readable. The two are separate because an
	// unreadable log is a real settle outcome — the run still settles,
	// with no diagnosis to quote — and an empty string cannot say which
	// of the two happened.
	//
	// The cutting is the judge's and not the observer's: which member a
	// section belongs to IS the judgment, and an observer that cut it
	// first would have made that judgment already.
	Log     string
	LogRead bool
}

// judgeRun settles one member against what the poll and its own section
// of the log said. It is the whole of the settle's decision: which state
// the run takes, what detail it carries, whether the environment
// survives.
//
// The refusal and blame readings both downgrade a failure, and both
// release the worker, because neither is a finding about the change: a
// port declining a platform is often the change working, and a
// dependency breaking before the change was reached leaves it untested
// rather than disproven. What the port itself broke on stays failed and
// keeps its environment.
//
// Keeping it is said with Keep and never by writing a name onto the run.
// The environment's name is the guest's, the lease is what knows it, and
// the caller stamps it on the job once for however many members failed
// inside it.
func judgeRun(in runInput) memberVerdict {
	r := in.Run
	if in.Vanished {
		// No release: the worker is already gone, which is the only
		// reason the provider cannot find the job.
		r.State, r.Detail = record.Errored, "job vanished: its worker no longer exists"
		return memberVerdict{Settled: true, Run: r, Disposition: Keep}
	}
	disposition := Keep
	switch in.Status.State {
	case verify.Running:
		return memberVerdict{Run: in.Run, Disposition: Keep}
	case verify.Passed:
		r.State = record.Passed
		if in.LogRead {
			// The log is about to become unreachable — a passing run's
			// worker is released — so what lint said is read now or never.
			// This is the lint box's corroboration, and the pointer is
			// written even when the line is empty: record.Run.Lint spells
			// "linted, and it said nothing" as a non-nil empty string, and
			// the runner lints every member it builds.
			lint := LintSummary(in.Log)
			r.Lint = &lint
		}
		disposition = ReleaseAndReport
		if r.Ask.KeepEnv {
			// Keep by request (D27): the person who started the run asked
			// to look inside a green build, and said so when they
			// submitted because by the time this judgment is reached the
			// release is in the same pass and nothing could intervene.
			// The lint line is still read above — the log is worth the
			// same whether or not the guest stands.
			disposition = Keep
		}
	case verify.Failed:
		r.State = record.Failed
		if in.LogRead {
			if PortDeclined(in.Log) {
				r.State = record.Unsupported
				r.Detail = "the port declines to build on this platform"
				disposition = ReleaseQuietly
			} else {
				// The diagnosis rides the record, so status answers "why"
				// without a log dig — the failure-side twin of the lint
				// evidence.
				r.Detail = FailureSummary(in.Log)
				// A failure that names a DIFFERENT port is a dependency
				// breaking before the change was ever reached: the branch
				// is untested, not disproven. blocked, not failed — and
				// the worker is released, because the breakage belongs to
				// a port this branch never touched (field-measured on
				// gomuks, whose verdict blamed the bump for olm).
				if dep, ok := DependencyFailure(r.Detail, in.Port); ok {
					r.State = record.Blocked
					// Blamed names a MEMBER of the change, and this is a
					// port outside it, so a blame an earlier settlement
					// wrote is cleared rather than left standing beside a
					// detail that contradicts it.
					r.Blamed = ""
					r.Detail = BlockedDetail(dep)
					disposition = ReleaseQuietly
				}
			}
		}
	case verify.Errored:
		r.State, r.Detail = record.Errored, in.Status.Detail
		disposition = ReleaseQuietly
	}
	return memberVerdict{Settled: true, Run: r, Disposition: disposition}
}

// interrupted is the whole judgment of an attempt somebody stopped: one
// state repeated, because an interrupt is a fact about the JOB and the
// job is shared. The typed cause decides the word — a person's cancel
// and a tip that moved are different things to read on a record months
// later — and the release is quiet, because nothing about the verdict
// depends on whether the guest went back.
//
// The log is still gathered by Observe before this is reached, and its
// detail is the interrupt's own rather than the log's: what a stopped
// build printed is evidence of where it got to, and the sentence a
// person reads is why it stopped.
func interrupted(e Evidence, roster []Member) map[string]memberVerdict {
	state := record.Canceled
	if e.Interrupt.Why == record.InterruptSuperseded {
		state = record.Superseded
	}
	out := make(map[string]memberVerdict, len(roster))
	for _, m := range roster {
		prior := e.Prior[m.Port]
		if withheld(prior) {
			continue
		}
		r := prior
		r.State, r.Detail, r.Blamed = state, e.Interrupt.Detail, ""
		out[m.Port] = memberVerdict{Settled: true, Run: r, Disposition: ReleaseQuietly}
	}
	return out
}

// uniform is the judgment of everything that is not a failed cohort:
// one answer repeated, because it is an answer about the guest and the
// guest is shared — a job the provider has lost, a build still running,
// an environment that never came up, a guest where every member passed.
// Each member still reads its own section, so a pass corroborates its
// own lint line and not a neighbour's.
func uniform(e Evidence, roster []Member) map[string]memberVerdict {
	marked, _ := marks(e, roster)
	out := make(map[string]memberVerdict, len(roster))
	for _, m := range roster {
		prior := e.Prior[m.Port]
		if withheld(prior) {
			continue
		}
		// A guest that passed while never announcing a member is a
		// runner fault, not a verdict: the member was not built, and
		// a pass invented for it would be evidence for a promotion
		// that nothing earned. Unreachable where nothing announced a
		// member of this change — every single-subject log — because
		// there the markers say nothing about the roster at all.
		if e.Status.State == verify.Passed && len(roster) > 1 && len(marked) > 0 && !marked[m.Port] {
			out[m.Port] = unbuilt(prior)
			continue
		}
		out[m.Port] = judgeRun(sectionInput(e, roster, marked, m.Port, e.Status))
	}
	return out
}

// blamedStrangers is the ports outside the change that a failed guest's
// log blames, one under each member whose dependency broke, in build
// order and named once.
//
// It answers with a list because a cohort can blame several strangers:
// the runner goes on past a member whose dependency broke, and the next
// member's dependency can break too. A member of the change is never
// among them — the report these feed tells a person there is nobody to
// nudge about someone else's port, and a sibling of the change is not
// someone else's.
func blamedStrangers(e Evidence, roster []Member) []string {
	if e.Interrupt != nil || e.Vanished || e.Status.State != verify.Failed || !e.LogRead {
		return nil
	}
	var deps []string
	named := map[string]bool{}
	for _, r := range attribute(e, roster) {
		if r.dep != "" && !named[r.dep] {
			deps = append(deps, r.dep)
			named[r.dep] = true
		}
	}
	return deps
}

// stamp writes onto a settled run the facts that belong to the ATTEMPT
// rather than to the reading: the content this verdict was earned
// against, when it was reached, the guest half of the evidence gathered
// while the lease was held, and — on a pass — what the provider claims a
// pass proves.
//
// The manifests and the probes are attached here and not inside
// judgeRun, for the reason the shipped settlement attached them outside
// its judgment too: they are OBSERVATIONS the guest handed over, and a
// judgment that could write them would be able to change what it was
// judging. The provider's claim is stamped as the run settles rather
// than looked up when the record is rendered, because the claim belongs
// to the environment that was actually used and providers get
// reconfigured.
func stamp(port string, r record.Run, e Evidence) record.Run {
	r.Content = e.Spec.Content
	r.At = e.At
	if m, ok := e.Manifests[port]; ok {
		r.Manifest, r.Baseline, r.BaselineSource = m.Candidate, m.Baseline, m.Source
	}
	if probes, ok := e.Probes[port]; ok {
		r.Probes = probes
	}
	if r.State == record.Passed {
		r.Evidence = e.Claim
	}
	return r
}

// fold reduces the members' answers about one guest to the job's one
// answer. Keep wins over either release — a failure's debug handle and a
// --keep-env pass both keep it, and a sibling that passed cannot take it
// away — and a release worth reporting wins over a quiet one, because
// the quiet ones are the compensating releases nothing waits on.
//
// It is only ever asked of two answers that were both REACHED. The zero
// Disposition is Keep, so seeding an accumulator with it would make
// every job keep its guest — Judge therefore takes the first settled
// member's answer as the seed and folds the rest in, and a job nothing
// was judged on keeps the zero, which is the right answer for a
// different reason: an environment this pass reached no verdict about is
// somebody else's to hand back.
func fold(a, b Disposition) Disposition {
	if a == Keep || b == Keep {
		return Keep
	}
	if a == ReleaseAndReport || b == ReleaseAndReport {
		return ReleaseAndReport
	}
	return ReleaseQuietly
}

// judgeable reports a member this attempt may write a verdict for. A
// prior run that already reached a terminal state was settled by
// somebody who watched it — a preflight decline, a withholding, a peer's
// judgment — and writing over it replaces a fact with a guess. The zero
// run is judgeable: no state at all is nothing recorded, not a verdict.
func judgeable(prior record.Run) bool {
	return prior.State == "" || !prior.State.Terminal()
}

// withheld reports a run this build deliberately never submitted.
//
// It is asked before every reading of the log, because the log is
// silent about such a member and every rule below reads silence as a
// fault: unbuilt calls it a runner error, and a blocked rule would
// blame a sibling for a member no sibling stopped. The submission
// already said what happened to it and why, and settling must not
// overwrite an answer with a worse guess.
func withheld(r record.Run) bool { return r.State == record.Withheld }
