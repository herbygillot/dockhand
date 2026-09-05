package verdict

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
// Every verdict here is still JudgeRun's, reached on one member's own
// section. That is deliberate and it is the only construction that
// makes a single subject provably unchanged: at one member there are no
// sections to cut and no record to read, the whole log is what every
// reader gets, and JudgeCohort is JudgeRun with a map around it.

// CohortInput is everything one guest's settlement turns on. It is the
// plural of RunInput and not a wrapper over it: the poll, the log, the
// record and the environment belong to the guest, so they are stated
// once, and only the runs and the roster are per member.
type CohortInput struct {
	// Subjects are the change's members in build order, Subjects[0] the
	// headline. The order is load-bearing: it is the order the runner
	// built them in, and the headline is who takes a failure nothing
	// else explains.
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
	// it is read only when neither the record nor the log's markers say
	// anything.
	Status verify.Status
	// Log is the whole cohort log, uncut, and LogRead says it was
	// readable. The cutting is this package's here rather than the
	// caller's: which member a section belongs to is the judgment, and
	// a caller that cut it first would have made that judgment already.
	Log     string
	LogRead bool
	// Reported is the guest's own record of each member, as the
	// provider read it back (verify.MemberStater): what the runner did
	// about each one, kept apart from anything the build printed. It is
	// empty for a provider that cannot say and for a guest whose runner
	// wrote none, and the log alone answers then — which is what it
	// always did, with one difference stated at unbuilt: a member the
	// log never announced is a fault, not a member waiting behind a
	// stop, because the runner no longer stops.
	Reported []verify.MemberState
	// Nomaintainer says, for each port outside the change that a
	// member's section blamed, whether that port has no maintainer. It
	// is keyed by port because a cohort can blame several strangers —
	// one under each member whose dependency broke — and CohortBlame
	// names them all for the caller to look up.
	Nomaintainer map[string]bool
}

// JudgeCohort settles every member of one change that shares a guest.
//
// The shapes it tells apart, on a failed guest:
//
// A member the runner built and whose every command exited zero passed,
// and its own section is where its lint line is read from. Whether the
// members around it failed says nothing about it.
//
// A member the runner built and that broke is judged on its own
// section exactly as one subject is: failed where the breakage is its
// own and the environment is kept; unsupported where it declined the
// platform; blocked on the stranger where a port outside the change
// broke under it, with the guest handed back because the breakage is
// not this change's.
//
// A member whose section blames a sibling — its install pulled another
// member out of the overlay and that member broke — is blocked on the
// sibling, in the sibling's words, and the sibling's own verdict says
// the rest. Where the sibling has no reading of its own, the failure
// printed under this member is the sibling's and it is judged on it.
//
// A member the runner skipped because a member it requires had failed
// is blocked on that prerequisite, blamed on it by name, and the
// sentence is true of the prerequisite: "fails to build" where it
// failed, "declines this platform" where it refused, and "could not be
// built" where it was itself blocked — skipped in its turn, or broken
// by a stranger. Its silence in the log is expected and not a fault.
//
// A member the guest recorded nothing about and never announced, in a
// cohort that recorded or announced others, is errored: the runner
// finishes every member it is given, so this is a runner that did not,
// and a pass invented for the member would be evidence for a promotion
// nothing earned.
//
// A log nothing framed — no record, and no marker naming a member of
// this change — is read whole, the way a single subject's is: the
// member the provider named, or the headline, takes the failure, and
// the rest are blocked behind it.
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
	if len(in.Subjects) == 0 {
		return out
	}
	j := &cohortJudge{
		in:       in,
		readings: attribute(in),
		out:      out,
		done:     make([]bool, len(in.Subjects)),
		busy:     make([]bool, len(in.Subjects)),
	}
	for i := range in.Subjects {
		j.verdict(i)
	}
	return out
}

// CohortBlame is BlamedDependency's cohort twin: the ports outside the
// change that a failed guest's log blames, one under each member whose
// dependency broke, in build order and named once.
//
// It exists for the same reason the singular does — whether a blamed
// port has a maintainer is a fact about the tree, which a pure judgment
// cannot go and read — and it answers with a list because a cohort can
// blame several strangers: the runner goes on past a member whose
// dependency broke, and the next member's dependency can break too. A
// caller runs this, looks each port up, and hands the answers back in
// CohortInput.Nomaintainer.
//
// A member of the change is never among them. The nomaintainer note
// tells a person there is nobody to nudge about someone else's port,
// and a sibling of the change is not someone else's.
func CohortBlame(in CohortInput) []string {
	if in.Vanished || in.Status.State != verify.Failed || !in.LogRead {
		return nil
	}
	var deps []string
	named := map[string]bool{}
	for _, r := range attribute(in) {
		if r.dep != "" && !named[r.dep] {
			deps = append(deps, r.dep)
			named[r.dep] = true
		}
	}
	return deps
}

// readingKind is what the guest's two records, taken together, say the
// runner did about one member.
type readingKind int

const (
	// finished: built, and every command exited zero. Passed on its own
	// section, whatever happened around it.
	finished readingKind = iota
	// broke: built, and its own section is where the failure was
	// printed. Judged on it exactly as one subject is — its own
	// breakage, a refusal, or a stranger's.
	broke
	// blamedSibling: built, and its own section names another member
	// of the change as what broke — the install pulled a sibling out of
	// the overlay and the sibling failed.
	blamedSibling
	// skipped: never attempted, because a member it requires had
	// failed or been skipped; the record names which.
	skipped
	// unrecorded: nothing recorded and nothing announced, in a cohort
	// that recorded or announced others. The runner finishes every
	// member it is given, so this is a runner fault.
	unrecorded
	// behind: nothing framed the log at all — no record, no marker
	// naming a member of this change — and this is not the member the
	// failure lands on. Blocked behind whoever is.
	behind
)

// reading is what the guest said about one member, before any verdict
// is reached from it.
type reading struct {
	kind readingKind
	// section is the member's own part of the log: its section where
	// the log is framed, the whole log where it is not.
	section string
	// port is the name its section is judged under — the member's own,
	// or the subport the log spelled its failure with, so that a member
	// failing in its own subport is not read as blocked on a stranger.
	port string
	// named is the port the section's failure names, as the log spelled
	// it, whoever it turns out to be; "" when the failure names nobody.
	named string
	// dep is the port outside the change the section blamed.
	dep string
	// sibling indexes the member this one is blocked on: the sibling
	// its section named, the prerequisite its record named, or the
	// member the failure lands on when nothing framed the log. -1 when
	// none.
	sibling int
}

// attribute reads both of the guest's records against the roster and
// says, for each member, what the runner did about it.
//
// The record comes first and the log's framing stands in for it. A
// member with a state file is read from that file: passed, failed, or
// skipped for the prerequisite it names. A member with none, in a guest
// that wrote them for others, is a runner fault however the log reads.
// Where the guest wrote no record at all, the markers answer instead:
// a member the log announced was attempted, and finished if the file
// went on to announce another member after it and its own section
// carries no failure — the runner announces a member before building
// it, so the last marker in the FILE's order is the member the guest
// was inside when it gave up, whatever position that member holds in
// the roster. A member the log never announced, in a log that
// announced others, is a runner fault for the same reason.
//
// Neither record is required to reach an answer. A log that names no
// member of this change and a guest that recorded nothing — every log
// a single-subject runner writes, and a log that could not be read —
// is read whole: the member the provider named, or the headline, takes
// the failure, and the rest stand behind it.
func attribute(in CohortInput) []reading {
	marked, last := marks(in)
	recorded := recordOf(in)
	// The member a failure lands on when nothing frames the log: the
	// member the provider named, or the headline.
	stopper := last
	if stopper < 0 {
		stopper = max(memberIndex(in.Subjects, in.Status.Subject), 0)
	}
	out := make([]reading, len(in.Subjects))
	for i, s := range in.Subjects {
		section := sectionOf(in, marked, s.Port)
		rec, has := recorded[i]
		var kind readingKind
		switch {
		case has && rec.Outcome == verify.MemberSkipped:
			k := memberIndex(in.Subjects, rec.Prerequisite)
			if k < 0 || k == i {
				// A skip naming nobody of ours, or itself, is a record this
				// judge cannot act on: a runner fault, read as one.
				out[i] = reading{kind: unrecorded, section: section, port: s.Port, sibling: -1}
				continue
			}
			out[i] = reading{kind: skipped, section: section, port: s.Port, sibling: k}
			continue
		case has && rec.Outcome == verify.MemberPassed:
			kind = finished
		case has && rec.Outcome == verify.MemberFailed:
			kind = broke
		case len(recorded) > 0, len(marked) > 0 && !marked[s.Port]:
			out[i] = reading{kind: unrecorded, section: section, port: s.Port, sibling: -1}
			continue
		case marked[s.Port] && i != last && FailureSummary(section) == "":
			kind = finished
		case marked[s.Port], i == stopper:
			kind = broke
		default:
			out[i] = reading{kind: behind, section: section, port: s.Port, sibling: stopper}
			continue
		}
		out[i] = readSection(in.Subjects, i, kind, section)
	}
	return out
}

// readSection reads one attempted member's section for who owns the
// failure in it: nobody, because it passed; the member itself, by its
// own name or a subport's; a sibling; or a port outside the change.
func readSection(subjects []record.Subject, i int, kind readingKind, section string) reading {
	r := reading{kind: kind, section: section, port: subjects[i].Port, sibling: -1}
	if kind == finished || PortDeclined(section) {
		return r
	}
	named, ok := failedPort(FailureSummary(section))
	if !ok {
		// A failure in no shape the readers know belongs to the member
		// it was printed under, which is the conservative answer in the
		// direction this package is conservative in everywhere: a failure
		// is one log read away from the truth, and calling it one keeps
		// the environment that read would be made in.
		return r
	}
	r.named = named
	switch k := memberIndex(subjects, named); {
	case k == i:
		// Its own failure, spelled the way the log spelled it — a subport
		// where the member ships one — so JudgeRun's blame reader
		// compares like against like.
		r.port = named
	case k >= 0:
		r.kind, r.sibling = blamedSibling, k
	default:
		r.dep = named
	}
	return r
}

// cohortJudge turns the readings into verdicts, member by member, in
// an order the blame dictates rather than the roster: a member blocked
// on a sibling is told what the sibling's own verdict is, so the
// sibling is settled first, however the two are placed.
type cohortJudge struct {
	in       CohortInput
	readings []reading
	out      map[string]Judgment
	// done says the member's verdict, or its deliberate absence, is in
	// out; busy says it is being reached right now, which a blame that
	// leads back to itself would otherwise chase forever.
	done, busy []bool
}

// verdict settles one member and says whether it has a verdict at all
// — a withheld member has none, and neither does a member met again
// along a blame that circles.
func (c *cohortJudge) verdict(i int) (Judgment, bool) {
	s := c.in.Subjects[i]
	if c.done[i] {
		j, ok := c.out[s.Port]
		return j, ok
	}
	if c.busy[i] {
		return Judgment{}, false
	}
	c.busy[i] = true
	defer func() { c.busy[i], c.done[i] = false, true }()

	run := c.in.Runs[s.Port]
	if withheld(run) {
		// Held back before the guest was asked for anything, so the
		// log's silence about it is expected rather than a fault. It
		// keeps the state the submission gave it.
		return Judgment{}, false
	}
	r := c.readings[i]
	var j Judgment
	switch r.kind {
	case finished:
		j = JudgeRun(c.sectionInput(i, r.section, verify.Status{State: verify.Passed, Handle: c.in.Status.Handle}))
	case broke:
		in := c.sectionInput(i, r.section, c.in.Status)
		in.Port, in.Nomaintainer = r.port, c.in.Nomaintainer[r.dep]
		j = JudgeRun(in)
	case blamedSibling:
		j = c.blamedOn(i, r)
	case skipped:
		kj, ok := c.verdict(r.sibling)
		if ok && kj.Run.State == record.Passed {
			// The record says this member was skipped for a member that
			// then passed. No runner writes that, and a blame reading
			// "fails to build" of a member that built would be a sentence
			// that cannot be true.
			j = unbuilt(run)
		} else {
			j = blockedBy(run, c.in.Subjects[r.sibling].Port, kj, ok)
		}
	case unrecorded:
		j = unbuilt(run)
	case behind:
		j = c.behindTheStopper(i, r)
	}
	c.out[s.Port] = j
	return j, true
}

// blamedOn is the verdict of a member whose own section names a
// sibling as what broke.
//
// The sibling's own verdict decides the sentence — and where the
// sibling has no reading of its own, the failure printed under this
// member is the sibling's and the sibling is judged on it, here, before
// this member is blamed on it. That is the case a headline's install
// makes when it pulls a sibling out of the overlay and the sibling
// breaks under the headline's marker: the roster is what tells the
// sibling from a stranger, and the environment stays, because the
// breakage is this change's own.
//
// A sibling that passed on its own leaves the failure where it was
// printed. The log says the sibling broke while this member was
// building it and the record says the sibling later built; what is
// true of this member is that its own build failed, and the detail
// says on what.
func (c *cohortJudge) blamedOn(i int, r reading) Judgment {
	k := r.sibling
	kp := c.in.Subjects[k].Port
	if kr := c.readings[k]; (kr.kind == behind || kr.kind == unrecorded) && !withheld(c.in.Runs[kp]) {
		// Whatever the sibling's silence had been read as — settled
		// already or not, since the roster's order says nothing about
		// which of the two is met first — the log's word about it wins.
		c.out[kp] = JudgeRun(RunInput{Run: c.in.Runs[kp], Port: r.named, Status: c.in.Status,
			Log: r.section, LogRead: c.in.LogRead})
		c.done[k] = true
	}
	kj, ok := c.verdict(k)
	if ok && kj.Run.State == record.Passed {
		// Judged under the name the log spelled, so JudgeRun's blame
		// reader sees a port failing on itself and not a dependency: the
		// sibling is one of us and built, and a sentence calling it a
		// stranger that fails to build would be false twice over.
		in := c.sectionInput(i, r.section, c.in.Status)
		in.Port = r.named
		return JudgeRun(in)
	}
	return blockedBy(c.in.Runs[c.in.Subjects[i].Port], kp, kj, ok)
}

// behindTheStopper is the verdict of a member in a log nothing framed,
// when the failure landed on another member. The member it lands on is
// settled first — its section may name this member as the sibling that
// broke, in which case this member has its verdict already — and
// otherwise this member stands behind whoever that section blamed: the
// sibling it named, or the member itself.
func (c *cohortJudge) behindTheStopper(i int, r reading) Judgment {
	s := c.in.Subjects[i]
	stopper := r.sibling
	sj, ok := c.verdict(stopper)
	if j, settled := c.out[s.Port]; settled {
		return j
	}
	target := stopper
	if sr := c.readings[stopper]; sr.kind == blamedSibling && sr.sibling != i {
		if tj, tok := c.verdict(sr.sibling); tok && tj.Run.State != record.Passed {
			target, sj, ok = sr.sibling, tj, tok
		}
	}
	return blockedBy(c.in.Runs[s.Port], c.in.Subjects[target].Port, sj, ok)
}

// sectionInput is one member's RunInput, cut to its section.
func (c *cohortJudge) sectionInput(i int, section string, st verify.Status) RunInput {
	port := c.in.Subjects[i].Port
	sub := RunInput{Run: c.in.Runs[port], Port: port, Vanished: c.in.Vanished, Status: st}
	if c.in.LogRead {
		sub.Log, sub.LogRead = section, true
	}
	return sub
}

// blockedBy is a member blocked on a sibling, in the sentence the
// sibling's own verdict makes true: "fails to build" where the sibling
// failed, "declines this platform" where it refused, and "could not be
// built" where it was itself blocked or recorded nothing — a sibling
// that was skipped in its turn, or broken by a stranger, did not fail
// at anything, and a reader sent looking for its breakage would find
// none. A sibling with no verdict at all — withheld from the build, or
// met again along a blame that circles — is named as failing, because
// this member's own evidence is that it did.
func blockedBy(run record.Run, sibling string, kj Judgment, ok bool) Judgment {
	switch {
	case !ok, kj.Run.State == record.Failed:
		return memberBlocked(run, sibling, BlockedByMember(sibling, false))
	case kj.Run.State == record.Unsupported:
		return memberBlocked(run, sibling, BlockedByMember(sibling, true))
	default:
		return memberBlocked(run, sibling, BlockedBehindMember(sibling))
	}
}

// recordOf reads the guest's record against the roster: each entry
// that names a member, keyed by the member's position. An entry naming
// nobody in the change is a record of nothing here — a runner that
// somehow wrote a subject file for a port the request did not carry —
// and is read as one, exactly as a marker naming nobody is.
func recordOf(in CohortInput) map[int]verify.MemberState {
	out := make(map[int]verify.MemberState, len(in.Reported))
	for _, ms := range in.Reported {
		if i := memberIndex(in.Subjects, ms.Port); i >= 0 {
			out[i] = ms
		}
	}
	return out
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
// The last is the last in the FILE's order and not the roster's. For a
// guest that wrote no record, it is the member the runner was inside
// when the job ended; a runner that returned to a member it had already
// built would announce it again, and the roster's order would then name
// a member the guest had long since left.
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
// change did not announce a cohort — every single-subject log, and a
// log whose markers are malformed — and cutting on them would hand
// every member "" through SubjectLog, silently throwing away the
// diagnosis, the lint line and the stranger check while still
// compiling. And a change with one member has nothing to cut a log
// INTO, whatever the log happens to contain: what a single subject
// concludes is decided by the request's shape and never by a line some
// build printed.
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

// memberBlocked is a member the cohort did not prove, the sibling that
// is the reason, and the sentence that says how — BlockedByMember where
// the sibling failed or declined, BlockedBehindMember where the sibling
// was itself blocked. The two are told apart by the caller because the
// caller is the one holding the sibling's verdict; what is common is
// that Blamed always names a SIBLING, never a port outside the change,
// so a reader of the body is pointed at a member whose own verdict says
// the rest.
//
// Blamed is WRITTEN and not merely set where empty. The run arrives as
// the note holds it, so a blame an earlier settlement wrote would
// otherwise survive a re-reading that reached a different answer.
//
// The environment goes back quietly. Whether the guest survives is the
// failed member's call — it keeps it, because it is this change's own
// breakage — and a member that was never built has nothing to add to
// that either way.
func memberBlocked(r record.Run, member, detail string) Judgment {
	r.State = record.Blocked
	r.Blamed = member
	r.Detail = detail
	return Judgment{Settled: true, Run: r, Release: ReleaseQuietly}
}

// unbuilt is a member the guest said nothing about, in a cohort where
// it said something about the others: a passing guest that never
// announced it, or a failed one that neither announced it nor recorded
// what became of it. The runner does not stop on purpose any more, so
// that silence is a runner that did not finish its work, and never a
// member waiting behind a failure — a member skipped for one has a
// record that says so.
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
// silent about such a member and every rule above reads silence as a
// fault: unbuilt calls it a runner error, and a blocked rule would
// blame a sibling for a member no sibling stopped. The submission
// already said what happened to it and why, and settling must not
// overwrite an answer with a worse guess.
func withheld(r record.Run) bool { return r.State == record.Withheld }
