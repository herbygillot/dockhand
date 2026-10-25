package command

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// commandImports are the packages beyond the standard library the command
// layer may import (doc.go), each with why.
var commandImports = map[string]string{
	"internal/engine": "does the work",
	// The verdict on a branch's state, whose kinds and Next the command
	// renders (v0.5.0 part 4).
	"internal/branchstate": "the verdict on a branch's state, rendered",
	"internal/refusal":     "why something didn't go, its kind and Next, rendered",
	"internal/model":       "the records the engine returns",
	"internal/config":      "the person's settings, turned into the engine's options",
	"internal/coord":       "the command's own session, and who leads",
	"internal/buildinfo":   "the command's own version",
	"internal/progress":    "the reporter each command installs, printing what the work reports to standard error",
	"internal/prose":       "counts and sizes worded as everywhere else",
	"internal/macos":       "the launchd job serve installs, written as dockhand writes every one",
	"internal/store":       "filter types for engine queries; records are read and written through the engine",
	"internal/scratch":     "the edit buffer an editor opens, in the process's run root",
	// Vocabulary the engine's results carry beyond model's: a checksum,
	// and a commit-rule finding, whose explanations explain prints.
	"internal/macports/commitrules": "vocabulary in engine results, and explain's text",
	"internal/compose":              "the engine, its providers, and its GitHub runtime, assembled for the command",
	"internal/buildenv":             "vocabulary: the providers' names, which check's and logs' words say, and what a check changed of the person's, which status says",
	"internal/buildenv/ghactions":   "vocabulary: the branch prefix check's preview names",
	"internal/buildenv/tart":        "setup's, until part 4.2: Tart's images and readiness, and config's Xcodes",
	"internal/github":               "setup's, until part 4.2: the login and its source; and GitHub's pull request URLs, the spend log, and the allowance",
	"internal/credential":           "setup's, until part 4.2: where the login is kept",
	"internal/credential/keychain":  "setup's, until part 4.2: the macOS Keychain",
	"github.com/spf13/cobra":        "the command line itself",
	"internal/term":                 "the streams: terminals, questions, the --json envelope, and exit codes",
	// Where a port's own build begins is the result's (model.OwnBuild);
	// logs --port prints its log from that line, counted as the guest
	// counts it.
	"internal/buildlog": "a log from the line a step of its build began on, for logs --port",
	"internal/macports": "a variant spec read as MacPorts reads one, for check's --variants",
}

const module = "github.com/herbygillot/dockhand/"

// layers are command's packages below its verbs, which import no verb and
// not the root: what renders, follows a check, holds the Mac and the
// settings, and finds a branch (part 4, item 2).
var layers = map[string]bool{"view": true, "checkrun": true, "tidyrun": true, "host": true, "settings": true, "branchflags": true, "cmdtest": true}

// verbImports are the verb packages a verb may import: the two
// composites, ship, whose stages are author's edit, tidy's Decide,
// submit's Apply, and outdated's Batch, and bump, ship's single-port
// front; nothing else reaches across (architecture's answer, 2026-10-22).
// update --mine is ship's edit stage over your outdated ports, outdated's
// Batch. The stages several verbs run are layers, as checkrun and tidyrun
// are, not verbs a verb imports.
var verbImports = map[string][]string{"ship": {"author", "submit", "outdated"}, "bump": {"ship", "author"}, "author": {"outdated"}}

// The command layer parses, finds context, prompts, and renders; the
// engine decides (doc.go). It imports nothing past its layer, and it
// reads and writes records only through the engine, never with the
// store's own transactions. Split into packages, its shape holds too: the
// layers import no verb and not the root, and no verb another but as
// verbImports names (architecture's note on the split, 2026-10-22).
func TestCommandTalksToTheEngine(t *testing.T) {
	t.Parallel()
	files := token.NewFileSet()
	used := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		pkg := filepath.Dir(path)
		file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			if !strings.Contains(strings.Split(imported, "/")[0], ".") {
				continue // the standard library
			}
			local := strings.TrimPrefix(imported, module)
			if within, ok := strings.CutPrefix(local, "internal/command"); ok {
				within = strings.TrimPrefix(within, "/")
				switch {
				case pkg == ".":
				case within == "":
					t.Errorf("%s imports the command line's root: only cmd/dockhand does", path)
				case layers[pkg] && !layers[within]:
					t.Errorf("%s imports %s: a layer below the verbs imports no verb", path, within)
				case !layers[pkg] && !layers[within] && !slices.Contains(verbImports[pkg], within):
					t.Errorf("%s imports %s: a verb imports no other verb, but as verbImports names", path, within)
				}
				continue
			}
			// The tests' harness reaches what the tests do: fakes, the
			// forge's stand-in, testify. Only its shape is the layers'.
			if pkg == "cmdtest" {
				continue
			}
			used[local] = true
			if _, allowed := commandImports[local]; !allowed {
				t.Errorf("%s imports %s: move the decision it serves into the engine, or name the import and why in commandImports", path, imported)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.SelectorExpr)
			if !ok || (call.Sel.Name != "View" && call.Sel.Name != "Update") {
				return true
			}
			if store, ok := call.X.(*ast.SelectorExpr); ok && store.Sel.Name == "Store" {
				t.Errorf("%s: the store's %s: read and write records through the engine", files.Position(call.Pos()), call.Sel.Name)
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	// The list shrinks as command does (the architecture review's X4).
	for local := range commandImports {
		if !used[local] {
			t.Errorf("commandImports names %s, which command no longer imports; take it off", local)
		}
	}
}
