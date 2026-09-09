package bump

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/checksums/rewrite"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// TestRederiveProbe asks the last open question about re-deriving another
// frame's digests: can the literals inside a checksums command that THIS
// evaluation never took be located and rewritten? It ships nothing.
//
//	DOCKHAND_REDERIVE_PORTFILE=/path/to/Portfile DOCKHAND_REDERIVE_PORT=gh \
//	  go test ./internal/intent/bump/ -run TestRederiveProbe -v
func TestRederiveProbe(t *testing.T) {
	path, name := os.Getenv("DOCKHAND_REDERIVE_PORTFILE"), os.Getenv("DOCKHAND_REDERIVE_PORT")
	if path == "" || name == "" {
		t.Skip("set DOCKHAND_REDERIVE_PORTFILE and DOCKHAND_REDERIVE_PORT to run")
	}
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	cst, perrs := syntax.Parse(src)
	require.Empty(t, perrs, "the Portfile must parse cleanly")

	// Every checksums command the parser can see in this context,
	// including the branches an evaluation would not take.
	var blocks []syntax.Command
	for cmd := range cst.Commands(src, portstyle.ScopeOf(src, name)) {
		if n, ok := cmd.Name(src); ok && (n == "checksums" || n == "checksums-append") {
			blocks = append(blocks, cmd)
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s: %d checksums command(s) in scope\n", name, len(blocks))

	for i, cmd := range blocks {
		var toks []string
		for _, w := range cmd.Words[1:] {
			lit, isLit := w.Literal(src)
			if !isLit {
				lit = "<" + w.Span.Text(src) + ">"
			}
			toks = append(toks, lit)
		}
		rec, perr := checksums.Parse(toks)
		fmt.Fprintf(os.Stderr, "  [%d] line %d, %d tokens, parse=%v, %d recorded triple(s)\n",
			i+1, intent.LineOf(src, cmd.Span.Start), len(toks), perr == nil, len(rec))

		// Could a new set of sums be written into THIS block? Compute the
		// replacements a re-derivation would produce and ask the locator
		// to place them, keeping only what lands inside this command.
		sums := map[string]checksums.Sums{}
		for _, r := range rec {
			sums[r.File] = checksums.Sums{Rmd160: "NEW-RMD", Sha256: "NEW-SHA", Size: 999}
		}
		reps, rerr := checksums.Replacements(rec, sums)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "       replacements: %v\n", rerr)
			continue
		}
		edits, unlocated, _ := rewrite.Edits(src, cst, portstyle.ScopeOf(src, name), name, reps)
		inside := 0
		for _, e := range edits {
			if e.Start >= cmd.Span.Start && e.End <= cmd.Span.End {
				inside++
			}
		}
		fmt.Fprintf(os.Stderr, "       %d replacement(s) -> %d edit(s), %d inside this block, %d unlocated\n",
			len(reps), len(edits), inside, len(unlocated))
	}
}
