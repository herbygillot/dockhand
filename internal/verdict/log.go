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
	m := failedPortRE.FindStringSubmatch(summary)
	if m == nil || m[1] == port {
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
