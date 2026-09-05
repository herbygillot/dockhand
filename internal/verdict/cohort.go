package verdict

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The cohort judge: one guest, one poll, one log, N verdicts.
//
// A change with several members builds them all inside one environment
// and writes them all into one file, so the facts a settlement has to
// go on are shared and the verdicts are not. What splits them is the
// runner's own framing — a marker line before each member's output —
// and what joins them again is that the runner stops at the first
// failure. Those two facts are the whole of the attribution: the last
// member the FILE announced is the one the guest was inside when it
// gave up, and the members after it were never built at all.
//
// The file's order and not the roster's, throughout. They agree for the
// runner there is, which announces each member once; they part for one
// that returns to a member it already built, and there the roster's
// order names a member the guest had long since left — passing the port
// that broke and condemning one that did not. Anything positional here
// is corroborated by the member's own section for the same reason: a
// place in a list is not evidence about a build.
//
// Every verdict here is still JudgeRun's, reached on one member's own
// section. That is deliberate and it is the only construction that
// makes a single subject provably unchanged: at one member there are no
// sections to cut, the whole log is what every reader gets, and
// JudgeCohort is JudgeRun with a map around it.

// CohortInput is everything one guest's settlement turns on. It is the
// plural of RunInput and not a wrapper over it: the poll, the log and
// the environment belong to the guest, so they are stated once, and
// only the runs and the roster are per member.
type CohortInput struct {
	// Subjects are the change's members in build order, Subjects[0] the
	// headline. The order is load-bearing twice over — it is the order
	// the runner built them in, so it says who came before the failure,
	// and the headline is who takes a failure nothing else explains.
	Subjects []record.Subject
	// Runs are the runs as the note holds them, by port. The judgment
	// modifies a copy of each, so what the submission recorded about a
	// member — its lint record, its from-source flag — comes through
	// untouched, exactly as it does for one.
	Runs map[string]record.Run
	// Vanished says the poll answered verify.ErrUnknownJob. It is one
	// fact for the whole cohort because it is a fact about the guest:
	// the worker is gone, and with it every member's build.
	Vanished bool
	// Status is the one poll. A provider that can name the member its
	// verdict is about says so in Status.Subject; none does today, and
	// the log's own markers answer instead.
	Status verify.Status
	// Log is the whole cohort log, uncut, and LogRead says it was
	// readable. The cutting is this package's here rather than the
	// caller's: which member a section belongs to is the judgment, and
	// a caller that cut it first would have made that judgment already.
	Log     string
	LogRead bool
	// Nomaintainer says the port CohortBlame named has no maintainer.
	// It is asked once per guest because there is one failure per guest
	// — the runner stops at it — and it is only ever consulted when a
	// port outside the change is what broke.
	Nomaintainer bool
}

// blame is who a failed cohort's failure belongs to: how far the runner
// got, which member the log named, and where the naming was printed.
type blame struct {
	// Stopper indexes the member the runner was inside when the job
	// failed, which is -1 only for a cohort with no members. It is the
	// last member the LOG announced, in the file's own order.
	Stopper int
	// Culprit indexes the member the failure belongs to, which is not
	// always the stopper. A headline's install builds the siblings it
	// depends on, so the member that actually broke can be one the
	// runner never got as far as announcing. -1 means no member owns
	// the failure and Dep does.
	Culprit int
	// Name is the culprit as the LOG spelled it, which for a subport is
	// the subport's own name and not its member's. It is what the blame
	// reader must compare against: a member's failure in its own
	// subport, compared against the member's port, reads as a
	// dependency's and blocks the member on itself.
	Name string
	// Dep is the port outside the change that the failure named.
	Dep string
	// Declined says the stopper refused the platform rather than
	// breaking on it.
	Declined bool
	// Section is the stopper's own part of the log — where the failure
	// was printed, whoever it turns out to belong to.
	Section string
	// Marked is the set of THIS CHANGE's members the log's own markers
	// announced, keyed by the member's port even where the marker
	// spelled a subport. Empty for every log a single-subject runner
	// writes — and for a log whose markers named nobody in the change,
	// which is the same fact: nothing here announced a cohort. Nothing
	// may require a marker to reach an answer.
	Marked map[string]bool
}

// JudgeCohort settles every member of one change that shares a guest.
//
// The shapes it must tell apart are four. A member the failure names is
// Failed, because it is this change's own breakage and the environment
// it broke in is worth keeping. A member the runner never reached is
// Blocked and says who stopped it — untested, not disproven. A member
// the runner finished before the stop passed, and its own section is
// where its lint line is read from. And a failure naming a port outside
// the change blocks the member it broke under on that stranger, which
// is exactly what one subject has always done — and blocks the members
// behind it on THAT member, not on the stranger, because nothing in the
// log says whether they depend on it. Measured live: py311-rawpy told
// that py310-scikit-image fails to build, a port it never depended on.
//
// The map is keyed by port and not by run key, because the release is
// one value the caller already holds and joining them is its business.
func JudgeCohort(in CohortInput) map[string]Judgment {
	out := make(map[string]Judgment, len(in.Subjects))
	// Everything that is not a failed cohort is one answer repeated,
	// because it is an answer about the guest and the guest is shared: a
	// job the provider has lost, a build still running, an environment
	// that never came up. Each member still reads its own section, so a
	// pass corroborates its own lint line and not a neighbour's.
	if in.Vanished || in.Status.State != verify.Failed {
		marked, _ := marks(in)
		for _, s := range in.Subjects {
			if withheld(in.Runs[s.Port]) {
				continue
			}
			// A guest that passed while never announcing a member is a
			// runner fault, not a verdict: the member was not built, and
			// a pass invented for it would be evidence for a promotion
			// that nothing earned. Unreachable where nothing announced a
			// member of this change — every single-subject log — because
			// there the markers say nothing about the roster at all.
			if in.Status.State == verify.Passed && len(in.Subjects) > 1 && len(marked) > 0 && !marked[s.Port] {
				out[s.Port] = unbuilt(in.Runs[s.Port])
				continue
			}
			out[s.Port] = JudgeRun(sectionInput(in, marked, s.Port, in.Status))
		}
		return out
	}
	b := attribute(in)
	if b.Stopper < 0 {
		return out
	}
	for i, s := range in.Subjects {
		run := in.Runs[s.Port]
		switch {
		case i == b.Culprit:
			// The member the failure belongs to, judged exactly as a
			// single subject is: on the section the failure was printed
			// in, against the name the log used for it.
			out[s.Port] = JudgeRun(RunInput{Run: run, Port: b.Name, Status: in.Status,
				Log: b.Section, LogRead: in.LogRead, Nomaintainer: in.Nomaintainer})
		case b.Culprit < 0 && i == b.Stopper:
			// A stranger broke, and this is the member that was building
			// when it did. Today's reading, unchanged: blocked on the
			// dependency, with the environment given back, because the
			// breakage belongs to a port this change never touched.
			out[s.Port] = JudgeRun(RunInput{Run: run, Port: s.Port, Status: in.Status,
				Log: b.Section, LogRead: in.LogRead, Nomaintainer: in.Nomaintainer})
		case b.reached(i, s, sectionOf(in, b.Marked, s.Port)):
			// Announced, finished before the stop, and its own section
			// carries no failure. The runner breaks at the first member
			// that fails, so a member it left behind it ran every command
			// it was given and every one of them exited zero.
			out[s.Port] = JudgeRun(sectionInput(in, b.Marked, s.Port,
				verify.Status{State: verify.Passed, Handle: in.Status.Handle}))
		case withheld(run):
			// Held back before the guest was asked for anything, so the
			// log's silence about it is expected rather than a fault. It
			// keeps the state the submission gave it.
			continue
		case len(b.Marked) > 0 && i < b.Stopper && !b.Marked[s.Port]:
			// A cohort that announced its members skipped this one, and
			// then stopped past it. The runner builds in this order and
			// announces before it builds, so there is no reading in which
			// this member's outcome is known: it is not a pass, and it is
			// not blocked by a member built after it either — blame that
			// pointed forward would be a sentence that cannot be true.
			out[s.Port] = unbuilt(run)
		case b.Culprit >= 0:
			culprit := in.Subjects[b.Culprit].Port
			out[s.Port] = memberBlocked(run, culprit, BlockedByMember(culprit, b.Declined))
		default:
			// A stranger broke under the stopper, and this member is not
			// the stopper: the runner stopped before it. What stopped it
			// is the sibling, and the sibling is who it is blamed on. The
			// stranger is NOT named here, because this member has no
			// section and the log therefore says nothing about what it
			// depends on — the stranger's name would ride a sentence
			// asserting an edge the tree may not carry (measured live on
			// py311-rawpy, blamed on py310-scikit-image). The stopper's
			// own verdict, above, is where the stranger is named, and
			// Blamed points a reader there.
			stopper := in.Subjects[b.Stopper].Port
			out[s.Port] = memberBlocked(run, stopper, BlockedBehindMember(stopper))
		}
	}
	return out
}

// CohortBlame is BlamedDependency's cohort twin: the port outside the
// change that a failed guest's log blames, when it blames one.
//
// It exists for the same reason the singular does — whether a blamed
// port has a maintainer is a fact about the tree, which a pure judgment
// cannot go and read — and it is asked once per guest rather than once
// per member, because a cohort stops at its first failure and therefore
// has one thing to blame. A caller runs this, looks the port up, and
// hands the answer back in CohortInput.Nomaintainer.
//
// A member of the change is never the answer. The nomaintainer note
// tells a person there is nobody to nudge about someone else's port,
// and a sibling of the change is not someone else's.
func CohortBlame(in CohortInput) (string, bool) {
	if in.Vanished || in.Status.State != verify.Failed || !in.LogRead {
		return "", false
	}
	b := attribute(in)
	return b.Dep, b.Dep != ""
}

// attribute reads a failed cohort's log for who owns the failure.
//
// Two questions, asked in that order. How far did the runner get — the
// log's markers answer it structurally, because a marker is printed
// before a member is built, so the LAST marker in the file is the
// section the guest was writing when it gave up. And what does the
// failure at that point actually name — which can be the stopper, a
// sibling the stopper was pulling in as a dependency, or a port outside
// the change entirely.
//
// Last in the file's order and not last in the roster's. They agree for
// a runner that announces each member once and stops at the first
// failure, which is the runner there is; they disagree for one that
// returns to a member it already built, which SplitSubjects
// deliberately permits — and there the roster's order would name the
// wrong stopper, pass the member that actually broke and condemn one
// that did not.
//
// Neither question requires a marker to have an answer. A log whose
// markers named no member of this change — every log a single-subject
// runner writes, every log written before a cohort runner existed, and
// a log whose markers are malformed — is read whole, the stopper falls
// back to what the provider said or to the headline, and the naming is
// read exactly as it always has been.
func attribute(in CohortInput) blame {
	b := blame{Stopper: -1, Culprit: -1}
	if len(in.Subjects) == 0 {
		return b
	}
	marked, last := marks(in)
	b.Marked, b.Stopper = marked, 0
	switch {
	case last >= 0:
		b.Stopper = last
	default:
		// Nothing announced a member of this change, so the guest's own
		// framing says nothing about how far it got. A provider that
		// names the member its verdict is about is believed; none does
		// today, and the headline is the answer when nobody speaks — the
		// member a refusal names.
		if i := memberIndex(in.Subjects, in.Status.Subject); i >= 0 {
			b.Stopper = i
		}
	}
	b.Section = sectionOf(in, b.Marked, in.Subjects[b.Stopper].Port)
	b.Name = in.Subjects[b.Stopper].Port
	if b.Declined = PortDeclined(b.Section); b.Declined {
		b.Culprit = b.Stopper
		return b
	}
	named, ok := failedPort(FailureSummary(b.Section))
	switch {
	case !ok:
		// A failure in no shape the readers know. It belongs to the
		// member the runner was inside, which is the conservative answer
		// in the direction this package is conservative in everywhere: a
		// failure is one log read away from the truth, and calling it
		// one keeps the environment that read would be made in.
		b.Culprit = b.Stopper
	case memberIndex(in.Subjects, named) >= 0:
		b.Culprit, b.Name = memberIndex(in.Subjects, named), named
	default:
		b.Dep = named
	}
	return b
}

// reached reports whether a member was announced, finished before the
// stop, and printed no failure of its own — the one shape that settles
// as a pass inside a failed cohort.
//
// It requires a marker, and that is the point: with nothing marked
// there is no evidence any member completed, and a member assumed to
// have passed on the strength of its position in a list would be a
// verdict about a build nobody watched.
//
// It also requires the member's own section to be clean, which position
// alone cannot say. A log where a member is announced twice puts output
// from after the stop under a member that sorts before it, and a pass
// awarded on position would then be awarded to the very member whose
// section holds the failure. A section carrying a diagnosis is never a
// pass here: the answer falls through to blocked, which claims nothing
// about the member either way.
func (b blame) reached(i int, s record.Subject, section string) bool {
	return i < b.Stopper && b.Marked[s.Port] && FailureSummary(section) == ""
}

// memberIndex finds the member that answers to a name a log line
// blamed, by build order, or -1.
//
// The match is against Subject.Names and not Port alone, which is what
// that field is for: a cohort log failing on py312-foo belongs to the
// member that ships the subport, and a reader comparing ports would
// find no member and blame a stranger — turning this change's own
// failure into somebody else's and handing back the environment that
// would have proved it. A member whose Names were never written answers
// to its Port, because an empty slice means nobody asked.
func memberIndex(subjects []record.Subject, port string) int {
	if port == "" {
		return -1
	}
	for i, s := range subjects {
		if s.Port == port {
			return i
		}
		for _, n := range s.Names {
			if n == port {
				return i
			}
		}
	}
	return -1
}

// marks reads the log's markers against the change's roster: which
// members were announced, and the index of the last one announced.
//
// Only members count. A marker naming a port this change does not
// contain announces nothing about the change — a build that printed the
// prefix itself, a runner whose marker file was lost and left the next
// one standing alone — and counting it would let a stranger's name
// decide how far the runner got. An empty set therefore means exactly
// one thing: nothing here announced a cohort, and every reading falls
// back to what a single subject has always done.
//
// The member is keyed by its own port even where the marker spelled a
// subport, for the reason Subject.Names exists: py312-foo's output is
// foo's, and a set keyed by what the log said would find no owner for
// it.
func marks(in CohortInput) (map[string]bool, int) {
	seen := make(map[string]bool, len(in.Subjects))
	last := -1
	for _, name := range verify.SubjectOrder(in.Log) {
		i := memberIndex(in.Subjects, name)
		if i < 0 {
			continue
		}
		seen[in.Subjects[i].Port] = true
		last = i
	}
	return seen, last
}

// sectionOf is the part of the log one member is judged by: its own
// section, and the whole log where sections do not apply.
//
// Two ways they do not. A log whose markers named no member of this
// change did not announce a cohort — every log written today, and a log
// whose markers are malformed — and cutting on them would hand every
// member "" through SubjectLog, silently throwing away the diagnosis,
// the lint line and the stranger check while still compiling. And a
// change with one member has nothing to cut a log INTO, whatever the
// log happens to contain: what a single subject concludes is decided by
// the request's shape and never by a line some build printed.
//
// The cut itself goes through verify.SubjectLog and not through the
// split map under it, because the implicit subject is keyed by the
// empty string and a lookup by port name would answer nothing for it.
func sectionOf(in CohortInput, marked map[string]bool, port string) string {
	if len(marked) == 0 || len(in.Subjects) < 2 {
		return in.Log
	}
	return verify.SubjectLog(in.Log, port)
}

// sectionInput is one member's RunInput, cut to its own section.
func sectionInput(in CohortInput, marked map[string]bool, port string, st verify.Status) RunInput {
	sub := RunInput{Run: in.Runs[port], Port: port, Vanished: in.Vanished, Status: st}
	if in.LogRead {
		sub.Log, sub.LogRead = sectionOf(in, marked, port), true
	}
	return sub
}

// memberBlocked is a member the cohort never reached, the sibling that
// stopped it, and the sentence that says how — BlockedByMember where
// the sibling failed or declined, BlockedBehindMember where the sibling
// was itself blocked by a stranger. The two are told apart by the
// caller because the caller is the one holding the blame; what is
// common is that Blamed always names a SIBLING, never a port outside
// the change, so a reader of the body is pointed at a member whose own
// verdict says the rest.
//
// Blamed is WRITTEN and not merely set where empty. The run arrives as
// the note holds it, so a blame an earlier settlement wrote would
// otherwise survive a re-reading that reached a different stopper.
//
// The environment goes back quietly. Whether the guest survives is the
// culprit's call — it keeps it, because it is this change's own
// breakage — and a member that was never built has nothing to add to
// that either way.
func memberBlocked(r record.Run, member, detail string) Judgment {
	r.State = record.Blocked
	r.Blamed = member
	r.Detail = detail
	return Judgment{Settled: true, Run: r, Release: ReleaseQuietly}
}

// unbuilt is a member the log never announced, in a cohort that
// announced others: a passing guest that skipped it, or a failed one
// that stopped somewhere past it.
//
// It is errored and not passed, and the difference is the whole reason
// this case is written down: a promotion sums the passes over every
// member, so a pass invented for a member nobody built would authorize
// publishing on evidence that does not exist. It is not failed either —
// nothing here is a finding about the port — so the guest goes back.
func unbuilt(r record.Run) Judgment {
	r.State = record.Errored
	// Nothing stopped this member, so nothing is blamed for it, and a
	// blame an earlier settlement wrote must not outlive the reading
	// that put it there.
	r.Blamed = ""
	r.Detail = "the guest reported no output for this subject"
	return Judgment{Settled: true, Run: r, Release: ReleaseQuietly}
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
