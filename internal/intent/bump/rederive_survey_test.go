package bump

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// TestRederiveHazardSurvey measures the constraint re-derivation has to
// be built under, over a whole ports tree. It ships nothing.
//
// THE HAZARD. Re-deriving another frame's digests means writing into a
// checksums command this evaluation never took. rewrite.Edits locates
// what it writes BY VALUE, so two commands in one Portfile holding a
// byte-identical digest are indistinguishable to it: a run meant for
// the second would land in the first. LyX is the port that showed this,
// and one port is not a frequency.
//
// SCOPE IS THE MITIGATION AND IT HAS TO BE MEASURED, NOT ASSUMED. A
// rewrite runs under portstyle.ScopeOf(src, context), which descends
// into conditionals and into the context's OWN subport and no other, so
// two blocks in two sibling subports are never candidates for one
// write however identical their digests. Only a collision WITHIN one
// scope can misplace anything.
//
// What is counted, per context — the top level and each subport
// separately — for every Portfile carrying more than one checksums
// command in a single scope:
//
//   - collide: two commands share a digest value byte for byte, so a
//     value-located write cannot tell them apart.
//   - WRITTEN: of those, the collision involves a digest a re-derivation
//     would actually rewrite. This is the number that matters, and it is
//     much smaller. neovim's two subports collide on the vendored
//     tree-sitter tarballs they SHARE — pinned files whose digests a
//     bump has no reason to touch — while the file it would rewrite,
//     ${name}-${version}.tar.gz, is unambiguous. A collision on a digest
//     nobody writes is not a hazard.
//   - repeat: one command holds the same digest value twice — the same
//     hazard inside a single command, which a span filter cannot fix
//     either.
//   - distinct: every digest in the file is unique, and locating by
//     value is safe as it stands.
//
// No evaluator is needed: this is a question about the text, and the
// text is where the write lands.
//
//	DOCKHAND_REDERIVE_TREE=/path/to/ports \
//	  go test ./internal/intent/bump/ -run TestRederiveHazardSurvey -timeout 30m -v
func TestRederiveHazardSurvey(t *testing.T) {
	root := os.Getenv("DOCKHAND_REDERIVE_TREE")
	if root == "" {
		t.Skip("set DOCKHAND_REDERIVE_TREE=<ports tree> to run the survey")
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "*", "Portfile"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	var multi, collide, written, repeat, distinct, unparsed int
	// The guard's own question, which is wider than the digest one: it
	// refuses ANY literal a replacement would move that is written twice
	// in scope, sizes and filenames included. Two distfiles of equal
	// length is not a digest collision and would misplace a write just
	// the same, so the cost of the guard is counted here rather than
	// inferred from the digest count.
	var anyRepeat, sizeOnly int
	worst := map[string]int{}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cst, perrs := syntax.Parse(src)
		if len(perrs) != 0 {
			unparsed++
			continue
		}
		contexts := append([]string{shortPort(path)}, subportNames(src, cst)...)
		blocksN := 0
		hasCollide, hasRepeat, hitsWritten := false, false, false
		for _, ctxName := range contexts {
			blocks := checksumCommands(src, cst, portstyle.ScopeOf(src, ctxName))
			if len(blocks) < 2 {
				continue
			}
			if len(blocks) > blocksN {
				blocksN = len(blocks)
			}
			c, r, w := collisions(blocks)
			hasCollide, hasRepeat, hitsWritten = hasCollide || c, hasRepeat || r, hitsWritten || w
		}
		if blocksN < 2 {
			continue
		}
		multi++
		if wide, sizes := wideRepeat(src, cst, contexts); wide {
			anyRepeat++
			if sizes && !hasCollide && !hasRepeat {
				sizeOnly++
			}
		}
		name := shortName(root, path)
		switch {
		case hasCollide:
			collide++
			worst[name] = blocksN
			label := "collide"
			if hitsWritten {
				written++
				label = "WRITTEN"
			}
			fmt.Fprintf(os.Stderr, "%s  %-40s %d blocks in one scope\n", label, name, blocksN)
		case hasRepeat:
			repeat++
			fmt.Fprintf(os.Stderr, "repeat   %-40s %d blocks in one scope\n", name, blocksN)
		default:
			distinct++
		}
	}
	fmt.Fprintf(os.Stderr, "\n== %d Portfiles | %d with >1 checksums command in one scope ==\n collide  %4d  (of which the collision hits a digest a bump would write: %d)\n repeat   %4d\n distinct %4d\n unparsed %4d\n\n the guard's wider question: %d Portfiles repeat SOME literal in scope, %d of them only a size or a name\n",
		len(files), multi, collide, written, repeat, distinct, unparsed, anyRepeat, sizeOnly)
}

// checksumCommands is every checksums command a scope reaches, as its
// word list — the exact set a rewrite under that scope could land in.
func checksumCommands(src []byte, cst *syntax.Script, scope func(syntax.Command) bool) [][]string {
	var out [][]string
	for cmd := range cst.Commands(src, scope) {
		n, ok := cmd.Name(src)
		if !ok || (n != "checksums" && n != "checksums-append") {
			continue
		}
		var toks []string
		for i, w := range cmd.Words[1:] {
			if lit, isLit := w.Literal(src); isLit {
				toks = append(toks, strings.Fields(lit)...)
				continue
			}
			// A word that is NOT a literal gets a placeholder unique to
			// its position. It has to hold the group structure open —
			// dropping it would fuse two file groups into one — while
			// never comparing equal to anything, because a computed
			// digest is not a value a rewrite locates by. Substituting
			// one shared sentinel here reported every pair of computed
			// blocks as a collision: qt5's `rmd160 [lindex ...]` twice
			// is not two identical digests, it is no digests at all.
			toks = append(toks, fmt.Sprintf("<computed-%d-%d>", len(out), i))
		}
		out = append(out, toks)
	}
	return out
}

// digestsOf is the digest values a checksums command carries, read
// through the same grouping the guard uses so that what is counted here
// is what a rewrite would go looking for.
func digestsOf(toks []string) []string {
	var out []string
	for _, g := range intent.ChecksumGroups(toks) {
		keys := make([]string, 0, len(g.Digests))
		for k := range g.Digests {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			// size is a length and collides constantly between files
			// that happen to be the same size; it is not what a rewrite
			// locates by.
			if k == "size" {
				continue
			}
			if v := g.Digests[k]; !strings.HasPrefix(v, "<computed-") {
				out = append(out, v)
			}
		}
	}
	return out
}

// movingDigests is the digests belonging to file groups whose content
// moves with the version — the ones a re-derivation writes.
//
// TWO SHAPES MOVE, and reading only the first is how this survey first
// reported LyX as harmless. A group whose file word interpolates names
// a file built from the version: neovim's ${name}-${version}.tar.gz,
// terraform's [terraformDistBase]_amd64.zip. A group that names NO file
// is the single-distfile shape — a bare `checksums rmd160 x sha256 y` —
// whose one implicit distfile is the port's own, and whose digests every
// bump rewrites. It is also the commonest checksums command in the tree.
//
// What stays put is a group with a LITERAL filename:
// tree-sitter-c-0.24.1.tar.gz is a pin that a bump has no reason to
// touch, which is why neovim's two subports sharing it is not a hazard.
func movingDigests(toks []string) []string {
	var out []string
	for _, g := range intent.ChecksumGroups(toks) {
		if g.File != "" && !strings.HasPrefix(g.File, "<computed-") {
			continue
		}
		for k, v := range g.Digests {
			if k != "size" && !strings.HasPrefix(v, "<computed-") {
				out = append(out, v)
			}
		}
	}
	return out
}

// collisions is the three findings for one scope's blocks.
func collisions(blocks [][]string) (collide, repeat, written bool) {
	seen := map[string]int{}
	moving := map[string]bool{}
	for i, toks := range blocks {
		within := map[string]bool{}
		for _, d := range digestsOf(toks) {
			if within[d] {
				repeat = true
			}
			within[d] = true
			if at, ok := seen[d]; ok && at != i {
				collide = true
				if moving[d] {
					written = true
				}
			}
			if _, ok := seen[d]; !ok {
				seen[d] = i
			}
		}
		for _, d := range movingDigests(toks) {
			moving[d] = true
		}
	}
	return collide, repeat, written
}

// wideRepeat is the guard's own test: some literal written twice among
// the checksums commands one scope reaches. It also reports whether the
// only repeated literals are sizes or filenames rather than digests,
// which is the part the digest census cannot see.
func wideRepeat(src []byte, cst *syntax.Script, contexts []string) (repeat, sizeOrNameOnly bool) {
	digestish := func(v string) bool {
		if len(v) < 16 {
			return false
		}
		for _, c := range v {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
		return true
	}
	sizeOrNameOnly = true
	for _, ctxName := range contexts {
		seen := map[string]int{}
		for cmd := range cst.Commands(src, portstyle.ScopeOf(src, ctxName)) {
			n, ok := cmd.Name(src)
			if !ok || (n != "checksums" && n != "checksums-append") {
				continue
			}
			for _, w := range cmd.Words[1:] {
				lit, isLit := w.Literal(src)
				if !isLit {
					continue
				}
				for _, tok := range strings.Fields(lit) {
					if checksumType(tok) {
						continue
					}
					seen[tok]++
					if seen[tok] == 2 {
						repeat = true
						if digestish(tok) {
							sizeOrNameOnly = false
						}
					}
				}
			}
		}
	}
	return repeat, repeat && sizeOrNameOnly
}

func checksumType(t string) bool {
	switch t {
	case "md5", "sha1", "rmd160", "sha256", "sha512", "size":
		return true
	}
	return false
}

// subportNames is every subport a Portfile declares: the other contexts
// an evaluation of it can run in.
func subportNames(src []byte, cst *syntax.Script) []string {
	var out []string
	for cmd := range cst.Commands(src, func(syntax.Command) bool { return true }) {
		if n, ok := cmd.Name(src); ok && n == "subport" && len(cmd.Words) >= 3 {
			if lit, isLit := cmd.Words[1].Literal(src); isLit {
				out = append(out, lit)
			}
		}
	}
	return out
}

func shortPort(path string) string { return filepath.Base(filepath.Dir(path)) }

func shortName(root, path string) string {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return path
	}
	return rel
}
