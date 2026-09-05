package tart

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/verify"
)

// The capability is the contract, provably.
var _ verify.MemberStater = Provider{}

// MemberStates reads back what the cohort runner recorded about each
// member: the state.<i> files, in the order the members were built,
// with the marker file beside each one saying whose record it is.
//
// It answers from the guest's own files and not from the log, which is
// the whole point of asking. The log is written by the build under
// test; the state files are written by dockhand's own script, and a
// member the runner skipped for a failed prerequisite leaves nothing in
// the log at all — the state file is the only place that says it was
// skipped rather than never reached.
//
// A job that built one subject has no per-member record — the
// single-subject runner writes none, and its bytes are frozen — and
// answers with nothing rather than with an error. Absence of the whole
// record is an answer this protocol relies on, the same way a missing
// baseline is for Manifests.
func (p Provider) MemberStates(ctx context.Context, job verify.Job) ([]verify.MemberState, error) {
	if err := p.owns(ctx, job); err != nil {
		return nil, err
	}
	out, err := Exec(ctx, p.Tools, job.ID, "/bin/sh", "-c", memberStatesScript(stateDir))
	if err != nil {
		return nil, fmt.Errorf("reading the members' states from %s: %w", job.ID, err)
	}
	return memberStatesOf(out), nil
}

// memberStatesScript prints the runner's record in one read: for each
// position that has a marker file, the marker line and then whatever
// the state file holds. One round trip for the whole cohort, because a
// cohort can hold every dependent a library has and a read per member
// would be two execs per port.
//
// The output is framed by the markers themselves, so the reader is
// verify.SplitSubjects — the same cut the judge makes on the log — and
// the member's name reaches this process the way it reaches the log:
// as the contents of a file launch wrote, never as a word this script
// interpolates. The directory is the only thing spliced in, and it is
// this package's own constant.
//
// The loop's last statement is the increment, so the loop's own status
// is zero however the file tests went, and the script exits zero for a
// guest whose record is simply short.
func memberStatesScript(dir string) string {
	return `d=` + dir + `
i=0
while [ -f "$d/subject.$i" ]; do
  cat "$d/subject.$i"
  [ -f "$d/state.$i" ] && cat "$d/state.$i"
  i=$((i+1))
done
`
}

// memberStatesOf reads the script's output back into the record: one
// entry per marker, in the marker's order, with the state file's first
// word as the outcome and, for a skipped member, its second line
// translated from the runner's position back into a port name.
//
// A member whose section is empty is one the runner wrote no state
// file for, and stays MemberUnreported. A word the runner never writes
// reads the same way: the runner's vocabulary is three words, and a
// fourth is a record this reader does not understand rather than one
// it should guess at. A skipped member naming a position that is not
// in the record, or its own, keeps the outcome and loses the name — the
// judge treats a skip with no prerequisite as a runner fault, which is
// what it is.
func memberStatesOf(out string) []verify.MemberState {
	ports := verify.SubjectOrder(out)
	sections := verify.SplitSubjects(out)
	states := make([]verify.MemberState, 0, len(ports))
	for i, port := range ports {
		ms := verify.MemberState{Port: port}
		var lines []string
		for line := range strings.Lines(sections[port]) {
			lines = append(lines, strings.TrimSpace(line))
		}
		if len(lines) == 0 {
			states = append(states, ms)
			continue
		}
		switch lines[0] {
		case "passed":
			ms.Outcome = verify.MemberPassed
		case "failed":
			ms.Outcome = verify.MemberFailed
		case "skipped":
			ms.Outcome = verify.MemberSkipped
			if len(lines) > 1 {
				if j, err := strconv.Atoi(lines[1]); err == nil && j >= 0 && j < len(ports) && j != i {
					ms.Prerequisite = ports[j]
				}
			}
		}
		states = append(states, ms)
	}
	return states
}
