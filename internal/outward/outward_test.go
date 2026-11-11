package outward

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every effect against every basis, in your fork, another person's, and
// MacPorts' repository: what's pushed, deleted, canceled, or run again
// never lands in MacPorts', a deletion needs a record that dockhand made
// it, and each effect takes the bases the table names (#128). The table
// is written out here, not read from the rules it checks (#159).
func TestEachEffectTakesOnlyItsBases(t *testing.T) {
	t.Parallel()
	expected := map[Effect][]Basis{
		PushBranch:       {OwnFork, Made, Invited},
		DeleteBranch:     {Made},
		OpenPullRequest:  {OwnFork, Made},
		EditPullRequest:  {Made, Invited, Named},
		DraftPullRequest: {Made, Invited, Named},
		ReadyPullRequest: {Made, Invited, Named},
		AskReviewers:     {Made, Named},
		PostReview:       {Named},
		RerunChecks:      {Made, Named},
		CancelRun:        {OwnFork, Made},
		RerunRun:         {OwnFork, Made},
	}
	neverUpstream := []Effect{PushBranch, DeleteBranch, CancelRun, RerunRun}
	require.ElementsMatch(t, slices.Collect(maps.Keys(expected)), slices.Collect(maps.Keys(rules)), "every effect is in the table")
	bases := []Basis{OwnFork, Made, Invited, Named}
	for effect, allowed := range expected {
		for _, repository := range []string{"ada/macports-ports", "someone/macports-ports", Upstream, "MacPorts/MacPorts-Ports"} {
			for _, basis := range bases {
				err := Permit(effect, repository, basis)
				upstream := strings.EqualFold(repository, Upstream) && slices.Contains(neverUpstream, effect)
				if upstream || !slices.Contains(allowed, basis) {
					require.Error(t, err, "%s in %s on %s", effect, repository, basis)
					continue
				}
				require.NoError(t, err, "%s in %s on %s", effect, repository, basis)
			}
		}
	}
	require.ErrorContains(t, Permit(PushBranch, Upstream, Invited), "dockhand doesn't push a branch in macports/macports-ports: this operation is never allowed in the upstream repository, regardless of ownership")
	require.ErrorContains(t, Permit(DeleteBranch, "someone/macports-ports", OwnFork), "dockhand doesn't delete a branch in someone/macports-ports based only on your fork")
	require.NoError(t, Permit(EditPullRequest, Upstream, Made), "pull requests live in MacPorts' repository")
}

// The methods that change something remote are called only here, so a
// new remote write can't go around the rules. What implements them, Git
// and the forges, and the test doubles, are theirs to define (#128). A
// call is found by the type it's made on, so e.pullRequests().Update is
// one where a list of receivers' names missed it (#159).
func TestNothingElseCallsARemoteWrite(t *testing.T) {
	t.Parallel()
	const module = "github.com/herbygillot/dockhand/"
	implementations := []string{"internal/git", "internal/forge", "internal/github", "internal/outward"}
	files := token.NewFileSet()
	// Each package's export data, from one go list of the build cache,
	// rather than type-checking every import from source, which took a
	// minute.
	listed, err := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}", "../...").Output()
	require.NoError(t, err)
	exports := map[string]string{}
	for line := range strings.Lines(string(listed)) {
		if path, export, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && export != "" {
			exports[path] = export
		}
	}
	imports := importer.ForCompiler(files, "gc", func(path string) (io.ReadCloser, error) {
		export, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(export)
	})
	iface := func(path, name string) *types.Interface {
		pkg, err := imports.Import(module + path)
		require.NoError(t, err)
		return pkg.Scope().Lookup(name).Type().Underlying().(*types.Interface)
	}
	// Each interface whose methods change something remote, by package
	// and name, with those methods: the forge's pull request writes, the
	// GitHub CLI's, GitHub Actions' runs, and Git's pushes.
	type remote struct {
		path, name string
		methods    []string
	}
	remotes := []remote{
		{"internal/outward", "PullRequests", []string{"Create", "Update", "MarkReady", "MarkDraft", "PostReview", "RequestReviewers", "RerunFailed"}},
		{"internal/engine", "GitHubCLI", []string{"MarkReady", "MarkDraft"}},
		{"internal/buildenv/ghactions", "API", []string{"Rerun", "Cancel"}},
	}
	interfaces := make([]*types.Interface, len(remotes))
	for i, r := range remotes {
		interfaces[i] = iface(r.path, r.name)
	}
	writes := func(selection *types.Selection) bool {
		method := selection.Obj()
		if method.Pkg() != nil && method.Pkg().Path() == module+"internal/git" && slices.Contains([]string{"Push", "DeleteRemoteBranch"}, method.Name()) {
			return true
		}
		receiver := selection.Recv()
		for i, r := range remotes {
			if !slices.Contains(r.methods, method.Name()) {
				continue
			}
			// The package being checked has its own copy of its types, so
			// its own interface is matched by name.
			if named, ok := types.Unalias(receiver).(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == module+r.path && named.Obj().Name() == r.name {
				return true
			}
			if types.Implements(receiver, interfaces[i]) || types.Implements(types.NewPointer(receiver), interfaces[i]) {
				return true
			}
		}
		return false
	}
	root := filepath.Join("..", "..")
	packages := map[string][]*ast.File{}
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if slices.ContainsFunc(implementations, func(dir string) bool { return strings.HasPrefix(relative, dir+"/") }) {
			return nil
		}
		// Only the files this build takes, as a platform's own.
		if match, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !match {
			return err
		}
		parsed, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		packages[filepath.Dir(relative)] = append(packages[filepath.Dir(relative)], parsed)
		return nil
	})
	require.NoError(t, err)
	var found []string
	for _, directory := range slices.Sorted(maps.Keys(packages)) {
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		config := types.Config{Importer: imports}
		_, err := config.Check(module+filepath.ToSlash(directory), files, packages[directory], info)
		require.NoError(t, err, directory)
		for _, file := range packages[directory] {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selection, ok := info.Selections[selector]; ok && selection.Kind() == types.MethodVal && writes(selection) {
					found = append(found, files.Position(call.Pos()).String()+": "+selection.Obj().Name())
				}
				return true
			})
		}
	}
	require.Empty(t, found, "a remote write outside outward")
}
