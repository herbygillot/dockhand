package render

// The follow-up draft, and the property that makes drafting it safe.
//
// A ping spends somebody else's attention, which is the ring dockhand
// never spends unattended. The draft is how that constraint is kept
// useful rather than merely obeyed: the reader gets the words, and the
// tool keeps no way to say them.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// The draft names the pull request, its age, and what the tier makes of
// the elapsed window — and says, in the line itself, that nothing sent
// it. The sentence a reader skims has to carry that: a line that read
// like a report of an action taken would be a lie told once per pass.
func TestTheFollowUpIsDraftedAndSaysNobodySentIt(t *testing.T) {
	n := noted(record.Passed)
	n.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}}}
	draft := FollowUpDraft(openPR("dockhand/jq-1.8", 5*24*time.Hour, TierOpenmaintainer, n), attentionClock)
	require.NotEmpty(t, draft)
	assert.Equal(t, `follow-up draft — dockhand cannot send this; macports-dev or the PR: `+
		`"PR #77 (jq) has been open 5d; the 72-hour review window elapsed 2d ago. `+
		`The port is openmaintainer, so a minor update may proceed once the window has passed."`, draft)
}

// Each tier says something different about what the elapsed window
// permits, because that is the whole of what the tier is read for. An
// unknown tier says nothing at all rather than guessing at a port's
// maintenance in front of the people who maintain it.
func TestTheDraftSaysWhatTheTierMakesOfTheWindow(t *testing.T) {
	for _, c := range []struct {
		tier Tier
		says string
	}{
		{TierNomaintainer, "nobody to review it"},
		{TierOpenmaintainer, "minor update may proceed"},
		{TierMaintained, "documents the timeout"},
	} {
		t.Run(string(c.tier), func(t *testing.T) {
			assert.Contains(t, FollowUpDraft(openPR("b", 100*time.Hour, c.tier, nil), attentionClock), c.says)
		})
	}
	bare := FollowUpDraft(openPR("b", 100*time.Hour, TierUnknown, nil), attentionClock)
	assert.NotContains(t, bare, "port is")
	assert.Contains(t, bare, "review window elapsed")
}

// Nothing is drafted while the window is still running. The follow-up
// is what an expired window earns; sending one inside the window would
// be asking a maintainer to hurry through time the policy gave them.
func TestNothingIsDraftedInsideTheWindow(t *testing.T) {
	assert.Empty(t, FollowUpDraft(openPR("b", 71*time.Hour, TierMaintained, nil), attentionClock))
	assert.Empty(t, FollowUpDraft(BranchReport{Note: noted(record.Passed)}, attentionClock))
}

// RULING: drafted follow-up text is rendered and never sent — and the
// forge client keeps having no way to send it.
//
// The draft is the reason this is a test and not a comment. Rendering
// the words a person would send puts one small step between dockhand
// and sending them, and the step that has to stay is the absence of a
// method: no comment, no ping, no review, no merge, no close. This
// enumerates internal/gh's exported surface, so a method added for any
// reason at all fails here and is read by somebody.
//
// It lives beside the draft rather than in the forge client's own tests
// because it is the draft's precondition. A reader who finds this
// failing is being told that the line above it stopped being safe.
func TestTheForgeClientHasNoWayToSendAFollowUp(t *testing.T) {
	// The surface as it stands. A method the client gains has to be added
	// here on purpose, which is the point: widening a forge client is a
	// decision, and this is where it gets made visible.
	allowed := map[string]bool{
		"OpenPortPRs": true, "UpstreamRepo": true, "OwnerRepoFromURL": true,
		"RealGhOut": true, "LookupPR": true, "QueryPR": true, "ForkRemote": true,
		"MarshalJSON": true,
	}
	entries, err := os.ReadDir(filepath.Join("..", "gh"))
	require.NoError(t, err)
	fset := token.NewFileSet()
	var surface []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join("..", "gh", name), nil, 0)
		require.NoError(t, perr)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			surface = append(surface, fn.Name.Name)
			assert.True(t, allowed[fn.Name.Name],
				"internal/gh gained the exported %s; a forge client that can write to a pull request "+
					"is what keeps a drafted follow-up from being a sent one", fn.Name.Name)
		}
	}
	require.NotEmpty(t, surface, "the scan found no exported functions, so it is checking nothing")
	for _, forbidden := range []string{"Comment", "Ping", "Review", "Merge", "Close", "Notify"} {
		for _, name := range surface {
			assert.NotContains(t, name, forbidden,
				"internal/gh must keep no way to spend somebody else's attention")
		}
	}
}

// And no argument vector in the layers that spend builds one. The
// client's seam takes raw argv, so the absence of a method is only half
// the property: `gh pr comment` is three strings away from any file
// that holds a Runner, and the engine holds one on every road.
//
// Scoped to the forge client and the engine on purpose. The composition
// root names these same verbs in order to REFUSE them — the runner it
// wires under an unattended invoker is a deny list, and a deny list is
// the opposite of a violation. Scanning the layer that guards alongside
// the layers that spend would make this test fail on its own
// enforcement, so it reads exactly the two packages from which a call
// could actually reach GitHub.
func TestNoArgumentVectorInTheSpendingLayersCommentsOnAPullRequest(t *testing.T) {
	var found []string
	for _, pkg := range []string{"gh", "engine"} {
		require.NoError(t, filepath.Walk(filepath.Join("..", pkg), func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, line := range strings.Split(string(b), "\n") {
				code, _, _ := strings.Cut(line, "//")
				for _, verb := range []string{`"comment"`, `"review"`, `"merge"`, `"close"`} {
					if strings.Contains(code, `"pr"`) && strings.Contains(code, verb) {
						found = append(found, path+": "+strings.TrimSpace(line))
					}
				}
			}
			return nil
		}))
	}
	assert.Empty(t, found, "a pull request write verb was assembled as argv, which no method-level check would catch")
}
