package observe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// A host access the evaluator recorded is explained here, or the probe is
// inconclusive: a compiler probe whose reads are confined to build
// positions, or a read inside a PortGroup whose frame lands on a benign
// command or the condition of a control whose bodies are benign.

// toolchainProbe reports whether a recorded host access came from MacPorts
// compiler selection. Such probes look for compilers the modeled profile
// lacks; when the Portfile reads toolchain options only in build positions,
// they cannot change any source declaration.
func toolchainProbe(declaration macports.Declaration) bool {
	for _, frame := range declaration.Frames {
		if strings.Contains(frame.Command, "portconfigure::") {
			return true
		}
	}
	return false
}

// portGroupReadBenign reports whether a host access was made inside a
// PortGroup file, in the condition of a branch whose every body is a benign
// sink, or as an argument of one: the qt4 PortGroup asking whether the Qt
// framework is installed to choose a dependency path, for example. Such a
// read shapes the build and can reach no source declaration, so the modeled
// context stays conclusive. groups caches the PortGroup files read.
func portGroupReadBenign(root string, frames []macports.SourceFrame, groups map[string][]byte) bool {
	if root == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	groupDir := filepath.Join(root, filepath.FromSlash(macports.PortGroupDirectory))
	// An overlay links the tree's PortGroups rather than copying them, so
	// their frames name the linked directory.
	if resolved, err := filepath.EvalSymlinks(groupDir); err == nil {
		groupDir = resolved
	}
	for i := len(frames) - 1; i >= 0; i-- {
		frame := frames[i]
		if frame.File == "" || frame.Line <= 0 {
			continue
		}
		file := frame.File
		if resolved, err := filepath.EvalSymlinks(file); err == nil {
			file = resolved
		}
		if filepath.Dir(file) != groupDir {
			continue
		}
		src, ok := groups[file]
		if !ok {
			data, err := os.ReadFile(file)
			if err != nil {
				return false
			}
			src, groups[file] = data, data
		}
		return benignAt(src, frame.Line)
	}
	return false
}

// benignAt judges the command that starts on the given line of a PortGroup
// file: benign when it is a benign sink, or a control structure with the
// line in its conditions and every body benign.
func benignAt(src []byte, line int) bool {
	offset := 0
	for n := 1; n < line; n++ {
		next := strings.IndexByte(string(src[offset:]), '\n')
		if next < 0 {
			return false
		}
		offset += next + 1
	}
	// The frame's line starts at its indentation; the command starts after it.
	for offset < len(src) && (src[offset] == ' ' || src[offset] == '\t') {
		offset++
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return false
	}
	var innermost *syntax.Command
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		if cmd.Span.Start <= offset && offset < cmd.Span.End {
			candidate := cmd
			innermost = &candidate
		}
	}
	if innermost == nil {
		return false
	}
	name, _ := innermost.Name(src)
	controls, bodies, control := innermost.Control(src)
	// A platform block runs its body where the platform matches; a read
	// inside it is benign where the whole body is, as qmake5 1.0's asks
	// whether the SDK is there to set use_xcode (the roadmap's v0.5.0 part
	// 3, item 2).
	if !control && name == "platform" && len(innermost.Words) >= 3 {
		return benignBodies(src, innermost.Words[len(innermost.Words)-1:])
	}
	if !control {
		return benignSink(name)
	}
	// A frame on the control command's own line is the condition being
	// evaluated; a frame inside a condition word is the same.
	inCondition := innermost.Span.Start == offset
	for _, word := range controls {
		if word.Span.Start <= offset && offset < word.Span.End {
			inCondition = true
		}
	}
	if !inCondition {
		return false
	}
	return benignBodies(src, bodies)
}

// benignBodies reports whether each body is benign (benignBody).
func benignBodies(src []byte, bodies []syntax.Word) bool {
	d := &dimension{read: func(string) bool { return false }, label: "a host read inside a PortGroup", tainted: map[string]bool{}}
	for _, body := range bodies {
		if err := benignBody(src, body, d); err != nil {
			return false
		}
	}
	return true
}

// tolerateExplainedProbes returns the observation with explained host
// accesses removed from its problems: compiler-selection probes, when the
// Portfile reads toolchain options only in build positions, and reads made
// inside a PortGroup whose only effect is a branch of build-only options.
// The second result says whether the remaining host state, if any, still
// makes the context inconclusive.
func Tolerate(ctx context.Context, port macports.PortObservation, contents []byte, root string) (macports.PortObservation, bool) {
	if !(port.HostAccess || port.ModeledHostAccess) {
		return port, false
	}
	toolchainBenign := false
	toolchainChecked := false
	groups := map[string][]byte{}
	explained := map[string]bool{}
	for _, declaration := range port.Declarations {
		if declaration.Command != "dockhand.host-access" || len(declaration.Values) == 0 {
			continue
		}
		var where []string
		for _, frame := range declaration.Frames {
			where = append(where, fmt.Sprintf("%s:%d %s", frame.File, frame.Line, sourceLine(frame.Command)))
		}
		switch {
		case buildDefaultReadBenign(root, contents, declaration, groups):
			progress.DebugReport(ctx, "Host access explained as a build-only option's default: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case toolchainProbe(declaration):
			if !toolchainChecked {
				toolchainBenign, toolchainChecked = toolchainReadsBenign(contents), true
			}
			if !toolchainBenign {
				progress.DebugReport(ctx, "Host access is a compiler probe but the Portfile reads toolchain options outside build positions: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
				return port, true
			}
		case portGroupReadBenign(root, declaration.Frames, groups):
			progress.DebugReport(ctx, "Host access explained as a PortGroup's build-only read: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		default:
			progress.DebugReport(ctx, "Host access not explained: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
			return port, true
		}
		explained[declaration.Values[0]] = true
	}
	if len(explained) == 0 {
		return port, true
	}
	var remaining []string
	for _, problem := range port.Problems {
		if !explained[problem] {
			remaining = append(remaining, problem)
		}
	}
	port.Problems = remaining
	port.HostAccess, port.ModeledHostAccess = false, false
	return port, false
}

// sourceLine is a frame command's first line, enough to recognize it.
func sourceLine(command string) string {
	line, _, _ := strings.Cut(command, "\n")
	if len(line) > 80 {
		line = line[:80] + "..."
	}
	return line
}

// defaultCheck is Base evaluating an option's default as it's read, the
// frame "default_check use_xcode {} read".
var defaultCheck = regexp.MustCompile(`^default_check\s+(\S+)\s`)

// buildOnlyDefault names the options whose defaults Base computes from the
// host and which shape only how a port builds: use_xcode, whose default
// asks whether /usr/lib/libxcselect.dylib is there, and the toolchain's.
func buildOnlyDefault(name string) bool {
	return name == "use_xcode" || toolchainRead(name)
}

// buildDefaultReadBenign reports a host read Base made computing the
// default of a build-only option, where neither the Portfile nor a
// PortGroup on the read's stack reads that option outside a build-only
// position: qmake5 1.0's platform block reads use_xcode, whose default
// reads the host, and librecad's and cpi's version lines and archives,
// which read no host state, were inconclusive in every modeled context
// (the roadmap's v0.5.0 part 3, item 2). A Portfile whose own source
// declarations read the option stays inconclusive.
func buildDefaultReadBenign(root string, contents []byte, declaration macports.Declaration, groups map[string][]byte) bool {
	// Each default on the way to the read is one: configure.cc's reads
	// configure.compiler's, which reads the host, so the value reaches
	// every option of the chain, and each must be build-only and read only
	// in build-only positions.
	var options []string
	for _, frame := range declaration.Frames {
		if match := defaultCheck.FindStringSubmatch(frame.Command); match != nil {
			if !buildOnlyDefault(match[1]) {
				return false
			}
			options = append(options, match[1])
		}
	}
	if len(options) == 0 {
		return false
	}
	for _, option := range options {
		if !optionReadBenign(root, contents, option, declaration, groups) {
			return false
		}
	}
	return true
}

// optionReadBenign reports whether neither the Portfile nor a PortGroup on
// the read's stack reads option outside a build-only position.
func optionReadBenign(root string, contents []byte, option string, declaration macports.Declaration, groups map[string][]byte) bool {
	reads := func(src []byte) bool {
		script, errs := syntax.Parse(src)
		return len(errs) == 0 && classifyReads(src, script, &dimension{read: func(name string) bool { return name == option }, label: option, tainted: map[string]bool{}}) == nil
	}
	// A read the walk can't see, as one inside an expression's braces,
	// counts too: the Portfile reading the option at all, by $name,
	// [option name], or [tbool name], keeps it inconclusive.
	readsAtAll := regexp.MustCompile(`\$\{?` + regexp.QuoteMeta(option) + `\b|\[(?:option|tbool|istrue)\s+` + regexp.QuoteMeta(option) + `\b`)
	if !reads(contents) || readsAtAll.Match(contents) {
		return false
	}
	if root == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	groupDir := filepath.Join(root, filepath.FromSlash(macports.PortGroupDirectory))
	// An overlay links the tree's PortGroups rather than copying them, so
	// their frames name the linked directory.
	if resolved, err := filepath.EvalSymlinks(groupDir); err == nil {
		groupDir = resolved
	}
	for _, frame := range declaration.Frames {
		file := frame.File
		if resolved, err := filepath.EvalSymlinks(file); err == nil {
			file = resolved
		}
		if file == "" || filepath.Dir(file) != groupDir {
			continue
		}
		src, ok := groups[file]
		if !ok {
			data, err := os.ReadFile(file)
			if err != nil {
				return false
			}
			src, groups[file] = data, data
		}
		if !reads(src) {
			return false
		}
	}
	return true
}
