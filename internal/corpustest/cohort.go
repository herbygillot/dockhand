package corpustest

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

// The cohort corpus's sidecars. A cohort log is one file holding
// several ports' output, so its expectation is one file holding several
// ports' verdicts — and, unlike the single-subject corpus, the roster
// is part of the expectation rather than a single port name: which
// members were in the change, and in what order the runner built them,
// is half of what the attribution turns on.
//
// It is a second reader and not a generalization of the first. The
// single-subject sidecar states an invariant a cohort's does not have —
// a blamed port exactly when blocked — because a cohort blocked on a
// port outside the change blames a stranger in its detail and no
// subject at all. Folding the two would have meant weakening the
// invariant that holds for every log dockhand has ever settled.

// Member is one subject's expected verdict inside a cohort.
type Member struct {
	// State is the wire word the note settles to: passed, failed,
	// blocked, unsupported or errored.
	State string
	// Detail is the sentence the note carries.
	Detail string
	// Blamed names the SIBLING whose failure this member inherited. It
	// is empty for a member blocked by a port outside the change, whose
	// name rides the detail: Blamed names a subject, and a stranger is
	// not one.
	Blamed string
	// Lint is what this member's own section of the log said, which the
	// note records only on a pass. Stating it per member is most of why
	// this corpus exists: a reader that took the whole file would give
	// every member the first lint line in it.
	Lint string
	// Reported is what the guest's own runner recorded about this
	// member, apart from the log: "passed", "failed", "skipped <member>"
	// naming the prerequisite it was skipped for, or "" for a member the
	// runner wrote no state file for. It is the second record the judge
	// reads, and the one that tells a member skipped on purpose from one
	// the guest never reached — both are silent in the log.
	Reported string
	// Forced names the sibling this member's build deactivated before it
	// ran — the D24 override, recorded on the run at submit and carried
	// through settle. Empty for every ordinary member. It is a member of
	// the cohort (the seated sibling is built), and never the headline.
	Forced string
}

// CohortExpect is a cohort log's .expect sidecar.
type CohortExpect struct {
	// Members are the change's ports in build order, Members[0] the
	// headline.
	Members []string
	// Outcome is what the guest's aggregate state file said: passed or
	// failed. It is one word for the whole cohort because it is one
	// guest.
	Outcome string
	// Verdict is the expected judgment per member, by port. Every
	// member has one; the reader refuses a sidecar that leaves one out,
	// because a member nobody stated an expectation for is exactly the
	// member a regression would settle wrongly in silence.
	Verdict map[string]Member
}

// ReadCohort parses a cohort sidecar: `key: value` lines, with the
// per-member keys spelled `<port>.<field>`.
//
// Strict about keys, about members and about words, on the same
// reasoning as the single-subject reader: a hand-written expectation is
// the part of the corpus a person is stating, and a typo in it must
// fail the sweep by name rather than pass as an empty string.
func ReadCohort(t *testing.T, path string) CohortExpect {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "every cohort log needs its sidecar; see the corpus README.md")
	e := CohortExpect{Verdict: map[string]Member{}}
	seen := map[string]bool{}
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		require.True(t, ok, "%s: %q is not a key: value line", path, line)
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		require.False(t, seen[key], "%s: %s given twice", path, key)
		seen[key] = true
		switch key {
		case "members":
			e.Members = strings.Fields(value)
			require.NotEmpty(t, e.Members, "%s: members is the roster in build order", path)
			for _, m := range e.Members {
				e.Verdict[m] = Member{}
			}
			continue
		case "outcome":
			e.Outcome = value
			continue
		}
		port, field, ok := strings.Cut(key, ".")
		require.True(t, ok, "%s: %q is not members, outcome, or <port>.<field>", path, key)
		require.NotEmpty(t, e.Members, "%s: members must come before %q", path, key)
		v, member := e.Verdict[port]
		require.True(t, member, "%s: %s is not one of the members", path, port)
		switch field {
		case "state":
			v.State = value
		case "detail":
			v.Detail = value
		case "blamed":
			v.Blamed = value
		case "lint":
			v.Lint = value
		case "reported":
			v.Reported = value
		case "forced":
			v.Forced = value
		default:
			require.Failf(t, "unknown sidecar field", "%s: %q; the fields are state, detail, blamed, lint, reported, forced", path, key)
		}
		e.Verdict[port] = v
	}
	require.Contains(t, []string{"passed", "failed"}, e.Outcome, "%s: outcome is the guest's state file", path)
	for _, m := range e.Members {
		v := e.Verdict[m]
		require.Contains(t, []string{"passed", "failed", "blocked", "unsupported", "errored"}, v.State,
			"%s: %s.state", path, m)
		if v.State == "passed" {
			require.Empty(t, v.Detail, "%s: %s passed and carries no detail", path, m)
		}
		if v.Blamed != "" {
			require.Equal(t, "blocked", v.State, "%s: only a blocked member inherits a blame", path, m)
			require.Contains(t, e.Members, v.Blamed, "%s: %s.blamed names a sibling, never a stranger", path, m)
		}
		if word, prereq, _ := strings.Cut(v.Reported, " "); v.Reported != "" {
			require.Contains(t, []string{"passed", "failed", "skipped"}, word,
				"%s: %s.reported is the runner's word: passed, failed, or skipped <member>", path, m)
			if word == "skipped" {
				require.Contains(t, e.Members, prereq, "%s: %s.reported names the member it was skipped for", path, m)
			} else {
				require.Empty(t, prereq, "%s: only a skip names a member", path, m)
			}
		}
		if v.Forced != "" {
			require.Contains(t, e.Members, v.Forced,
				"%s: %s.forced names the seated sibling it was built without, which is a member of the cohort", path, m)
			require.NotEqual(t, e.Members[0], m, "%s: the headline is never a forced member", path, m)
		}
	}
	if e.Outcome == "passed" {
		for _, m := range e.Members {
			require.NotEqual(t, "failed", e.Verdict[m].State,
				"%s: a passing guest disproves nobody", path, m)
			require.NotContains(t, []string{"failed", "skipped"}, e.Verdict[m].Reported,
				"%s: a passing guest recorded no failure and skipped nobody", path, m)
		}
	}
	return e
}

// MemberStates is the runner's record as the provider hands it to the
// settle: one entry per member that has a reported word, in build
// order, a skip carrying the prerequisite it named. Members the sidecar
// gives no word for are left out, which is what a guest that wrote no
// state file for them looks like.
func (e CohortExpect) MemberStates() []verify.MemberState {
	var out []verify.MemberState
	for _, m := range e.Members {
		word, prereq, _ := strings.Cut(e.Verdict[m].Reported, " ")
		ms := verify.MemberState{Port: m}
		switch word {
		case "":
			continue
		case "passed":
			ms.Outcome = verify.MemberPassed
		case "failed":
			ms.Outcome = verify.MemberFailed
		case "skipped":
			ms.Outcome, ms.Prerequisite = verify.MemberSkipped, prereq
		}
		out = append(out, ms)
	}
	return out
}
