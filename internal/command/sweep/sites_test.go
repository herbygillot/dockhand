package sweep

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// effectSites are the call sites of the effects the sweep fails, read from
// the source: each function outside the store and the sweep that runs a
// store transaction (Update, or the store's own helper over it,
// store.Recorded, given a function taking a store.Tx) or
// registers a repository, each that calls the remote-write rule's
// functions, and each that calls a git function that can run a writing
// git command (gitWriters), by the kind of effect, as an address names its site
// ("engine.(*Engine).publish"). The call-site listing holds each to a
// scenario that reaches it or the issue that owes one (docs/designs/
// fault-sweep.md, "What makes it systematic").
func effectSites(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	sites := map[string]string{}
	writers := gitWriters(t)
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (path == filepath.Join(root, "store") || filepath.Dir(path) == filepath.Join(root, "store") || path == filepath.Join(root, "command", "sweep")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		// The git package's own calls, and the remote-write rule's, are
		// inside the boundaries: their callers are the sites.
		dir := filepath.Dir(path)
		local := dir == filepath.Join(root, "git") || dir == filepath.Join(root, "remotewrite")
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			site := siteName(file.Name.Name, fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "remotewrite" && selector.Sel.Name != "Permit" {
					sites["remote@"+site] = "remotewrite." + selector.Sel.Name
					return true
				}
				// A git write, called with a context first, as every git
				// method is: a method named like one that takes none
				// (archive's Tops.Add) isn't. A push is a remote write,
				// as the clean runs address it.
				if writers[selector.Sel.Name] && !local && len(call.Args) > 0 && isContext(call.Args[0]) {
					kind := "git@"
					if selector.Sel.Name == "Push" || selector.Sel.Name == "DeleteRemoteBranch" {
						kind = "remote@"
					}
					sites[kind+site] = "git." + selector.Sel.Name
					return true
				}
				switch {
				// A transaction: Update, or a helper over it, as
				// store.Recorded, whose last argument takes a store.Tx.
				case (selector.Sel.Name == "Update" || isIdent(selector.X, "store")) && slices.ContainsFunc(call.Args, takesTx):
					sites["store.update@"+site] = "a store transaction"
				// A helper's own Update of a transaction it was handed.
				case selector.Sel.Name == "Update" && len(call.Args) == 3 && isIdent(call.Args[0], "ctx") && isTxParameter(fn, call.Args[2]):
					sites["store.update@"+site] = "a store transaction handed in"
				case selector.Sel.Name == "Register" && len(call.Args) == 2 && isIdent(call.Args[0], "ctx"):
					sites["store.register@"+site] = "a repository registered"
				}
				return true
			})
		}
		return nil
	}))
	return sites
}

// gitWriters are the git package's functions that can run a command the
// classing test calls a write (gitWrite), by name: those whose own git
// command lines, read with each argument that isn't a literal as a
// positional one, gitWrite counts, and those that call one of them.
func gitWriters(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "git", "*.go"))
	require.NoError(t, err)
	writes, calls := map[string]bool{}, map[string][]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					switch callee := n.Fun.(type) {
					case *ast.SelectorExpr:
						calls[name] = append(calls[name], callee.Sel.Name)
					case *ast.Ident:
						calls[name] = append(calls[name], callee.Name)
					}
					if gitWrite(literals(n.Args)) {
						writes[name] = true
					}
				// A command line built as a slice, []string{"add", ...}.
				case *ast.CompositeLit:
					if gitWrite(literals(n.Elts)) {
						writes[name] = true
					}
				}
				return true
			})
		}
	}
	for changed := true; changed; {
		changed = false
		for name, callees := range calls {
			if !writes[name] && slices.ContainsFunc(callees, func(callee string) bool { return writes[callee] }) {
				writes[name], changed = true, true
			}
		}
	}
	return writes
}

// literals are a call's or a slice's arguments as a git command line,
// from its first string literal on, each that isn't one read as "?".
func literals(args []ast.Expr) []string {
	var line []string
	for _, arg := range args {
		lit, ok := arg.(*ast.BasicLit)
		switch {
		case ok && lit.Kind == token.STRING:
			line = append(line, strings.Trim(lit.Value, "\"`"))
		case len(line) > 0:
			line = append(line, "?")
		}
	}
	return line
}

// isContext reports whether an argument is a context by its name: ctx, or
// a call or field that makes or holds one.
func isContext(arg ast.Expr) bool {
	switch arg := arg.(type) {
	case *ast.Ident:
		return arg.Name == "ctx" || strings.HasSuffix(arg.Name, "Ctx")
	case *ast.CallExpr:
		return isContext(arg.Fun)
	case *ast.SelectorExpr:
		return strings.HasSuffix(arg.Sel.Name, "Context") || isIdent(arg.X, "context")
	}
	return false
}

// siteName is a function as a call site names it: its package's last
// element and its function, a method by its receiver.
func siteName(pkg string, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return pkg + "." + fn.Name.Name
	}
	switch receiver := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := receiver.X.(*ast.Ident); ok {
			return pkg + ".(*" + id.Name + ")." + fn.Name.Name
		}
	case *ast.Ident:
		return pkg + "." + receiver.Name + "." + fn.Name.Name
	}
	return pkg + "." + fn.Name.Name
}

func takesTx(arg ast.Expr) bool {
	lit, ok := arg.(*ast.FuncLit)
	if !ok || len(lit.Type.Params.List) != 1 {
		return false
	}
	tx, ok := lit.Type.Params.List[0].Type.(*ast.SelectorExpr)
	return ok && tx.Sel.Name == "Tx" && isIdent(tx.X, "store")
}

// isTxParameter reports whether an argument is a parameter of the
// function that takes a store.Tx.
func isTxParameter(fn *ast.FuncDecl, arg ast.Expr) bool {
	id, ok := arg.(*ast.Ident)
	if !ok {
		return false
	}
	for _, field := range fn.Type.Params.List {
		kind, ok := field.Type.(*ast.FuncType)
		if !ok || len(kind.Params.List) != 1 {
			continue
		}
		tx, ok := kind.Params.List[0].Type.(*ast.SelectorExpr)
		if !ok || tx.Sel.Name != "Tx" || !isIdent(tx.X, "store") {
			continue
		}
		for _, name := range field.Names {
			if name.Name == id.Name {
				return true
			}
		}
	}
	return false
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// closure is a function literal's suffix on its enclosing function's
// name, as the runtime names it: applyTidy.func2.
var closure = regexp.MustCompile(`(\.func\d+)+(\.\d+)?$`)

// owed are the effect call sites no scenario reaches yet, each with the
// issue whose scenario owes it, or why none can: the listing fails where
// a site is neither reached nor here, and where one here is reached, so
// the list only shrinks (#129's fix list, 2).
var owed = map[string]string{
	"store.update@engine.(*Engine).AdoptPullRequest":    "#129: the adopt scenario, review's adoption",
	"store.update@engine.(*Engine).Create":              "#129: create's scenario",
	"store.update@engine.(*Engine).RenameArchived":      "#129: the rename scenario, an archived branch's name taken back",
	"store.update@engine.(*Engine).RenamePort":          "#129: the rename scenario",
	"store.update@engine.(*Engine).Start":               "#129: start's scenario; every scenario's arrangement runs start unswept",
	"store.update@engine.(*Engine).markTried":           "#191: serve's opening tried, group C",
	"store.update@engine.(*Engine).renameForVersion":    "#194: an update naming its branch for the version it finds",
	"remote@engine.(*Engine).PostReview":                "#129: the review scenario",
	"remote@engine.(*Engine).Ready":                     "#191: draft and ready, group B",
	"remote@engine.(*Engine).RerunCI":                   "#129: a pull request's failed CI run again, waiting on the person's word on rule 1, which the ownership check follows and remotewrite's RerunChecks doesn't",
	"remote@engine.(*Engine).draftFirst":                "#191: draft and ready, group B, the GitHub CLI's way",
	"remote@engine.(*Engine).readyByCLI":                "#191: draft and ready, group B, the GitHub CLI's way",
	"remote@ghactions.(*Provider).Execute":              "#193: a GitHub check launched and canceled",
	"remote@ghactions.(*Provider).Stop":                 "#193: a GitHub check stopped",
	"remote@ghactions.(*Provider).Settle":               "#241: killed before Settle, killed after the delete and before it is noted, delete refused",
	"remote@ghactions.(*Provider).RemoveLeftover":       "#241: clean after killed before Settle, killed after the delete and before it is noted, delete refused",
	"remote@ghactions.(*Provider).removeBranch":         "#193: a GitHub check's branch removed",
	"store.update@checks.(*Archives).Keep":              "#193: a check's kept archives",
	"store.update@checks.(*Archives).Prune":             "#193: a check's kept archives pruned",
	"store.update@checks.(*Service).RequestCancel":      "#193: a cancel",
	"store.update@checks.(*Service).Reserve":            "#193: a check's slot",
	"store.update@checks.(*Service).Retry":              "#193: a retry",
	"store.update@checks.(*Service).SayHeld":            "#193: a check held",
	"store.update@checks.(*Service).drive":              "#193: a check driven",
	"store.update@checks.(*Service).enqueue":            "#193: a check queued",
	"store.update@coord.(*Session).Acquire":             "#193: a check's lease taken",
	"store.update@coord.(*Session).Beat":                "excluded: a heartbeat, which the worlds' session interval (heartbeat, 90 seconds) keeps out of a run, as the design's effect addresses say, unless a scenario is about one",
	"store.update@coord.(*Session).Fenced":              "#193: a write under a check's lease",
	"store.update@coord.(*Session).Release":             "#193: a check's lease released",
	"store.update@engine.(*Engine).Adopt":               "#129: the adopt scenario",
	"store.update@engine.(*Engine).AllowEvaluation":     "#129: the adopt scenario, the person's yes to evaluating",
	"store.update@engine.(*Engine).Archive":             "#129: the archive scenario",
	"store.update@engine.(*Engine).Decide":              "#129: decide's scenario",
	"store.update@engine.(*Engine).Forget":              "#129: clean --delete",
	"store.update@engine.(*Engine).KeepForSubmit":       "#191: a continuation that leaves the flags out, group E",
	"store.update@engine.(*Engine).KeepNote":            "#191: a continuation that leaves the flags out, group E",
	"store.update@engine.(*Engine).Ready":               "#191: draft and ready, group B",
	"store.update@engine.(*Engine).RecordReview":        "#129: the review scenario",
	"store.update@engine.(*Engine).RefreshAdopted":      "#129: the adopt scenario, the author's push followed",
	"store.update@engine.(*Engine).RemoveLeftovers":     "#193: what checks left, removed by clean, by cleanup, and by a check waiting for a Tart slot (#239), which owes killed between judging and removing, and killed after removing before counting again",
	"store.update@engine.(*Engine).answeredCreate":      "#191: serve's opening charged or released, group C",
	"store.update@engine.(*Engine).carryRename":         "#129: a branch renamed with Git",
	"store.update@engine.(*Engine).checkOutAgain":       "#129: a worktree clean removed, checked out again",
	"store.update@engine.(*Engine).cleanup":             "#193: automatic cleanup of what checks left",
	"store.update@engine.(*Engine).holdBeforeCheck":     "#193: ship holding before a check",
	"store.update@engine.(*Engine).keepExcepted":        "#194: the dependents an update leaves out",
	"store.update@engine.(*Engine).moveCreated":         "#129: create's scenario, a new port moved",
	"store.update@engine.(*Engine).placeWorktree":       "#129: the adopt scenario, its worktree",
	"store.update@engine.(*Engine).rebase":              "#129: the rebase scenario",
	"store.update@engine.(*Engine).recordHandEdit":      "#194: a hand edit's attempt",
	"store.update@engine.(*Engine).recordRevision":      "#194: a revision recorded where it wasn't",
	"store.update@engine.(*Engine).refresh":             "#191: status --refresh as the recovery, group A",
	"store.update@engine.(*Engine).revisionAssessments": "#194: a revision's assessments",
	"store.update@engine.(*Engine).revisionChanges":     "#194: a revision's change records",
	"store.update@engine.(*Engine).serveFirstPass":      "#191: serve's count over an upgrade, group C",
	"store.update@engine.(*Engine).settleTriedRows":     "#191: serve's continuation and count, group C",
	"store.update@engine.(*Engine).shellRunning":        "#193: a shell entering a running check",

	// Local git writes (#129's second round, 2).
	"git@buildcmd.(*Provider).Execute":              "#193: a check's commit bundled for its builder",
	"git@checks.(*Service).revisionCommit":          "#193: a check's revision commit kept under a ref",
	"git@engine.(*Engine).AdoptPullRequest":         "#129: the adopt scenario, review's adoption",
	"git@engine.(*Engine).ArchiveBranch":            "#129: the archive scenario",
	"git@engine.(*Engine).Create":                   "#129: create's scenario",
	"git@engine.(*Engine).Forget":                   "#129: clean --delete",
	"git@engine.(*Engine).OpenReview":               "#129: the review scenario, its pull request fetched",
	"git@engine.(*Engine).RefreshAdopted":           "#129: the adopt scenario, the author's push followed",
	"git@engine.(*Engine).RenameArchived":           "#129: the rename scenario, an archived branch's name taken back",
	"git@engine.(*Engine).RenamePort":               "#129: the rename scenario",
	"git@engine.(*Engine).Start":                    "#129: start's scenario; every scenario's arrangement runs start unswept",
	"git@engine.(*Engine).StartAtHead":              "#129: start's scenario, a branch started at HEAD",
	"git@engine.(*Engine).checkOutAgain":            "#129: a worktree clean removed, checked out again",
	"git@engine.(*Engine).moveCreated":              "#129: create's scenario, a new port moved",
	"git@engine.(*Engine).narrow":                   "#129: a tidy whose change narrows the worktree's sparse cone",
	"git@engine.(*Engine).placeWorktree":            "#129: the adopt scenario, its worktree",
	"git@engine.(*Engine).rebase":                   "#129: the rebase scenario",
	"git@engine.(*Engine).renameForVersion":         "#194: an update naming its branch for the version it finds",
	"git@history.(*Transitions).dropCheckpointRefs": "#129: a checkpoint left prepared and abandoned, reached only in recovery after a stop, which the listing's clean runs never make",
	"git@history.(*Transitions).finishIndex":        "#129: a checkpoint left prepared whose change landed, reached only in recovery after a stop",
	"git@history.(*Transitions).finishRestoreIndex": "#129: a restore left unrecorded, reached only in recovery after a stop",
}

// Every effect call site is reached by some scenario's clean run, or owed
// by an issue with its reason (docs/designs/fault-sweep.md, "What makes
// it systematic"; #129's fix list, 2): addresses were taken from each
// clean run alone, so a site only a state no scenario arranges reached
// was never failed, and nothing said so.
func TestEveryEffectCallSiteIsReachedOrOwed(t *testing.T) {
	t.Parallel()
	sites := effectSites(t)
	require.NotEmpty(t, sites)
	reached := map[string]bool{}
	var mu sync.Mutex
	// The listing is read once every scenario's clean run has ended.
	t.Cleanup(func() { judgeSites(t, sites, reached) })
	for _, sc := range scenarios() {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			r := runOnce(t, sc, nil)
			mu.Lock()
			defer mu.Unlock()
			for _, e := range r.effects {
				site := closure.ReplaceAllString(e.Site, "")
				switch {
				case strings.HasPrefix(e.Boundary, "store."):
					reached[e.Boundary+"@"+site] = true
				case strings.HasPrefix(e.Boundary, "forge."), e.Boundary == "git push":
					reached["remote@"+site] = true
				case strings.HasPrefix(e.Boundary, "git "):
					reached["git@"+site] = true
				}
			}
		})
	}
}

// judgeSites holds the call sites to the clean runs and owed.
func judgeSites(t *testing.T, sites map[string]string, reached map[string]bool) {
	var unreached, stale []string
	for site := range sites {
		if _, isOwed := owed[site]; !reached[site] && !isOwed {
			unreached = append(unreached, site+" ("+sites[site]+")")
		}
	}
	for site, why := range owed {
		switch {
		case sites[site] == "":
			stale = append(stale, site+": no such effect call site")
		case reached[site]:
			stale = append(stale, site+": a scenario reaches it now; take it off owed")
		case !regexp.MustCompile(`#\d+`).MatchString(why) && !strings.HasPrefix(why, "excluded: "):
			stale = append(stale, site+": owed names no issue and no exclusion: "+why)
		}
	}
	slices.Sort(unreached)
	slices.Sort(stale)
	assert.Empty(t, unreached, "effect call sites no scenario reaches and owed doesn't list")
	assert.Empty(t, stale)
	t.Logf("%d effect call sites: %d reached, %d owed", len(sites), len(sites)-len(owed), len(owed))
	for _, site := range slices.Sorted(maps.Keys(reached)) {
		if sites[site] != "" {
			t.Logf("reached: %s", site)
		}
	}
}

// The inventory takes in the call sites of every git command the classing
// test calls a write, as the design's effects do: a ref write only
// recovery reaches, as restore's or dropCheckpointRefs', is the kind of
// site the listing exists for (#129's second round, 2).
func TestConfirm129LocalGitSitesAreInventoried(t *testing.T) {
	t.Parallel()
	sites := effectSites(t)
	for _, site := range []string{"git@history.(*Transitions).dropCheckpointRefs", "git@engine.(*Engine).restore"} {
		require.Contains(t, sites, site)
	}
	require.NotContains(t, sites, "git@archive.(*Tops).Add", "a method named like a git write, taking no context, isn't one")
	writers := gitWriters(t)
	require.True(t, writers["UpdateRefs"] && writers["Add"] && writers["RemoveWorktree"], "%v", writers)
	require.False(t, writers["AllCheckouts"] || writers["CurrentBranch"] || writers["Unattended"], "worktree list, symbolic-ref --quiet HEAD and config --get read")
}
