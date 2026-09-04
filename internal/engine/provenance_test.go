package engine

// Provenance: what the record remembers about who asked, and the line
// between remembering it and acting on it.
//
// The ruling has two halves and both are here. A run's invoker is
// DECLARED by the caller and written at mint, so an unattended pass's
// work is countable afterwards; and what is written is never read back
// as permission, because a change that could authorize itself by
// claiming its own history would make the machine gate decorative.

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// What the invocation declared is what the record says, both fields and
// both words. The engine is told; it never works it out.
func TestMintRecordsTheDeclaredProvenance(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch, Invoker: record.Machine, Agent: "claude-code"}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, record.Machine, n.AskedBy)
	assert.Equal(t, "claude-code", n.Agent)
}

// An invocation that declared nothing is a person, which is every verb
// somebody typed. The default is safe only because nothing gates on the
// value: it is the common case recorded as itself rather than an empty
// string a later count would have to guess about.
func TestAnUndeclaredMintIsAPersons(t *testing.T) {
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	var out, errb bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errb)

	require.NoError(t, runPlan(t, ctx, eng, bumpPlan(t, repo, "bump", "1.8"),
		Policy{Destination: record.ToBranch}))

	tip, err := repo.RevParse(ctx, "dockhand/jq-1.8")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, record.Human, n.AskedBy)
	assert.Empty(t, n.Agent, "no marker was set, and an absent one is written as absent")
}

// Extending never re-derives provenance from whoever is extending. A
// person adding a commit to an auto-minted branch is a person working
// on the machine's change, not the machine's change becoming theirs —
// and the ladder counts the difference.
func TestExtendDoesNotLaunderProvenance(t *testing.T) {
	ctx := context.Background()
	repo, sha := twoPortRepo(t)
	extendable(t, repo, sha)

	l := ledger.Open(repo)
	n, err := l.Read(ctx, sha)
	require.NoError(t, err)
	n.AskedBy, n.Agent = record.Machine, "claude-code"
	require.NoError(t, l.Write(ctx, n))

	eng := testState(t, repo, &verifytest.Fake{})
	tip, err := eng.Extend(ctx, repo, ExtendRequest{
		Branch:      "dockhand/jq-1.8",
		ExpectedTip: sha,
		Commit:      oneFile("textproc/oniguruma/Portfile", "revision 1\n", "oniguruma: bump revision"),
		Subjects: []record.Subject{{Port: "oniguruma", Names: []string{"oniguruma"},
			Portdir: "textproc/oniguruma", Intent: "bump-revision", Target: "rev1"}},
	})
	require.NoError(t, err)

	grown, err := l.Read(ctx, tip)
	require.NoError(t, err)
	assert.Equal(t, record.Machine, grown.AskedBy)
	assert.Equal(t, "claude-code", grown.Agent)
}

// The line itself: the gate reads the invoker it was PASSED and never
// the one the record carries. Both directions are asserted, because
// only having both rules out a gate that happens to agree with the
// record on the cases a test tried.
//
// The second case is the dangerous one. A record minted unattended,
// carrying an unanswered proposal, publishes when a person asks —
// because a person is looking at the advisory and publishing anyway is
// their answer. A gate that read AskedBy would refuse them over a
// question they had just read.
func TestTheGateReadsTheInvokerAndNotTheRecord(t *testing.T) {
	open := []record.Finding{{Kind: "dependent-revbump", Disposition: record.Proposed}}
	const branch = "dockhand/jq-1.8"

	askedByAMachine := record.Record{AskedBy: record.Machine, Agent: "claude-code", Findings: open}
	require.NoError(t, GateMachinePublish(askedByAMachine, branch, record.Human),
		"a person is publishing; what the note remembers about who asked is not who is asking")

	askedByAPerson := record.Record{AskedBy: record.Human, Findings: open}
	err := GateMachinePublish(askedByAPerson, branch, record.Machine)
	require.Error(t, err, "the unattended road is refused whoever queued the change")
	assert.Equal(t, exitcode.MachineGate, exitcode.TwinOf(err).Code)
}

// And the same property structurally, because the behavioral test above
// can only cover the gates that exist today. AskedBy and Agent are
// written at mint, carried at extend, COPIED ONTO THE AUDIT ROW, and
// named nowhere else in the engine — so an occurrence outside those
// files is either a gate being fed provenance or the beginning of one.
//
// THE AUDIT ROW IS THE ONE EXCEPTION AND IT IS NARROW. A publication has
// two provenances and they part company on exactly the shape the machine
// road exists to produce: a person queues a change and an unattended
// pass puts it out. A row that recorded the publisher as the asker would
// invert the trust ladder's own numerator, so the record's answer is
// carried onto the row — by the publisher, which holds the record
// already, into a field, which is all Publish then writes.
//
// It is an exception to WHERE the field may be named and not to what the
// rule protects. The rule is that provenance is never an input to a
// decision, and the assertions below say so directly: the two permitted
// sites are pinned to one line each, and no line anywhere may hand
// either field to a gate.
//
// Comment lines are skipped so that the rule can be named where it is
// explained, and test files so that this file can state it.
func TestProvenanceIsWrittenInTwoPlacesAndReadInNone(t *testing.T) {
	provenance := regexp.MustCompile(`\.AskedBy|\.Agent\b`)
	// mint bears the record and extend carries it onto the new tip.
	writers := map[string]bool{"mint.go": true, "extend.go": true}
	// The audit row's two files, each allowed the exact lines named
	// below and no others.
	audit := map[string]*regexp.Regexp{
		// The publisher, copying the record's answer into the value it is
		// about to hand to Publish.
		"promote.go": regexp.MustCompile(`^\s*AskedBy: n\.AskedBy\}$`),
		// The field it lands in, and the row it is written to.
		"publish.go": regexp.MustCompile(`^\s*(AskedBy record\.Driver|AskedBy:\s+askedOr\(p\.AskedBy, p\.Invoker\),)$`),
	}
	// Nothing, anywhere, may pass provenance to a gate. This is the
	// property the file-level rule above exists to serve, asserted over
	// every file including the ones the rule excuses.
	fed := regexp.MustCompile(`Gate\w*\([^)]*\.(AskedBy|Agent)`)

	require.NoError(t, filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		allowed, excused := audit[base]
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			assert.NotRegexp(t, fed, line,
				"%s:%d: provenance is recorded, never consulted", path, i+1)
			switch {
			case writers[base]:
			case excused:
				if provenance.MatchString(line) {
					assert.Regexp(t, allowed, line,
						"%s:%d: the audit row's exception is one line, and this is not it", path, i+1)
				}
			default:
				assert.NotRegexp(t, provenance, line,
					"%s:%d: provenance is recorded, never consulted", path, i+1)
			}
		}
		return nil
	}))
}
