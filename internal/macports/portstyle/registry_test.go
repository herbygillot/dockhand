package portstyle

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// setupSkiplist names X.setup procs that deliberately have no style entry:
// variant machinery with no version parameter, sub-setups that version
// something other than the port, and toolless setups.
var setupSkiplist = map[string]string{
	"compilers.setup":          "variant machinery, no version",
	"compilers.setup_variants": "variant machinery",
	"mpi.setup":                "variant machinery, no version",
	"mpi.setup_variants":       "variant machinery",
	"linalg.setup":             "variant machinery, no version",
	"elisp.setup":              "no parameters",
	"crossgcc.setup_libc":      "versions the libc, not the port",
}

// TestStyleTableCoversPortgroupRegistry keeps the style table honest
// against the complete PortGroup registry in the fixtures: every X.setup
// proc with a version-shaped parameter must appear in versionStyles at the
// parameter's position, or be explicitly skiplisted. A fixture refresh
// that introduces a new PortGroup fails here instead of silently aging the
// table.
func TestStyleTableCoversPortgroupRegistry(t *testing.T) {
	tabled := map[string]int{}
	for _, vs := range versionStyles {
		tabled[vs.command] = vs.word
	}

	checked := 0
	for _, file := range testenv.Portgroups(t) {
		src := testenv.Portgroup(t, file)
		tree, errs := syntax.Parse(src)
		require.Empty(t, errs, file)

		for name, params := range setupProcs(src, tree) {
			pos := versionParam(params)
			if pos == 0 {
				require.NotContains(t, tabled, name,
					"%s: tabled but has no version-shaped parameter", name)
				continue
			}
			if _, skip := setupSkiplist[name]; skip {
				continue
			}
			checked++
			require.Contains(t, tabled, name,
				"%s (%s): setup proc with version parameter %q at word %d is not in the style table",
				name, file, params[pos-1], pos)
			require.Equal(t, pos, tabled[name],
				"%s: table word index disagrees with proc signature %v", name, params)
		}
	}
	require.Greater(t, checked, 15, "registry scan found suspiciously few setup procs")
}

// setupProcs finds top-level `proc X.setup* {params} {...}` definitions and
// returns their parameter names.
func setupProcs(src []byte, tree *syntax.Script) map[string][]string {
	out := map[string][]string{}
	for _, it := range tree.Items {
		cmd, ok := it.(syntax.Command)
		if !ok {
			continue
		}
		if name, ok := cmd.Name(src); !ok || name != "proc" || len(cmd.Words) < 3 {
			continue
		}
		procName := cmd.Words[1].Span.Text(src)
		if !strings.Contains(procName, ".setup") {
			continue
		}
		braced, ok := cmd.Words[2].Segments[0].(syntax.Braced)
		if !ok {
			continue
		}
		elems, errs := syntax.SplitList(src, braced.Body)
		if len(errs) != 0 {
			continue
		}
		var params []string
		for _, e := range elems {
			raw := e.Text(src)
			// A defaulted parameter is {name default}; its name is the
			// first word of the element's value.
			val := syntax.ListValue(raw)
			fields := strings.Fields(val)
			if len(fields) > 0 {
				params = append(params, fields[0])
			}
		}
		out[procName] = params
	}
	return out
}

// versionParam returns the 1-based position of the version-shaped
// parameter, or 0 if none: a name that is, or ends in, version/vers.
func versionParam(params []string) int {
	for i, p := range params {
		lower := strings.ToLower(p)
		if lower == "version" || lower == "vers" ||
			strings.HasSuffix(lower, "_version") || strings.HasSuffix(lower, "portversion") {
			return i + 1
		}
	}
	return 0
}
