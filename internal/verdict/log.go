package verdict

import (
	"fmt"
	"regexp"
	"strings"
)

// The settle-time log readers. A finished guest log is the only thing
// dockhand ever learns about a build beyond pass or fail, and these four
// functions are everything it reads out of one: what lint said, why the
// run failed, whether the port refused the platform rather than breaking
// on it, and whether the breakage belonged to a neighbour. What they
// return becomes the note, so their answers are as much a wire format as
// the note's fields are.
//
// Every one of them is conservative in the same direction: a shape they
// do not recognize leaves the run failed, because a failure is only ever
// a log read away from the truth while a wrong unsupported releases the
// environment that could have proved it.

// lintRE matches port lint's own summary line.
var lintRE = regexp.MustCompile(`(\d+) errors? and (\d+) warnings? found`)

// LintSummary compresses a run's lint outcome to what a reviewer wants:
// "clean", or the warning count — the run already failed if there were
// errors. Empty when the log carries no lint summary.
func LintSummary(log string) string {
	m := lintRE.FindStringSubmatch(log)
	switch {
	case m == nil:
		return ""
	case m[2] == "0":
		return "clean"
	case m[2] == "1":
		return "1 warning"
	}
	return m[2] + " warnings"
}

// FailureSummary is the first substantive Error line of a failed run's
// log — the line naming which phase failed and why — skipping the
// boilerplate pointers that follow it. Empty when the log carries none.
func FailureSummary(log string) string {
	for line := range strings.Lines(log) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Error: ") ||
			strings.HasPrefix(line, "Error: See ") ||
			strings.HasPrefix(line, "Error: Follow ") {
			continue
		}
		if len(line) > 160 {
			line = line[:160] + "…"
		}
		return strings.TrimPrefix(line, "Error: ")
	}
	return ""
}

// PortDeclined reads a failure log for the shapes of a port refusing a
// platform rather than breaking on it. Conservative on purpose: an
// unrecognized refusal stays "failed", which is only ever a log-read
// away from the truth.
func PortDeclined(log string) bool {
	for _, marker := range []string{"known to fail", "known_fail"} {
		if strings.Contains(log, marker) {
			return true
		}
	}
	return false
}

// failedPortRE reads which port a MacPorts failure line blames — the
// "Failed to <phase> <name>:" shape every phase failure opens with.
var failedPortRE = regexp.MustCompile(`^Failed to [a-z]+ ([A-Za-z0-9._+-]+):`)

// DependencyFailure reports the port a failure summary blames when it
// is not the port under test. Conservative like PortDeclined: a line
// that names no port, or names the port itself, changes nothing.
//
// The summary it reads is FailureSummary's output — already truncated,
// with the "Error: " prefix already gone — and not the raw log. The
// regexp is anchored, so feeding it anything else silently matches
// nothing.
func DependencyFailure(summary, port string) (string, bool) {
	name, ok := failedPort(summary)
	if !ok || name == port {
		return "", false
	}
	return name, true
}

// failedPort is the port a failure summary blames, whoever it turns out
// to be: DependencyFailure without the question of whether that is the
// port under test.
//
// A cohort has to ask that question of every member rather than of one,
// and it cannot ask it by calling DependencyFailure N times: a summary
// naming a sibling would come back "yes, a dependency" from every
// member except the sibling itself, which is the same answer a stranger
// gives and means the opposite thing.
func failedPort(summary string) (string, bool) {
	m := failedPortRE.FindStringSubmatch(summary)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// BlamedDependency is the two readers a caller must run in order to
// know whether a failed run needs the nomaintainer lookup: a port
// refusing the platform is not a blocked run and must not send anyone
// globbing the tree, so PortDeclined wins over the blame check exactly
// as it does inside JudgeRun.
//
// It exists so the guard order is written once. A caller doing the tree
// read that JudgeRun cannot do calls this first, looks the dependency
// up, and passes the answer back in.
func BlamedDependency(log, port string) (string, bool) {
	if PortDeclined(log) {
		return "", false
	}
	return DependencyFailure(FailureSummary(log), port)
}

// BlockedDetail names the dependency that blocked a verification, and
// whether anyone maintains it — a nomaintainer dependency means there
// is no one to nudge, which changes what the maintainer does next.
//
// Whether the dependency is maintained is a fact about the ports tree,
// so the caller reads it: glob the one category level for the
// dependency's Portfile and look for "nomaintainer" in it. A port that
// cannot be found is simply not annotated, which is why the answer is a
// plain bool and a failed lookup is indistinguishable from a maintained
// port. Both mean "say nothing".
func BlockedDetail(dep string, nomaintainer bool) string {
	who := ""
	if nomaintainer {
		who = " (nomaintainer)"
	}
	return fmt.Sprintf("dependency %s%s fails to build; the change itself is untested", dep, who)
}

// BlockedByMember names the sibling that stopped a cohort before this
// member was reached.
//
// It is a second sentence rather than BlockedDetail with a different
// name in it, because BlockedDetail ends "the change itself is
// untested" and that is a true thing to say about a stranger breaking
// and a false one about a sibling. A cohort's sibling IS the change:
// part of it was built, part of it broke, and the part that never ran
// is what this sentence is about.
//
// A decline is worded apart for the same reason. A member that refuses
// the platform stopped the cohort without failing at anything, and a
// reader told it "fails to build" would go looking for a breakage that
// is not there.
//
// What it says of the blocked member is "untested" and not "never
// built", because the two are different and only one of them is always
// true: the member the runner gave up inside had started, and the
// members behind it in the queue had not, and both of them come out of
// a cohort with nothing proven about them.
func BlockedByMember(member string, declined bool) string {
	what := "fails to build"
	if declined {
		what = "declines this platform"
	}
	return fmt.Sprintf("%s %s; this member is untested", member, what)
}

// BlockedBehindMember names the sibling this member was skipped for
// when that sibling was itself BLOCKED — a port outside the change
// broke under it, or it was skipped in its turn for a prerequisite of
// its own — rather than failed.
//
// It is a third sentence and not BlockedByMember with the stranger's
// name in it, because the stranger is not this member's to blame. A
// member the runner skipped has no section, so nothing in the log says
// what it depends on; the one fact the record carries about it is the
// member it was skipped for, and that member is the thing a reader can
// go and check. Naming the stranger instead was measured live
// (py311-rawpy told that py310-scikit-image fails to build — a port it
// does not depend on): a sentence asserting a dependency edge the tree
// does not carry, in a body a reviewer reads.
//
// "Could not be built" and not "fails to build", because the sibling
// did not fail at anything: its dependency did, or its own
// prerequisite's, and a reader sent looking for the sibling's breakage
// would find none. "Not built" and not "not reached", because the
// runner did reach this member and chose not to build it — and a
// member behind a failure in a log nothing framed, the one case where
// "not reached" might be true, is not a case the record can tell from
// this one.
func BlockedBehindMember(member string) string {
	return fmt.Sprintf("%s could not be built, so this member was not built; it is untested", member)
}
