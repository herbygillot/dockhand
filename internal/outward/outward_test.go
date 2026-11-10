package outward

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every effect against every basis, in your fork, another person's, and
// MacPorts' repository: what's pushed, deleted, canceled, or run again
// never lands in MacPorts', a deletion needs a record that dockhand made
// it, and each effect takes the bases the table names (#128).
func TestEachEffectTakesOnlyItsBases(t *testing.T) {
	t.Parallel()
	bases := []Basis{OwnFork, Made, Invited, Named}
	for effect, allowed := range rules {
		for _, repository := range []string{"ada/macports-ports", "someone/macports-ports", Upstream, "MacPorts/MacPorts-Ports"} {
			for _, basis := range bases {
				err := Permit(effect, repository, basis)
				upstream := strings.EqualFold(repository, Upstream) && slices.Contains(inUpstream, effect)
				if upstream || !slices.Contains(allowed, basis) {
					require.Error(t, err, "%s in %s on %s", effect, repository, basis)
					continue
				}
				require.NoError(t, err, "%s in %s on %s", effect, repository, basis)
			}
		}
	}
	require.Equal(t, []Basis{Made}, rules[DeleteBranch], "a deletion needs a record that dockhand made it")
	require.ErrorContains(t, Permit(PushBranch, Upstream, Invited), "dockhand doesn't push a branch in macports/macports-ports: it never pushes, deletes, cancels, or reruns anything there, whoever's the change is")
	require.ErrorContains(t, Permit(DeleteBranch, "someone/macports-ports", OwnFork), "dockhand doesn't delete a branch in someone/macports-ports on the grounds of your fork")
	require.NoError(t, Permit(EditPullRequest, Upstream, Made), "pull requests live in MacPorts' repository")
}

// The methods that change something remote are called only here, so a
// new remote write can't go around the rules. What implements them, Git
// and the forges, and the test doubles, are theirs to define (#128).
func TestNothingElseCallsARemoteWrite(t *testing.T) {
	t.Parallel()
	// Named alone, these are remote writes wherever they're called.
	writes := []string{"Push", "DeleteRemoteBranch", "MarkDraft", "MarkReady", "PostReview", "RequestReviewers", "RerunFailed"}
	// These names are common, and are remote writes only on the
	// receivers that hold a forge or GitHub Actions.
	onForge := map[string][]string{"Create": {"e.forge()", "e.Forge"}, "Update": {"e.forge()", "e.Forge"}, "Cancel": {"p.API"}, "Rerun": {"p.API"}}
	implementations := []string{"internal/git", "internal/forge", "internal/github", "internal/outward"}
	// Calls of the same names that aren't remote writes: the engine's own
	// review, which posts through here, and a plan's push words.
	notWrites := []string{"e.PostReview", "plan.Push"}
	root := filepath.Join("..", "..")
	files := token.NewFileSet()
	var found []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if slices.ContainsFunc(implementations, func(dir string) bool { return strings.HasPrefix(relative, dir+"/") }) {
			return nil
		}
		parsed, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			var receiver strings.Builder
			_ = printer.Fprint(&receiver, files, selector.X)
			name := selector.Sel.Name
			if receiver.String() == "outward" || slices.Contains(notWrites, receiver.String()+"."+name) {
				return true
			}
			if slices.Contains(writes, name) || slices.Contains(onForge[name], receiver.String()) {
				found = append(found, files.Position(call.Pos()).String()+": "+receiver.String()+"."+name)
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, found, "a remote write outside outward")
}
