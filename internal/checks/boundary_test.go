package checks

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// checksImports are the packages beyond the standard library the
// check-execution service may import, each with why. It drives the
// providers and records in the store; the engine composes it and drives
// it, and it imports neither the engine nor the command layer.
var checksImports = map[string]string{
	"internal/model":             "the records it admits, drives, and settles",
	"internal/store":             "the store's contract, where runs, leases, and results are recorded",
	"internal/coord":             "sessions and leases: a run's driver holds its lease",
	"internal/buildenv":          "the contract the providers meet, which it drives",
	"internal/git":               "the checkout a snapshot is committed in and reuse reads trees of",
	"internal/reuse":             "what each target's build read, and which earlier build stands for it",
	"internal/evidence":          "what a run's records establish, which settles its state",
	"internal/buildlog":          "a failure's cause from its log, and the logs kept compressed",
	"internal/progress":          "what the work reports as a check builds, which its run keeps",
	"internal/refusal":           "why something didn't go: a refused check, and trouble another attempt may clear",
	"internal/prose":             "counts and ages worded as everywhere else",
	"internal/macos":             "macOS releases, for an environment's words and its log directory",
	"internal/macports":          "a port as MacPorts evaluates it, which the port reader returns",
	"internal/macports/portfile": "which subports' blocks hold a Portfile's change, and which inert options, for reuse",
	"internal/macports/fidelity": "whether two revisions' change records find an inert change alone, for reuse",
	"internal/atomicfile":        "kept archives, whole or not at all",
	"internal/failpoint":         "the acceptance harness's kill steps, which a normal build hasn't",
	"golang.org/x/sync/errgroup": "a check's environments building together",
}

// The check-execution service imports what it drives and records with,
// and nothing that composes or drives it: not the engine, not the command
// layer (finding 4's second step). Each import is named with why, and one
// no longer imported is taken off.
func TestTheServiceImportsWhatItDrivesWith(t *testing.T) {
	t.Parallel()
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
			if _, allowed := checksImports[local]; !allowed {
				t.Errorf("%s imports %s: name it and why in checksImports, or leave what needs it to the engine", name, path)
			}
		}
	}
	for local := range checksImports {
		if !used[local] {
			t.Errorf("checksImports names %s, which the service no longer imports; take it off", local)
		}
	}
}
