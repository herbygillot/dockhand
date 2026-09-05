package verdict

import (
	"sort"

	"github.com/herbygillot/dockhand/internal/record"
)

// Reading schema 3's two maps back, for the judgments here and for the
// renderings above them.
//
// A record keeps one job per release and one run per subject per
// release, so anything that speaks about a run needs three things at
// once: which subject it judges, which platform it ran on, and which
// guest it ran in. Record.Platforms answers a narrower question — the
// environments the change was actually submitted to — and a report
// built on it alone would drop every queued run, which is precisely
// the set a reader is asking about when nothing has finished yet.
//
// The names are reached through the subjects and the run's own
// platform, never by splitting the run's key. The key is a join of two
// values the record already holds apart, and a reader that re-parsed it
// would be guessing at something it was handed. The engine reads its
// side the same way, for the same reason.

// RunRef is one run with everything needed to speak about it: the
// subject it judges, the platform it ran on, the run, and the guest.
type RunRef struct {
	Port     string
	Platform string
	Run      record.Run
	// Job is the guest this run's platform names, and Submitted says
	// the record actually held one.
	//
	// The flag is not decoration. A queued run was never submitted and
	// a run carried over from a pre-mint gate was earned in an
	// environment already handed back, so a missing key is an ordinary
	// answer — and the zero JobRecord a map hands back for one would
	// otherwise read as a guest that started at the zero time, which is
	// how a report comes to say a build has been running since year one.
	Job       record.JobRecord
	Submitted bool
}

// Runs walks a record's runs in a stable order: subjects in build
// order, then platforms sorted within each subject.
//
// Build order first because that is the order the change happens in: a
// cohort's headline is what the branch is about and its dependents
// follow, so a reader meets them the way the commit series does. The
// order is a property of the record and not of the call, so two
// renderings of one record name its runs identically.
//
// A run whose port no subject names is not reached, which is the same
// reading every other verb takes: the ledger writes a subject for any
// port it records a run against, so such a run is a mangled record
// rather than a shape this walk is meant to serve, and inventing a
// subject for it here would put a different answer in the report from
// the one every acting verb sees.
func Runs(r record.Record) []RunRef {
	rels := releases(r)
	out := make([]RunRef, 0, len(r.Runs))
	for _, s := range r.Subjects {
		for _, rel := range rels {
			run, ok := r.Runs[record.RunKey(s.Port, rel)]
			if !ok {
				continue
			}
			job, submitted := r.Jobs[rel]
			out = append(out, RunRef{Port: s.Port, Platform: rel, Run: run, Job: job, Submitted: submitted})
		}
	}
	return out
}

// releases lists every release a record knows about, sorted: the jobs'
// own keys, and the platforms its runs name.
//
// The union, and deliberately not Record.Platforms. Platforms answers
// what the change was submitted to, which is the right answer to a
// different question; a run that was queued and never submitted names
// a platform no job does, and a report that could not see it would go
// quiet exactly while the user is waiting.
func releases(r record.Record) []string {
	seen := make(map[string]bool, len(r.Jobs))
	for rel := range r.Jobs {
		seen[rel] = true
	}
	for _, run := range r.Runs {
		if run.Platform != "" {
			seen[run.Platform] = true
		}
	}
	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// Names reports whether a rendering must say which subject a run is
// about. One subject is what the branch is named for and every line is
// already about it; several are a cohort, where an unattributed verdict
// says nothing a reader can act on.
//
// It is asked of the record rather than counted at each line, so that
// one change renders one way throughout: a cohort whose second member
// happens to have no runs yet must not print its first member's lines
// as though the change were about that member alone.
func Names(r record.Record) bool { return len(r.Subjects) > 1 }

// DependentsNotProven names the members a promotion is publishing
// without a pass, one line each, for the author to read before the pull
// request exists.
//
// The dependents are best effort and do not gate, which makes this the
// only warning there is. It states what happened to each rather than a
// count: "2 dependents did not pass" tells an author to go and look,
// and the whole cost of looking is the reason they would not.
//
// The headline is never listed. It gates, so a promotion that got this
// far has one, and repeating it here would bury the members that are
// the point of the sentence.
func DependentsNotProven(r record.Record) []string {
	head := r.Headline().Port
	var out []string
	unproven := map[string]bool{}
	for _, p := range r.UnprovenMembers() {
		unproven[p] = true
	}
	for _, ref := range Runs(r) {
		// One reading with the audit row: the members named here are
		// exactly the ones the row counts. A member with a pass elsewhere
		// is not listed for its other platforms, and a port declining a
		// platform is not "unproven" on it.
		if ref.Port == head || !unproven[ref.Port] || ref.Run.State == record.Passed {
			continue
		}
		line := "promoting with " + ref.Port + " not proven on " + ref.Platform +
			": " + string(ref.Run.State)
		if ref.Run.Detail != "" {
			line += " — " + ref.Run.Detail
		}
		out = append(out, line)
	}
	return out
}
