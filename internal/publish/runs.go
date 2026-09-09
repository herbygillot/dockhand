package publish

import (
	"sort"

	"github.com/herbygillot/dockhand/internal/record"
)

// Reading a change's verdict set back out of the STATE REF.
//
// This is the half of the shipped internal/verdict that had to move
// rather than dissolve, and it changed shape on the way: verdict.Runs
// walked record.Record.Runs, a map on the exported NOTE keyed by (port,
// platform), and no decision may read the note. The authority is
// Attempt.Runs — record.Record says so where the projection is declared
// — so the walk is over the change's attempts and the collapse to one
// verdict per (port, platform) happens here instead of at export.
//
// WHICH ATTEMPTS ARE EVIDENCE FOR THIS TIP is decided by CONTENT and not
// by sha, and that is the design's own answer to what verdict/drift.go
// used to compute by scanning: "whether a passing record covers this
// tip's content is a content-identity lookup that the design makes a
// direct query rather than a scan". record.ContentID is the digest over
// the complete file set a change writes, so an attempt earned over the
// same content IS evidence for these bytes — which is what makes a
// reworded amend or a rebase keep its verification instead of losing it
// to a sha that moved.

// Verdict is one member's outcome on one platform, with everything
// needed to speak about it: the subject it judges, the platform it ran
// on, and the run.
//
// It is a struct and not a map key, for record.RunKey's own reason: a
// "port@release" spelling is a struct wearing a string's clothes, and
// the three call sites that parsed one back out with a hand-written
// scanner are what the pair type exists to retire.
type Verdict struct {
	Port     string
	Platform string
	Run      record.Run
}

// verdicts is the change's verdict set, collapsed to one run per
// (member, platform), in the change's own stable order: subjects in
// build order — the order the commit series happens in, so a reader
// meets a cohort the way its commits do — then platforms sorted within
// each subject.
//
// THE COLLAPSE RULE IS THE RECORD'S OWN, with one arm added and the arm
// stated because it is a departure. record.Record.Runs declares the
// projection as "for each (member, platform) keep the most recently
// STARTED attempt that reached a terminal verdict". That is right for
// the note, which is a record of what happened. It is not sufficient
// here, because the body this feeds must be able to say "verification
// was still running when this was promoted" — a sentence the record's
// rule can never produce, since a running attempt has reached no
// terminal verdict and would be dropped. So: a terminal verdict wins
// over a non-terminal one and a later start wins over an earlier one,
// and a (member, platform) with nothing terminal keeps its newest run
// rather than none.
//
// A run whose port no subject names is not reached, which is the same
// reading every other verb takes: a change writes a subject for any port
// it records a run against, so such a run is a mangled record rather
// than a shape this walk is meant to serve, and inventing a subject for
// it here would put a different answer in the pull request from the one
// every acting road sees.
func verdicts(c record.Change, attempts []record.Attempt) []Verdict {
	type pick struct {
		run      record.Run
		started  int64
		terminal bool
		set      bool
	}
	best := map[record.RunKey]pick{}
	platforms := map[string]bool{}
	for _, a := range attempts {
		if a.Content != c.Content || a.Platform == "" {
			continue
		}
		platforms[a.Platform] = true
		for port, run := range a.Runs {
			key := record.RunKey{Port: port, Platform: a.Platform}
			cur, had := best[key]
			cand := pick{run: run, started: a.Started.UnixNano(), terminal: run.State.Terminal(), set: true}
			if !had || better(cand.terminal, cand.started, cur.terminal, cur.started) {
				best[key] = cand
			}
		}
	}
	rels := make([]string, 0, len(platforms))
	for rel := range platforms {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	out := make([]Verdict, 0, len(best))
	for _, s := range c.Subjects {
		for _, rel := range rels {
			if p, ok := best[record.RunKey{Port: s.Port, Platform: rel}]; ok && p.set {
				out = append(out, Verdict{Port: s.Port, Platform: rel, Run: p.run})
			}
		}
	}
	return out
}

// better is the collapse rule as one comparison: a terminal verdict
// beats a non-terminal one whatever their ages, and among equals the
// later start wins. Written out rather than inlined because it is the
// rule, and a rule spelled at its one call site inside a map lookup is a
// rule nobody can find.
func better(terminal bool, started int64, wasTerminal bool, wasStarted int64) bool {
	if terminal != wasTerminal {
		return terminal
	}
	return started > wasStarted
}

// evidenceAt reports whether any attempt of this change was earned over
// the tip's own content. It is the difference between "nothing has been
// run" and "this commit adds to a change that was verified elsewhere",
// which are two different sentences in a pull request body and were one
// for the whole of the shipped tree's life.
func evidenceAt(c record.Change, attempts []record.Attempt) bool {
	for _, a := range attempts {
		if a.Content == c.Content {
			return true
		}
	}
	return false
}

// named reports a change a body must name its members in: more than one
// subject. A single change's evidence lines already have a subject — the
// pull request is about that port and its title says so — and prefixing
// every line with it would be noise in the one place candour is the
// whole point.
func named(c record.Change) bool { return len(c.Subjects) > 1 }

// headline is the port the change is about: Subjects[0], the one the
// branch is named for and the one a refusal names.
func headline(c record.Change) record.Subject {
	if len(c.Subjects) == 0 {
		return record.Subject{}
	}
	return c.Subjects[0]
}

// promotable is the publication gate over a verdict set: some run proved
// the change, the headline passed somewhere and failed nowhere, and
// every dependent reached an OUTCOME.
//
// It is record.Record.Promotable's rule, moved to the state ref's
// attempts along with everything else that used to read the note. The
// per-subject clause is not the first clause stated twice: a cohort's
// runs are summed over the whole set, so a change whose headline passed
// and whose dependent was blocked by a stranger, or errored, or was
// never reached at all, satisfies "one passed and none failed" — and
// publishing it would put a port into a pull request on evidence that
// does not exist.
//
// THE DEPENDENTS ARE BEST EFFORT (maintainer's ruling, 2026-09-04) and
// the gate says so by not asking them for a pass. A revision bump is
// owed to a dependent because the library it links moved; whether that
// dependent builds today is frequently a fact about the dependent —
// gthumb was already broken on this platform, measured — and a gate that
// held the whole change for it would make a cohort hostage to the least
// maintained port in it. What the failure earns is a line in the pull
// request body, not a veto.
//
// Terminal is still required, and that is not the same relaxation. A
// dependent still building is not a best-effort outcome, it is no
// outcome: publishing over it would put a body in front of a reviewer
// that its own guest is still in the middle of disproving.
func promotable(c record.Change, vs []Verdict) bool {
	if !anyState(vs, record.Passed) {
		return false
	}
	if len(c.Subjects) == 0 {
		// A change naming no subjects is answered by the runs alone, and
		// there is no headline to hold to a higher standard than the rest:
		// without a roster, "dependent" is not a thing this evidence can say
		// about anything. So the older, stricter rule stands here — any
		// failure blocks — rather than a best-effort reading resting on a
		// distinction the record cannot make.
		return !anyState(vs, record.Failed)
	}
	head := headline(c).Port
	if !provenFor(vs, head) || failedFor(vs, head) {
		return false
	}
	for _, s := range c.Subjects[1:] {
		// Proven anywhere is answered — a pass on one platform beside a
		// cancellation on another is a member with evidence, not a hole —
		// and otherwise every run it has must be an outcome about the port.
		if !provenFor(vs, s.Port) && !settledFor(vs, s.Port) {
			return false
		}
	}
	return true
}

func anyState(vs []Verdict, want record.RunState) bool {
	for _, v := range vs {
		if v.Run.State == want {
			return true
		}
	}
	return false
}

func provenFor(vs []Verdict, port string) bool {
	for _, v := range vs {
		if v.Port == port && v.Run.State == record.Passed {
			return true
		}
	}
	return false
}

func failedFor(vs []Verdict, port string) bool {
	for _, v := range vs {
		if v.Port == port && v.Run.State == record.Failed {
			return true
		}
	}
	return false
}

// settledFor reports a subject with at least one run, every one of which
// reached an outcome ABOUT THE DEPENDENT. A subject with no run at all
// is not settled: nobody has asked about it, which is the hole
// promotable exists to find.
//
// NOT EVERY TERMINAL STATE IS AN OUTCOME (maintainer's ruling,
// 2026-09-04). Best effort rests on the argument that whether a
// dependent builds is a fact about the dependent, and three terminal
// states are facts about something else: errored is the machine's
// failure to answer, by its own reviewer-facing sentence; canceled is a
// person's "no"; superseded is the branch moving. A publication that
// read those as settled would be reading absence of evidence as evidence
// — and, for canceled, could supply the absence itself by stopping the
// builds it was about to publish over.
func settledFor(vs []Verdict, port string) bool {
	seen := false
	for _, v := range vs {
		if v.Port != port {
			continue
		}
		seen = true
		switch v.Run.State {
		case record.Passed, record.Failed, record.Unsupported, record.Blocked, record.Withheld:
			// An outcome about the port: it built, it did not, it declines the
			// platform, a neighbour stopped it before it was reached, or this
			// build deliberately kept it out of the guest.
		case record.Queued, record.Submitting, record.Running,
			record.Canceled, record.Superseded, record.Errored, record.Faulted:
			return false
		default:
			// A state word this build cannot read is not an outcome. The
			// reading that waits is the reading that cannot publish something
			// on a verdict it did not understand.
			return false
		}
	}
	return seen
}

// unfinished names the platforms whose runs will still change on their
// own, in the change's stable order and without repeats. It asks the
// state's own Terminal, so a word this build cannot read counts as
// unfinished, for settledFor's reason.
func unfinished(vs []Verdict) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		if v.Run.State.Terminal() || seen[v.Platform] {
			continue
		}
		seen[v.Platform] = true
		out = append(out, v.Platform)
	}
	return out
}

// failedOn names the platforms a failure was recorded on, in the
// change's stable order and without repeats — what FailedError carries,
// so a refusal names the build a reader can go and look at.
func failedOn(vs []Verdict) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		if v.Run.State != record.Failed || seen[v.Platform] {
			continue
		}
		seen[v.Platform] = true
		out = append(out, v.Platform)
	}
	return out
}

// unprovenMembers names the subjects the change would publish without a
// pass of their own — the population best effort makes routine, and the
// number record.PublicationState.Unproven carries. The headline is not
// among them: a headline with no pass does not reach a publication at
// all.
func unprovenMembers(c record.Change, vs []Verdict) []string {
	var out []string
	for _, s := range c.Subjects {
		if s.Port == headline(c).Port {
			continue
		}
		if !provenFor(vs, s.Port) {
			out = append(out, s.Port)
		}
	}
	return out
}

// proposals are the findings on this change nobody has answered. A
// person is told about each and publishes anyway if they mean to; the
// machine road refuses while one stands, because there is nobody on it
// to have read the question.
func proposals(c record.Change) []record.Finding {
	var out []record.Finding
	for _, f := range c.Findings {
		if f.Disposition == record.Proposed {
			out = append(out, f)
		}
	}
	return out
}
