package verify

import "strings"

// subjectPrefix opens a subject marker. It spells dockhand out because
// the marker shares a log with MacPorts' output and with whatever a
// port's own build system prints, and a splitter keyed on something a
// build could plausibly emit would cut the log in the wrong place.
// Attributing one port's failure to another is worse than not splitting
// at all.
const subjectPrefix = "===> dockhand subject: "

// SubjectMarker is the line a provider's runner prints before the
// output of one subject in a cohort. The runner and SplitSubjects agree
// on these bytes, so they are spelled in one place rather than in two
// that can drift.
//
// The line carries the subject's name and nothing else — anything else
// on it would be read as part of the name. The newline is the runner's
// to add.
func SubjectMarker(port string) string { return subjectPrefix + port }

// SplitSubjects splits a cohort's log into one section per subject,
// keyed by the name its marker carried.
//
// A section is the text between one marker and the next, marker lines
// excluded: a marker is the runner's framing, not the port's output,
// and the readers that summarize a failure must see what a
// single-subject log shows them today.
//
// The empty key is the implicit subject: everything before the first
// marker. A cohort runner's setup output lands there, and so does an
// entire log that carries no marker — which is every log today, because
// nothing prints a marker yet. A log with one implicit subject
// therefore comes back whole under "", byte for byte, and a caller
// reading today's logs gets exactly what it has always had.
//
// A subject a marker announced is present even when its section is
// empty: the runner saying a port ran and the port saying nothing is
// not the same fact as the port never being in the cohort. The implicit
// subject, which no marker announces, appears only when there is text
// before the first marker, so an empty log yields no subjects at all.
//
// A marker repeated for a subject already seen appends to that
// subject's section instead of replacing it, so a runner that returns
// to a port — building it, then testing it — loses neither half.
func SplitSubjects(log string) map[string]string {
	parts := map[string]*strings.Builder{}
	subject := ""
	for line := range strings.Lines(log) {
		name, marker := subjectOf(line)
		if marker {
			subject = name
		}
		if parts[subject] == nil {
			parts[subject] = &strings.Builder{}
		}
		if !marker {
			parts[subject].WriteString(line)
		}
	}
	out := make(map[string]string, len(parts))
	for name, b := range parts {
		out[name] = b.String()
	}
	return out
}

// SubjectOrder is the subjects the log's markers announced, in the
// order the markers appeared, repeats included.
//
// It is the companion SplitSubjects cannot be. A map has no order, so a
// reader asking "which subject was the runner inside when it stopped"
// can only answer from the map by imposing an order of its own — and
// the obvious one, the roster's, is wrong for exactly the log this
// package promises to carry: a runner that returns to a port it already
// built announces it again, and the member the guest gave up inside is
// the one the FILE names last, not the one that sorts last in the
// change.
//
// Repeats are kept rather than collapsed for the same reason. The last
// element is the whole point, and a set would not have one.
func SubjectOrder(log string) []string {
	var out []string
	for line := range strings.Lines(log) {
		if name, marker := subjectOf(line); marker {
			out = append(out, name)
		}
	}
	return out
}

// SubjectLog is the log a reader should judge one port by: that
// subject's section of a cohort log, and the whole log when nothing
// marked a subject at all.
//
// This is the accessor, and SplitSubjects is the shape underneath it.
// The difference matters because the implicit subject is keyed by the
// EMPTY string, not by a port name: a caller indexing the map by the
// port it is judging would get nothing back for every log that exists
// today, and every failure summary, lint line and refusal check would
// read an empty log while still compiling and still passing a test
// written against a cohort. Asking for the port and falling back only
// when no marker claimed anything cannot fail that way.
//
// A port that is not in a log that DID mark its subjects gets "": the
// cohort said who ran, this port was not among them, and handing back
// the prologue would let a runner's setup output be read as a port's
// own.
func SubjectLog(log, port string) string {
	subjects := SplitSubjects(log)
	if s, ok := subjects[port]; ok {
		return s
	}
	if s, ok := subjects[""]; ok && len(subjects) == 1 {
		return s
	}
	return ""
}

// subjectOf reads the subject a marker line names.
//
// The whole line must be a marker, once surrounding whitespace is gone:
// a build that quotes one mid-line — echoing the runner's own command,
// say — names no subject and must not split anything. Trimming is
// deliberate the other way too, because a log crosses a guest agent and
// a terminal before it reaches here, and a stray indent or carriage
// return must not silently merge two subjects into one.
//
// A marker with no name is not a marker. The empty key belongs to the
// implicit subject, and a runner that printed a nameless marker would
// otherwise pour its output into the prologue.
func subjectOf(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), subjectPrefix)
	if !ok {
		return "", false
	}
	name := strings.TrimSpace(rest)
	return name, name != ""
}
