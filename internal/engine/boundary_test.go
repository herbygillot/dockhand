package engine

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// engineImports are the packages beyond the standard library the engine
// may import, each with why. The engine decides and composes the pieces
// that do the work; it is driven by the command layer and drives the
// providers, and imports neither.
var engineImports = map[string]string{
	"internal/model":               "the records it decides about",
	"internal/store":               "the store's contract",
	"internal/store/sqlite":        "the store, opened with the engine",
	"internal/coord":               "sessions and leases for runs and serve",
	"internal/buildenv":            "the contract the providers meet; the command layer composes them",
	"internal/git":                 "the checkout, captures, and history",
	"internal/history":             "tidy, rebase, and restore as complete transitions",
	"internal/reuse":               "what each target's build read, for reuse",
	"golang.org/x/sync/errgroup":   "a check's environments building together",
	"internal/macos":               "macOS releases, for environments and their words",
	"internal/scratch":             "temporary directories for archives and preparation",
	"internal/atomicfile":          "kept archives, whole or not at all",
	"internal/version":             "its own version, in the pull request",
	"internal/archive":             "reading archives, for diff --archive",
	"internal/sourcecompare":       "what upstream's source changed, for update",
	"internal/outdated":            "what outdated reads of upstream",
	"internal/preparation":         "preparing an update's edit",
	"internal/upstream":            "upstream projects' releases",
	"internal/forge":               "the forge's contract: pull requests, releases, tags",
	"internal/forge/github":        "GitHub as a forge",
	"internal/forge/gitlab":        "GitLab as a forge, for releases",
	"internal/github":              "GitHub's client, which the forge and create use",
	"internal/credential/keychain": "the GitHub login, where the forge needs it",
	// MacPorts itself: evaluating Portfiles, and what MacPorts asks.
	"internal/macports":                   "ports as MacPorts evaluates them",
	"internal/macports/eval":              "MacPorts' evaluator",
	"internal/macports/portindex":         "a port index of a revision",
	"internal/macports/selection":         "reading ports from a checkout",
	"internal/macports/portsource":        "the forges a Portfile's upstream conventions name, for preparation",
	"internal/macports/workspace":         "a revision's files for MacPorts to read",
	"internal/macports/portfile":          "Portfile vocabulary",
	"internal/macports/portedit/archives": "a Portfile's archives as MacPorts shipped them, for diff --archive and stealth updates, and MacPorts' mirror",
	"internal/macports/newport":           "new Portfiles, for create",
	"internal/macports/commitmsg":         "commit messages, for tidy",
	"internal/macports/commitrules":       "the commit rules, for tidy, submit, and review",
}

// The engine imports what it decides with, and nothing that drives it or
// that it drives: not the command layer, not a provider, not a VM. Each
// import is named with why, and one no longer imported is taken off (the
// architecture review of 2026-09-27, finding 4).
func TestTheEngineImportsWhatItDecidesWith(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	files := token.NewFileSet()
	used := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			if !strings.Contains(strings.Split(path, "/")[0], ".") {
				continue // the standard library
			}
			local := strings.TrimPrefix(path, "github.com/herbygillot/dockhand/")
			used[local] = true
			if _, allowed := engineImports[local]; !allowed {
				t.Errorf("%s imports %s: name it and why in engineImports, or move what needs it out of the engine", name, path)
			}
		}
	}
	// The list shrinks as the engine does.
	for local := range engineImports {
		if !used[local] {
			t.Errorf("engineImports names %s, which the engine no longer imports; take it off", local)
		}
	}
}
