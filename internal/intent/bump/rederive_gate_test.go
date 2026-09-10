package bump

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// TestRederiveGateSurvey measures whether rule T is reachable at all. It
// ships nothing.
//
// Rule T is the gh shape: a checksums command in a branch this host did
// not take, whose filenames are unknown until something evaluates the
// port under the OTHER frame. Re-deriving it means choosing that frame,
// which is only possible if dockhand can express the condition the
// branch is gated on.
//
// eval/platform.go sets build_arch, universal_archs, macos_version,
// macos_version_major, macosx_version and macosx_deployment_target from
// a frame, computed through base's own cascades. It deliberately sets
// NEITHER os_minor, macosx_sdk_version NOR xcodeversion — those describe
// the machine, not a macOS release, and a frame claiming them would be
// claiming something it cannot know.
//
// So each gated block sorts into one of:
//
//   - expressible: every variable in the gate is one a frame sets, so
//     re-derivation can pick a frame that takes the branch.
//
//   - OUT OF FRAME: the gate reads something a frame does not set. No
//     choice of frame reaches the branch, and the honest answer is the
//     refusal that already exists.
//
//   - mixed: both, which is out of frame for the same reason.
//
//     DOCKHAND_REDERIVE_TREE=/path/to/ports \
//     go test ./internal/intent/bump/ -run TestRederiveGateSurvey -timeout 30m -v
func TestRederiveGateSurvey(t *testing.T) {
	root := os.Getenv("DOCKHAND_REDERIVE_TREE")
	if root == "" {
		t.Skip("set DOCKHAND_REDERIVE_TREE=<ports tree> to run the survey")
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "*", "Portfile"))
	require.NoError(t, err)

	var gated, expressible, outOfFrame, mixed int
	seen := map[string]int{}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cst, perrs := syntax.Parse(src)
		if len(perrs) != 0 {
			continue
		}
		for _, gate := range gatedChecksums(src, cst) {
			gated++
			nFrame, nCtx, nOut := 0, 0, 0
			for _, v := range gate {
				seen[v]++
				switch {
				case framed[v]:
					nFrame++
				case selector[v]:
					nCtx++
				default:
					nOut++
				}
			}
			switch {
			case nOut > 0:
				outOfFrame++
			case nFrame > 0:
				expressible++
			case nCtx > 0:
				mixed++
			}
		}
	}
	type kv struct {
		v string
		n int
	}
	var all []kv
	for v, n := range seen {
		all = append(all, kv{v, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n })
	fmt.Fprintf(os.Stderr, "\n== variables gating a checksums command ==\n")
	for i, e := range all {
		if i == 25 {
			break
		}
		mark := "  "
		switch {
		case framed[e.v]:
			mark = "FR"
		case selector[e.v]:
			mark = "cx"
		}
		fmt.Fprintf(os.Stderr, " %s %-28s %5d\n", mark, e.v, e.n)
	}
	fmt.Fprintf(os.Stderr, "\n== %d Portfiles | %d gated checksums commands ==\n context only %4d (evaluate that subport; already reachable)\n frame        %4d (needs the frame axis)\n OUT OF REACH %4d\n",
		len(files), gated, mixed, expressible, outOfFrame)
}

// framed is what eval/platform.go actually sets, plus the names base
// derives from those within one evaluation. A gate reading only these
// can be steered by choosing a frame.
var framed = map[string]bool{
	"os.arch": true, "os.platform": true, "os.major": true,
	"os_major": true, "build_arch": true, "configure.build_arch": true,
	"universal_archs": true, "macos_version": true,
	"macos_version_major": true, "macosx_version": true,
	"macosx_deployment_target": true, "supported_archs": true,
	// cxx_stdlib is set by platformOverrides, and configure.cxx_stdlib
	// is a lazy `default` over it (portconfigure.tcl:65), so it is
	// computed at evaluation time and follows the frame.
	"cxx_stdlib": true, "configure.cxx_stdlib": true,
	"os_platform": true, "os_version": true, "os_subplatform": true,
	"muniversal.architectures": true, "configure.universal_archs": true,
}

// selector is what a gate reads when it picks a SUBPORT rather than a
// platform. Evaluating the port as that subport takes the branch, so
// these need no frame at all.
var selector = map[string]bool{
	"subport": true, "name": true, "my_subport": true, "myport": true,
	// A language PortGroup's version is set FROM the subport it is
	// building: py27-foo evaluates with python.version 27. Measured on
	// 135 Portfiles, `if {${python.version} == 27}` guarding a
	// checksums command is an older release kept for an older Python —
	// reachable by evaluating that subport, not by any platform choice.
	"python.version": true, "python.branch": true, "python_version": true,
	"php.branch": true, "php.version": true,
	"perl5.major": true, "perl5.version": true, "ruby.version": true,
}

var varRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_.]*)\}|\$([A-Za-z_][A-Za-z0-9_.]*)`)

// gatedChecksums is, for each checksums command that sits inside a
// conditional, the variables that conditional's test reads. The
// innermost enclosing conditional wins: it is the one that decides
// whether the command runs.
func gatedChecksums(src []byte, cst *syntax.Script) [][]string {
	type cond struct {
		span syntax.Command
		vars []string
	}
	var conds []cond
	var blocks []syntax.Command
	for cmd := range cst.Commands(src, func(syntax.Command) bool { return true }) {
		n, ok := cmd.Name(src)
		if !ok {
			continue
		}
		switch n {
		case "if", "elseif":
			if len(cmd.Words) >= 2 {
				var vs []string
				for _, m := range varRef.FindAllStringSubmatch(cmd.Words[1].Span.Text(src), -1) {
					if m[1] != "" {
						vs = append(vs, m[1])
					} else {
						vs = append(vs, m[2])
					}
				}
				conds = append(conds, cond{cmd, vs})
			}
		case "checksums", "checksums-append":
			blocks = append(blocks, cmd)
		}
	}
	var out [][]string
	for _, b := range blocks {
		best := -1
		for i, c := range conds {
			if c.span.Span.Start <= b.Span.Start && b.Span.End <= c.span.Span.End &&
				(best < 0 || c.span.Span.Start > conds[best].span.Span.Start) {
				best = i
			}
		}
		if best >= 0 && len(conds[best].vars) > 0 {
			out = append(out, conds[best].vars)
		}
	}
	return out
}
