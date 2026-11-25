package remotewrite

import (
	"context"
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

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

type identitySequence struct {
	accounts []string
	reads    int
}

type remoteNames map[string]string

func (r remoteNames) NameFromRemote(remote string) (string, error) {
	name, ok := r[remote]
	if !ok {
		return "", fmt.Errorf("unknown remote %q", remote)
	}
	return name, nil
}

func (s *identitySequence) Current(context.Context) (github.Identity, error) {
	i := min(s.reads, len(s.accounts)-1)
	s.reads++
	return github.Identity{Account: s.accounts[i]}, nil
}

type writeForge struct {
	ready, draft, edit int
	editErr            error
	markedWrite        bool
}

func (f *writeForge) Create(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, nil
}
func (f *writeForge) Update(ctx context.Context, _ forge.PullRequestInput) (forge.PullRequestObservation, error) {
	f.edit++
	f.markedWrite = github.IsWriteRequest(ctx)
	err := f.editErr
	f.editErr = nil
	return forge.PullRequestObservation{}, err
}
func (f *writeForge) MarkReady(context.Context, forge.PullRequestRef) (forge.PullRequestObservation, error) {
	f.ready++
	return forge.PullRequestObservation{}, nil
}
func (f *writeForge) MarkDraft(context.Context, forge.PullRequestRef) (forge.PullRequestObservation, error) {
	f.draft++
	return forge.PullRequestObservation{}, nil
}
func (f *writeForge) PostReview(context.Context, forge.ReviewInput) (string, error) { return "", nil }
func (f *writeForge) RequestReviewers(context.Context, forge.PullRequestRef, []string) error {
	return nil
}
func (f *writeForge) RerunFailed(context.Context, string, int64) error { return nil }

func TestPreviewAccountGuardsEditReadyAndDraft(t *testing.T) {
	watcher := &identitySequence{accounts: []string{"bea"}}
	ctx := ForRemote(ForAccount(t.Context(), "ada", watcher), remoteNames{"local://fork": "ada/macports-ports"})
	_, pushErr := Push(ctx, nil, OwnFork, "ada/macports-ports", git.Push{Remote: "local://fork"})
	require.EqualError(t, pushErr, "the account changed; run it again")
	for _, effect := range []string{"edit", "ready", "draft"} {
		t.Run(effect, func(t *testing.T) {
			watcher := &identitySequence{accounts: []string{"bea"}}
			f := &writeForge{}
			ctx := ForAccount(t.Context(), "ada", watcher)
			var err error
			switch effect {
			case "edit":
				_, err = Edit(ctx, f, Named, forge.PullRequestInput{Repository: "macports/macports-ports"})
			case "ready":
				_, err = Ready(ctx, f, Named, forge.PullRequestRef{Repository: "macports/macports-ports"})
			case "draft":
				_, err = Draft(ctx, f, Named, forge.PullRequestRef{Repository: "macports/macports-ports"})
			}
			require.EqualError(t, err, "the account changed; run it again")
			require.Zero(t, f.edit+f.ready+f.draft, "no write reached the forge")
		})
	}
}

func TestRenewedCredentialRetriesOnlyForThePreviewedAccount(t *testing.T) {
	t.Run("same account", func(t *testing.T) {
		watcher := &identitySequence{accounts: []string{"ada", "ada"}}
		f := &writeForge{editErr: github.ErrCredentialRenewed}
		_, err := Edit(ForAccount(t.Context(), "ada", watcher), f, Named, forge.PullRequestInput{Repository: "macports/macports-ports"})
		require.NoError(t, err)
		require.Equal(t, 2, f.edit)
		require.Equal(t, 2, watcher.reads)
	})
	t.Run("changed account", func(t *testing.T) {
		watcher := &identitySequence{accounts: []string{"ada", "bea"}}
		f := &writeForge{editErr: github.ErrCredentialRenewed}
		_, err := Edit(ForAccount(t.Context(), "ada", watcher), f, Named, forge.PullRequestInput{Repository: "macports/macports-ports"})
		require.EqualError(t, err, "the account changed; run it again")
		require.Equal(t, 1, f.edit)
		require.Equal(t, 2, watcher.reads)
	})
}

func TestWriteWithoutAccountBindingIsRefused(t *testing.T) {
	f := &writeForge{}
	_, err := Edit(t.Context(), f, Named, forge.PullRequestInput{Repository: "macports/macports-ports"})
	require.EqualError(t, err, "remote write requires an account binding")
	require.Zero(t, f.edit)
}

func TestRemotewriteMarksItsWriteContext(t *testing.T) {
	f := &writeForge{}
	watcher := &identitySequence{accounts: []string{"ada"}}
	_, err := Edit(ForAccount(t.Context(), "ada", watcher), f, Named, forge.PullRequestInput{Repository: "macports/macports-ports"})
	require.NoError(t, err)
	require.True(t, f.markedWrite)
}

func TestPushRefusesRemoteThatDoesNotNamePermittedRepository(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "git-started")
	executable := filepath.Join(t.TempDir(), "git")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\ntouch "+marker+"\nexit 1\n")
	repo := &git.Repository{Root: t.TempDir(), Program: git.Program{Executable: executable, Env: testsupport.GitEnv(t)}}
	ctx := ForAccount(t.Context(), "ada", &identitySequence{accounts: []string{"ada"}})
	ctx = ForRemote(ctx, remoteNames{
		"local://fork":     "ada/macports-ports",
		"local://upstream": "macports-ports/macports-ports",
	})
	_, err := Push(ctx, repo, OwnFork, "ada/macports-ports", git.Push{
		Remote: "local://upstream", Branch: "main",
		Commit: strings.Repeat("a", 40),
	})
	require.EqualError(t, err, "remote write to macports-ports/macports-ports does not match permitted repository ada/macports-ports")
	require.NoFileExists(t, marker, "a mismatched remote must be refused before Git starts")
}

func TestPushRefusesRemoteForgeCannotName(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "git-started")
	executable := filepath.Join(t.TempDir(), "git")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\ntouch "+marker+"\nexit 1\n")
	repo := &git.Repository{Root: t.TempDir(), Program: git.Program{Executable: executable, Env: testsupport.GitEnv(t)}}
	ctx := ForAccount(t.Context(), "ada", &identitySequence{accounts: []string{"ada"}})
	ctx = ForRemote(ctx, remoteNames{})
	_, err := Push(ctx, repo, OwnFork, "ada/macports-ports", git.Push{Remote: "local://unrecognized", Branch: "main", Commit: strings.Repeat("a", 40)})
	require.EqualError(t, err, "dockhand doesn't push a branch to an unnamed remote: unknown remote \"local://unrecognized\"")
	require.NoFileExists(t, marker, "a remote the forge cannot name must be refused before Git starts")
}

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
	// The SDK client cannot be kept out of packages outside forge: github.Runtime
	// exposes Client and engine.GitHubClient exposes forge/github.Client. The
	// SDK-backed writes owned here are PullRequests.Create, PullRequests.Edit,
	// PullRequests.CreateReview, PullRequests.RequestReviewers, and
	// Actions.RerunFailedJobsByID. Ready/draft use GitHub's GraphQL endpoint;
	// workflow cancellation uses its REST endpoint directly. Their wrappers
	// live in internal/forge/github and internal/buildenv/ghactions.
	implementations := []string{"internal/remotewrite"}
	files := token.NewFileSet()
	// Each package's export data, from one go list of the build cache,
	// rather than type-checking every import from source, which took a
	// minute.
	listed, err := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}", "../../cmd/...", "../../internal/...").Output()
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
		{"internal/remotewrite", "PullRequests", []string{"Create", "Update", "MarkReady", "MarkDraft", "PostReview", "RequestReviewers", "RerunFailed"}},
		{"internal/engine", "GitHubCLI", []string{"MarkReady", "MarkDraft"}},
		{"internal/buildenv/ghactions", "API", []string{"Rerun", "Cancel"}},
		{"internal/remotewrite", "CLI", []string{"MarkReady", "MarkDraft"}},
		{"internal/remotewrite", "Runs", []string{"Cancel", "Rerun"}},
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
	var buildTaggedFiles []*ast.File
	for _, sourceRoot := range []string{filepath.Join(root, "cmd"), filepath.Join(root, "internal")} {
		err = filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, err error) error {
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
			if match, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil {
				return err
			} else if !match {
				buildTaggedFiles = append(buildTaggedFiles, parsed)
				return nil
			}
			packages[filepath.Dir(relative)] = append(packages[filepath.Dir(relative)], parsed)
			return nil
		})
		if err != nil {
			break
		}
	}
	require.NoError(t, err)
	var found []string
	for _, directory := range slices.Sorted(maps.Keys(packages)) {
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		config := types.Config{Importer: imports}
		_, err := config.Check(module+filepath.ToSlash(directory), files, packages[directory], info)
		require.NoError(t, err, directory)
		for _, file := range packages[directory] {
			ast.Inspect(file, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selection, ok := info.Selections[selector]; ok && writes(selection) {
					found = append(found, files.Position(selector.Pos()).String()+": "+selection.Obj().Name())
				}
				return true
			})
		}
	}
	// Build-tagged files for another host are not type-checked here. Limit
	// the selector-name fallback to receiver identifiers declared as one of
	// the known write interfaces, avoiding unrelated calls such as os.Create.
	interfaceNames := map[string]bool{"PullRequests": true, "CLI": true, "Runs": true, "API": true, "GitHubCLI": true}
	for _, file := range buildTaggedFiles {
		writeReceivers := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				return true
			}
			for _, field := range fn.Type.Params.List {
				typeName, ok := field.Type.(*ast.Ident)
				if ok && interfaceNames[typeName.Name] {
					for _, name := range field.Names {
						writeReceivers[name.Name] = true
					}
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok || !slices.ContainsFunc(remotes, func(remote remote) bool { return slices.Contains(remote.methods, selector.Sel.Name) }) {
				return true
			}
			if receiver, ok := selector.X.(*ast.Ident); ok && writeReceivers[receiver.Name] {
				found = append(found, files.Position(selector.Pos()).String()+": "+selector.Sel.Name+" (build-tagged)")
			}
			return true
		})
	}
	require.Empty(t, found, "a remote write outside remotewrite")
}

// A write refused before anything was sent says so to errors.Is, in the
// check's own words (#345).
func TestAWriteRefusedBeforeSendingSaysNothingWasSent(t *testing.T) {
	t.Parallel()
	ran := false
	err := write(context.Background(), func(context.Context) error { ran = true; return nil })
	require.ErrorIs(t, err, ErrNotSent)
	require.EqualError(t, err, "remote write requires an account binding")
	require.False(t, ran)
}
