package portfile

import (
	"bytes"
	"slices"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// platformWords are what a condition reads to ask which Mac it runs on:
// MacPorts' os.* variables, the deployment target and macOS version, the
// Xcode versions, and the build architecture. A condition that reads one
// takes a branch an evaluation on another Mac may not.
var platformWords = [][]byte{
	[]byte("os.platform"), []byte("os.subplatform"), []byte("os.major"), []byte("os.minor"), []byte("os.version"),
	[]byte("os.arch"), []byte("os.endian"), []byte("macosx_deployment_target"), []byte("macos_version"),
	[]byte("xcodeversion"), []byte("xcodecltversion"), []byte("build_arch"),
}

// UnseenChanges says which subports the text two versions of a Portfile
// differ in may change under a condition an evaluation on this Mac
// doesn't take: inside a platform block, or a control structure whose
// condition reads the platform, such as "if {${os.major} < 20}". A change
// inside a subport's literal block is that subport's; one anywhere else is
// every subport's, all. Where either version doesn't parse, nothing can
// be said of it, and that's all too. An evaluation here sees what these
// changes do only where it runs on the platform they name, so a change
// record made here can't say the subports they touch are unchanged.
func UnseenChanges(before, after []byte) (subports []string, all bool) {
	changed := changedLines(before, after)
	for i, src := range [2][]byte{before, after} {
		script, errs := syntax.Parse(src)
		if len(errs) > 0 {
			return nil, true
		}
		var regions []unseenRegion
		unseenIn(src, script, "", &regions)
		for _, line := range changed[i] {
			for _, region := range regions {
				if region.span.Start >= line.End || line.Start >= region.span.End {
					continue
				}
				if region.subport == "" {
					return nil, true
				}
				if !slices.Contains(subports, region.subport) {
					subports = append(subports, region.subport)
				}
			}
		}
	}
	slices.Sort(subports)
	return subports, false
}

// unseenRegion is a command whose bodies run only on some platforms, and
// the subport whose literal block holds it, or "" outside any.
type unseenRegion struct {
	span    textedit.Span
	subport string
}

// unseenIn finds the platform-conditional commands of a script, as
// MacPorts would run it, each with the subport block it's in.
func unseenIn(src []byte, script *syntax.Script, subport string, regions *[]unseenRegion) {
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		inner := subport
		switch {
		case name == "platform":
			*regions = append(*regions, unseenRegion{span: cmd.Span, subport: subport})
		case name == "subport" && len(cmd.Words) == 3:
			if literal, ok := cmd.Words[1].Literal(src); ok {
				inner = literal
			}
		default:
			if controls, _, ok := cmd.Control(src); ok && readsPlatform(src, controls) {
				*regions = append(*regions, unseenRegion{span: cmd.Span, subport: subport})
			}
		}
		for _, body := range bodies(src, cmd) {
			unseenIn(src, body, inner, regions)
		}
	}
}

func readsPlatform(src []byte, controls []syntax.Word) bool {
	for _, word := range controls {
		text := src[word.Span.Start:word.Span.End]
		for _, platform := range platformWords {
			if bytes.Contains(text, platform) {
				return true
			}
		}
	}
	return false
}

// changedLines are the lines of each version a line diff doesn't match
// with the other's, as spans of its source: a longest common subsequence
// of lines, after their common start and end.
func changedLines(before, after []byte) [2][]textedit.Span {
	a, b := lineSpans(before), lineSpans(after)
	same := func(i, j int) bool {
		return bytes.Equal(before[a[i].Start:a[i].End], after[b[j].Start:b[j].End])
	}
	start := 0
	for start < len(a) && start < len(b) && same(start, start) {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && same(endA-1, endB-1) {
		endA--
		endB--
	}
	n, m := endA-start, endB-start
	// lcs[i][j] is the longest common subsequence of a[start+i:endA] and
	// b[start+j:endB].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if same(start+i, start+j) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var changed [2][]textedit.Span
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && same(start+i, start+j):
			i++
			j++
		case j == m || i < n && lcs[i+1][j] >= lcs[i][j+1]:
			changed[0] = append(changed[0], a[start+i])
			i++
		default:
			changed[1] = append(changed[1], b[start+j])
			j++
		}
	}
	return changed
}

// lineSpans are a source's lines, each without its newline.
func lineSpans(src []byte) []textedit.Span {
	var spans []textedit.Span
	for start := 0; start < len(src); {
		end := bytes.IndexByte(src[start:], '\n')
		if end < 0 {
			spans = append(spans, textedit.Span{Start: start, End: len(src)})
			break
		}
		spans = append(spans, textedit.Span{Start: start, End: start + end})
		start += end + 1
	}
	return spans
}
