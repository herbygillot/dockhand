package portfile

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
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

// InertChanges says which inert options (macports.InertOption) the lines
// two versions of a Portfile differ in set, ok where every changed line is
// a comment, a blank, or belongs to commands that each set one, with a
// modifier or without (maintainers-append), their value words plain: no
// command substitution, `[…]`, and no expansion. Both versions must
// parse, and a change outside a command, between a structure's words or
// inside a non-inert command's, is no inert one. Where a line sits, in a
// platform's body, a subport's, or an `if`'s, doesn't matter: an inert
// option builds nothing differently wherever it's set, and the lines,
// not an evaluation, decide, since this Mac evaluates its own platform
// alone and a `platform darwin 21` body the edit touched runs only on 12.
// It's how a check after an openmaintainer line, or a categories reorder,
// reuses what an earlier check built (decision 28's inert difference).
func InertChanges(before, after []byte) (options []string, ok bool) {
	changed := changedLines(before, after)
	for i, src := range [2][]byte{before, after} {
		script, errs := syntax.Parse(src)
		if len(errs) > 0 {
			return nil, false
		}
		var found inertLayout
		found.collect(src, script)
		for _, line := range changed[i] {
			set, ok := found.judge(src, line)
			if !ok {
				return nil, false
			}
			for _, option := range set {
				if !slices.Contains(options, option) {
					options = append(options, option)
				}
			}
		}
	}
	slices.Sort(options)
	// An option the Portfile reads elsewhere, as a destroot hook writing
	// ${description} into a file, builds differently when it changes, so
	// no certificate is given: inert by name isn't inert by use (Codex's
	// sixth review, #70). A read in another inert option's line, as
	// long_description's ${description}, is metadata still.
	for _, src := range [2][]byte{before, after} {
		if readElsewhere(src, options) {
			return nil, false
		}
	}
	return options, true
}

// readElsewhere reports whether a Portfile reads any of these options,
// as ${name}, $name, or [option name], in a command other than one that
// sets an inert option; true where it can't tell.
func readElsewhere(src []byte, options []string) bool {
	if len(options) == 0 {
		return false
	}
	var patterns []string
	for _, option := range options {
		quoted := regexp.QuoteMeta(option)
		patterns = append(patterns, `\$\{`+quoted+`\}`, `\[option\s+`+quoted+`[\s\]]`)
		if !strings.Contains(option, ".") {
			patterns = append(patterns, `\$`+quoted+`(?:[^A-Za-z0-9_:.]|$)`)
		}
	}
	reads := regexp.MustCompile(strings.Join(patterns, "|")).FindAllIndex(src, -1)
	if len(reads) == 0 {
		return false
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return true
	}
	var commands []syntax.Command
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		commands = append(commands, cmd)
	}
	for _, read := range reads {
		// The innermost command holding the read is the one reading it;
		// one held by none is in a comment.
		var reader *syntax.Command
		for i := range commands {
			c := &commands[i]
			if c.Span.Start <= read[0] && read[1] <= c.Span.End && (reader == nil || c.Span.End-c.Span.Start < reader.Span.End-reader.Span.Start) {
				reader = c
			}
		}
		if reader == nil {
			continue
		}
		name, ok := reader.Name(src)
		if !ok {
			return true
		}
		for _, modifier := range optionModifiers {
			if trimmed, cut := strings.CutSuffix(name, modifier); cut {
				name = trimmed
				break
			}
		}
		if !macports.InertOption(name) {
			return true
		}
	}
	return false
}

// optionModifiers are the suffixes an option takes as a command of its
// own: maintainers-append adds to maintainers.
var optionModifiers = []string{"-append", "-prepend", "-delete", "-replace", "-strsed"}

// inertLayout is a script's commands at every depth MacPorts runs them,
// each before those in its bodies, with its bodies' spans, and its
// comments at every depth.
type inertLayout struct {
	commands []syntax.Command
	bodies   [][]textedit.Span
	comments []textedit.Span
}

func (l *inertLayout) collect(src []byte, script *syntax.Script) {
	for _, item := range script.Items {
		switch item := item.(type) {
		case syntax.Comment:
			l.comments = append(l.comments, item.Span)
		case syntax.Command:
			inner := bodies(src, item)
			spans := make([]textedit.Span, len(inner))
			for i, body := range inner {
				spans[i] = body.Span
			}
			l.commands = append(l.commands, item)
			l.bodies = append(l.bodies, spans)
			for _, body := range inner {
				l.collect(src, body)
			}
		}
	}
}

// innermost is the deepest command holding a span, -1 where none does:
// siblings don't overlap, so the last in collection order is the deepest.
func (l *inertLayout) innermost(span textedit.Span) int {
	found := -1
	for i, cmd := range l.commands {
		if cmd.Span.Start <= span.Start && span.End <= cmd.Span.End {
			found = i
		}
	}
	return found
}

// judge says which inert options a changed line sets, ok where it sets
// nothing else: each character not blank and not in a comment belongs to
// an inert command, and a blank line lies between commands, in a body or
// outside every command, or inside an inert command's words.
func (l *inertLayout) judge(src []byte, line textedit.Span) ([]string, bool) {
	var options []string
	blank := true
	for at := line.Start; at < line.End; at++ {
		if c := src[at]; c == ' ' || c == '\t' || c == '\r' {
			continue
		}
		blank = false
		if slices.ContainsFunc(l.comments, func(s textedit.Span) bool { return s.Start <= at && at < s.End }) {
			continue
		}
		i := l.innermost(textedit.Span{Start: at, End: at + 1})
		if i < 0 {
			return nil, false
		}
		option, ok := inertCommand(src, l.commands[i])
		if !ok {
			return nil, false
		}
		if !slices.Contains(options, option) {
			options = append(options, option)
		}
	}
	if !blank {
		return options, true
	}
	i := l.innermost(line)
	if i < 0 {
		return nil, true
	}
	if slices.ContainsFunc(l.bodies[i], func(s textedit.Span) bool { return s.Start <= line.Start && line.End <= s.End }) {
		return nil, true
	}
	option, ok := inertCommand(src, l.commands[i])
	if !ok {
		return nil, false
	}
	return []string{option}, true
}

// inertCommand is the inert option a command sets, ok where its first
// word, less a modifier, is one, and its words are plain.
func inertCommand(src []byte, cmd syntax.Command) (string, bool) {
	name, ok := cmd.Name(src)
	if !ok {
		return "", false
	}
	for _, modifier := range optionModifiers {
		if trimmed, cut := strings.CutSuffix(name, modifier); cut {
			name = trimmed
			break
		}
	}
	if !macports.InertOption(name) {
		return "", false
	}
	for _, word := range cmd.Words[1:] {
		if !word.Plain() {
			return "", false
		}
	}
	return name, true
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
