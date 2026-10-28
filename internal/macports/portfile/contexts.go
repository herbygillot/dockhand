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

// BlockChanges says which subports' literal blocks hold every line two
// versions of a Portfile differ in: MacPorts runs a `subport X { … }`
// block's body only when evaluating X, so a change there is X's alone.
// all is a change anywhere else, outside every subport's block, which is
// every subport's, or a version that doesn't parse, of which nothing can
// be said. It's the attribution UnseenChanges makes, for every change
// rather than the platform's: terraform-1.16's patch bump, inside its own
// block, reaches none of its 18 siblings (field testing's terraform
// replay, 2026-10-22).
func BlockChanges(before, after []byte) (subports []string, all bool) {
	changed := changedLines(before, after)
	for i, src := range [2][]byte{before, after} {
		script, errs := syntax.Parse(src)
		if len(errs) > 0 {
			return nil, true
		}
		var blocks []unseenRegion
		subportBlocks(src, script, &blocks)
		for _, line := range changed[i] {
			j := slices.IndexFunc(blocks, func(b unseenRegion) bool { return b.span.Start <= line.Start && line.End <= b.span.End })
			if j < 0 {
				return nil, true
			}
			if !slices.Contains(subports, blocks[j].subport) {
				subports = append(subports, blocks[j].subport)
			}
		}
	}
	slices.Sort(subports)
	return subports, false
}

// subportBlocks are a script's `subport X { … }` commands with literal
// names, wherever they are, as MacPorts would run them; a block inside
// another is the outer's, which runs it.
func subportBlocks(src []byte, script *syntax.Script, blocks *[]unseenRegion) {
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if name, _ := cmd.Name(src); name == "subport" && len(cmd.Words) == 3 {
			if literal, ok := cmd.Words[1].Literal(src); ok {
				*blocks = append(*blocks, unseenRegion{span: cmd.Span, subport: literal})
				continue
			}
		}
		for _, body := range bodies(src, cmd) {
			subportBlocks(src, body, blocks)
		}
	}
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
	from, _ := textedit.Match(len(a), len(b), func(i, j int) bool {
		return bytes.Equal(before[a[i].Start:a[i].End], after[b[j].Start:b[j].End])
	})
	var changed [2][]textedit.Span
	for _, i := range textedit.Unmatched(len(a), from) {
		changed[0] = append(changed[0], a[i])
	}
	for j, i := range from {
		if i < 0 {
			changed[1] = append(changed[1], b[j])
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
