package observe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// A host access the evaluator recorded is explained here, or the probe is
// inconclusive: a compiler probe whose reads are confined to build
// positions, or a read inside a PortGroup whose frame lands on a benign
// command or the condition of a control whose bodies are benign.

// lookups are Base's program and dependency lookups, which a modelled
// context closes once each (dispatcher's closed_lookup).
var lookups = []string{"findBinary", "binaryInPath", "_mportsearchpath"}

// fetchInputs are Base's phase options its own fetch defaults read:
// distfiles' default is ${distname}${extract.suffix}.
var fetchInputs = []string{"extract.suffix"}

// baseLookupBenign reports whether a closed lookup is Base's own, made for
// options no fetch or checksum value reads, under the same "only where"
// condition as build.jobs's read (architecture's step 2 on #110's finding
// 1; closed-model.md's verdict). Who made the lookup and which value took
// it are decided apart. Who: with an option's default on the way, the
// innermost default is one Base's own source writes, and no frame below
// it names a file outside Base's library (defaultIsBases); with none, the
// lookup is in a callback Base runs once the Portfile is read, its owner
// is Base, and every frame below the callback is Base's code
// (bodyIsBases). A Portfile's or PortGroup's own findBinary, a default
// one wrote, and a callback one registered, are theirs, whichever of
// Base's procedures they call (architecture's points 1 and 2). Which
// value: each option whose default the chain evaluated must be one of
// Base's extract, patch, configure, build, test or destroot options or
// build-only defaults, none read by Base's own fetch defaults, and its
// value, followed through every option and variable the Portfile and its
// PortGroups give it to, read only in build positions (optionReadBenignAs,
// point 3). A PortGroup may have written an outer default, as cmake's
// configure.args reads build.cmd, so long as no fetch value reads it.
func baseLookupBenign(root string, contents []byte, declaration macports.Declaration, groups map[string][]byte, base *baseCode) bool {
	if len(declaration.Values) < 4 || declaration.Values[1] != "closed" {
		return false
	}
	lookup, _, _ := strings.Cut(declaration.Values[3], " ")
	if !slices.Contains(lookups, lookup) {
		return false
	}
	call := slices.IndexFunc(declaration.Frames, func(frame macports.SourceFrame) bool {
		return strings.HasPrefix(frame.Command, lookup+" ")
	})
	if call < 0 {
		return false
	}
	// Which value took it: each option whose default the chain read.
	var options []string
	for _, frame := range declaration.Frames[:call] {
		if match := defaultCheck.FindStringSubmatch(frame.Command); match != nil {
			options = append(options, match[1])
		}
	}
	// A lookup in one of Base's callbacks, run once the Portfile is read,
	// takes a dependency list or the notes, which no fetch or checksum
	// value reads: add_extract_deps' lbzip2, the build system's make.
	if len(options) == 0 {
		return declaration.Values[2] == "base" && len(declaration.Frames) > 0 && strings.TrimSpace(declaration.Frames[0].Command) == "port::run_callbacks" &&
			base.bodyIsBases(declaration.Frames[1:call+1])
	}
	if !base.defaultIsBases(declaration.Frames, call+1) {
		return false
	}
	for _, option := range options {
		if !(buildOnlyDefault(option) || basePhaseOption(option)) || slices.Contains(fetchInputs, option) {
			return false
		}
		if !optionReadBenignAs(root, contents, option, declaration, groups, true) {
			return false
		}
	}
	return true
}

// basePhaseOption names Base's extract, patch and configure options,
// beside build, test and destroot's (phaseOption).
func basePhaseOption(name string) bool {
	for _, phase := range []string{"extract.", "patch.", "configure."} {
		if strings.HasPrefix(name, phase) {
			return true
		}
	}
	return false
}

// sourceDateEpochLookup reports whether a recorded host access is a closed
// lookup Base's portmain::get_source_date_epoch made, by its frame, never
// by what was looked for: its findBinary git. The tree dockhand evaluates
// is a snapshot with no .git, and the model answers git's work-tree
// question of it as git does there (dispatcher's work_tree), so with git
// found or not the value is the files' times, and the closed lookup can
// reach no fetch value through it (architecture's call on #110's finding
// 1; TestSourceDateEpochIsTheSameWithGitAndWithout holds the claim). The
// frame is the procedure exactly, and everything below it to the lookup
// is Base's library (bodyIsBases): a Portfile's or PortGroup's own lookup
// of git, a procedure of its own named like Base's, or one overriding
// Base's, is its own, and stays a problem (architecture's point 4).
func sourceDateEpochLookup(declaration macports.Declaration, base *baseCode) bool {
	if len(declaration.Values) < 4 || declaration.Values[1] != "closed" {
		return false
	}
	lookup, _, _ := strings.Cut(declaration.Values[3], " ")
	if lookup != "findBinary" && lookup != "binaryInPath" {
		return false
	}
	call := slices.IndexFunc(declaration.Frames, func(frame macports.SourceFrame) bool {
		return strings.HasPrefix(frame.Command, lookup+" ")
	})
	for i, frame := range declaration.Frames[:max(call, 0)] {
		if strings.TrimSpace(frame.Command) == "portmain::get_source_date_epoch" {
			return base.bodyIsBases(declaration.Frames[i+1 : call+1])
		}
	}
	return false
}

// toolchainProbe reports whether a recorded host access came from MacPorts
// compiler selection. Such probes look for compilers the modeled profile
// lacks; when the Portfile reads toolchain options only in build positions,
// they cannot change any source declaration. The outermost frame calling
// a procedure in portconfigure is the probe, and Base's only where every
// frame below it is Base's library (bodyIsBases): a procedure a Portfile
// names into portconfigure has its file on its body (architecture's point
// 4).
func toolchainProbe(declaration macports.Declaration, base *baseCode) bool {
	for i, frame := range declaration.Frames {
		if name, _, _ := strings.Cut(strings.TrimSpace(frame.Command), " "); strings.HasPrefix(name, "portconfigure::") {
			return base.bodyIsBases(declaration.Frames[i+1:])
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
	base := newBaseCode(port.BaseLibrary)
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
		case buildDefaultReadBenign(root, contents, declaration, groups, base):
			progress.DebugReport(ctx, "Host access explained as a build-only option's default: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case baseLookupBenign(root, contents, declaration, groups, base):
			progress.DebugReport(ctx, "Host access explained as Base's own lookup for an option no fetch or checksum value reads: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case sourceDateEpochLookup(declaration, base):
			progress.DebugReport(ctx, "Host access explained as Base's source_date_epoch lookup of git: the snapshot has no .git, so the answer doesn't depend on whether git is found: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case callbackExplained(declaration, contents):
			progress.DebugReport(ctx, "Host access explained as a PortGroup callback's build-only read: %s; frames: %s", declaration.Values[0], strings.Join(where, " <- "))
		case toolchainProbe(declaration, base):
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
		if explained[problem] {
			continue
		}
		// A modelled context's host problem no declaration explains, as
		// the master's closed answers to Base's developer-tools questions
		// before the plan, leaves the context inconclusive, never a
		// refusal of the port (#110, #125).
		if strings.HasPrefix(problem, "modeled context ") {
			return port, true
		}
		remaining = append(remaining, problem)
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
// asks whether /usr/lib/libxcselect.dylib is there, the toolchain's, and
// build.jobs, whose default reads the kernel's CPU count and memory
// (buildJobsRead).
func buildOnlyDefault(name string) bool {
	return name == "use_xcode" || name == "build.jobs" || toolchainRead(name) || phaseOption(name)
}

// phaseOption names a build, test or destroot phase's own option, which a
// default may take build.jobs into, as cargo's build.pre_args and
// legacy-support's test.jobs do: it shapes how the port builds, tests or
// stages, never what it fetches.
func phaseOption(name string) bool {
	for _, phase := range []string{"build.", "test.", "destroot."} {
		if strings.HasPrefix(name, phase) {
			return true
		}
	}
	return false
}

// buildJobsRead reports a read under build.jobs's default that is Base's
// own: build_getjobs asking sysctl for hw.activecpu and hw.memsize
// (portbuild.tcl:152), which a modelled context closes (#110). Any other
// read under that default isn't explained by it.
func buildJobsRead(declaration macports.Declaration) bool {
	if !strings.Contains(declaration.Values[0], "sysctl") {
		return false
	}
	for _, frame := range declaration.Frames {
		if strings.TrimSpace(frame.Command) == "portbuild::build_getjobs" {
			return true
		}
	}
	return false
}

// buildDefaultReadBenign reports a host read Base made computing the
// default of a build-only option, where neither the Portfile nor a
// PortGroup on the read's stack reads that option outside a build-only
// position: qmake5 1.0's platform block reads use_xcode, whose default
// reads the host, and librecad's and cpi's version lines and archives,
// which read no host state, were inconclusive in every modeled context
// (the roadmap's v0.5.0 part 3, item 2). A Portfile whose own source
// declarations read the option stays inconclusive. The read is the
// default's only where Base wrote the innermost default on the way and
// nothing below it is anyone else's code (defaultIsBases): a default a
// Portfile or PortGroup wrote reads the host for itself (architecture's
// point 2).
func buildDefaultReadBenign(root string, contents []byte, declaration macports.Declaration, groups map[string][]byte, base *baseCode) bool {
	// Each default on the way to the read is one: configure.cc's reads
	// configure.compiler's, which reads the host, so the value reaches
	// every option of the chain, and each must be build-only and read only
	// in build-only positions.
	var options []string
	for _, frame := range declaration.Frames {
		if match := defaultCheck.FindStringSubmatch(frame.Command); match != nil {
			if !buildOnlyDefault(match[1]) || match[1] == "build.jobs" && !buildJobsRead(declaration) {
				return false
			}
			options = append(options, match[1])
		}
	}
	if len(options) == 0 || !base.defaultIsBases(declaration.Frames, len(declaration.Frames)) {
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
	return optionReadBenignAs(root, contents, option, declaration, groups, option == "build.jobs")
}

// optionReadBenignAs is optionReadBenign, where expressionsOnly counts a
// read the walk can't see only in an expression or by [option], as for
// build.jobs, which Portfiles pass to the build by name, -j${build.jobs}:
// a Base lookup's options are judged so (architecture's step 2 on #110's
// finding 1), as test.cmd ${build.cmd} is a build position. Either way
// the value is followed (followedReadsBenign): an option given it is
// judged as the option is, and a read the walk can't place keeps the
// context inconclusive (architecture's point 3).
func optionReadBenignAs(root string, contents []byte, option string, declaration macports.Declaration, groups map[string][]byte, expressionsOnly bool) bool {
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
	sources := [][]byte{contents}
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
		sources = append(sources, src)
	}
	watched, ok := followedReadsBenign(sources, option)
	if !ok {
		return false
	}
	// A read the walk can't see, as one inside an expression's braces,
	// counts too: the Portfile reading the option at all, by $name,
	// [option name], or [tbool name], keeps it inconclusive.
	names := []string{option}
	if expressionsOnly {
		names = watched
	}
	for _, name := range names {
		readsAtAll := regexp.MustCompile(`\$\{?` + regexp.QuoteMeta(name) + `\b|\[(?:option|tbool|istrue)\s+` + regexp.QuoteMeta(name) + `\b`)
		if expressionsOnly {
			// Portfiles pass build.jobs to the build by name, -j${build.jobs},
			// in positions the walk sees; only one in an expression it can't
			// see, or asked for by [option], counts (#110).
			readsAtAll = regexp.MustCompile(`(?:\bif|\belseif|\bexpr|\bwhile)\s*\{[^}\n]*\$\{?` + regexp.QuoteMeta(name) + `\b|\[(?:option|tbool|istrue)\s+` + regexp.QuoteMeta(name) + `\b`)
		}
		if readsAtAll.Match(contents) {
			return false
		}
	}
	return true
}

// followedReadsBenign follows option's value through the Portfile and the
// PortGroups in sources (architecture's point 3 on #110's finding 1): an
// option a read of a watched one reaches as a benign sink, in its
// arguments or in a branch the read selects, is watched too, to a fixed
// point, and each watched option is judged as the first is, its reads in
// build positions only (classifyReads), so test.cmd ${build.cmd} followed
// by a distfiles reading test.cmd is a fetch value's read. Then every
// mention of a watched option, or of a variable one was carried into, must
// be a read the walk placed or a write of it; one it didn't place, by
// [set name], [option name], in a substitution or a script it doesn't
// walk, or anywhere inside a procedure's body, whose value a fetch field
// may call for, keeps the context inconclusive. The second result says
// whether every read is benign; the first is the options watched.
func followedReadsBenign(sources [][]byte, option string) ([]string, bool) {
	watched := map[string]bool{option: true}
	scripts := make([]*syntax.Script, len(sources))
	for i, src := range sources {
		script, errs := syntax.Parse(src)
		if len(errs) > 0 {
			return nil, false
		}
		scripts[i] = script
	}
	var carried map[string]bool
	var seen []textedit.Span
	for {
		added := false
		carried, seen = map[string]bool{}, nil
		for i, src := range sources {
			d := &dimension{
				read:       func(name string) bool { return watched[name] },
				label:      option,
				tainted:    map[string]bool{},
				carried:    carried,
				hostAnswer: true,
				seen:       func(read syntax.VarSub) { seen = append(seen, read.Span) },
				sink: func(command string) {
					if name, ok := optionSink(command); ok && !watched[name] {
						watched[name], added = true, true
					}
				},
			}
			if classifyReads(src, scripts[i], d) != nil {
				return nil, false
			}
		}
		if !added {
			break
		}
	}
	for i, src := range sources {
		if !accountedFor(src, scripts[i], watched, carried, seen) {
			return nil, false
		}
	}
	names := make([]string, 0, len(watched))
	for name := range watched {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, true
}

// optionSuffix is what a Portfile command adds to an option's name to
// change its value rather than set it.
var optionSuffix = regexp.MustCompile(`-(?:append|prepend|delete|replace|strsed)$`)

// optionSink is the option a benign sink command sets, where it sets one:
// test.cmd, configure.args-append's configure.args. Commands that run or
// print, as system and ui_msg, set none; patchfiles is no sink for a host
// answer at all (dimension.benign).
func optionSink(command string) (string, bool) {
	name := optionSuffix.ReplaceAllString(command, "")
	switch name {
	case "use_xcode", "macosx_deployment_target", "notes":
		return name, true
	}
	for _, family := range benignFamilies {
		if strings.HasPrefix(name, family) {
			return name, true
		}
	}
	return "", false
}

// accountedFor reports whether every mention in src of a watched option or
// a carried variable, outside comments, is a read the walk placed (seen)
// or the command that writes it, and none is inside a procedure's body.
func accountedFor(src []byte, script *syntax.Script, watched, carried map[string]bool, seen []textedit.Span) bool {
	var comments, procs, writes []textedit.Span
	var walk func(*syntax.Script)
	walk = func(script *syntax.Script) {
		for _, item := range script.Items {
			switch item := item.(type) {
			case syntax.Comment:
				comments = append(comments, item.Span)
			case syntax.Command:
				if name, ok := item.Name(src); ok {
					if watched[optionSuffix.ReplaceAllString(name, "")] {
						writes = append(writes, item.Words[0].Span)
					}
					if name == "proc" {
						procs = append(procs, item.Span)
					}
					if name == "set" && len(item.Words) == 3 {
						if variable, ok := item.Words[1].Literal(src); ok && carried[variable] {
							writes = append(writes, item.Words[1].Span)
						}
					}
				}
				for _, word := range item.Words {
					if body, ok := word.BracedScript(src); ok {
						walk(body)
					}
					for _, segment := range word.Segments {
						if sub, ok := segment.(syntax.CmdSub); ok {
							walk(sub.Script)
						}
					}
				}
			}
		}
	}
	walk(script)
	within := func(spans []textedit.Span, offset int) bool {
		return slices.ContainsFunc(spans, func(span textedit.Span) bool { return span.Start <= offset && offset < span.End })
	}
	nameByte := func(c byte, dot bool) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || dot && c == '.'
	}
	text := string(src)
	for _, names := range []map[string]bool{watched, carried} {
		for name := range names {
			for from := 0; ; {
				at := strings.Index(text[from:], name)
				if at < 0 {
					break
				}
				at += from
				from = at + 1
				if at > 0 && nameByte(text[at-1], true) || at+len(name) < len(text) && nameByte(text[at+len(name)], false) {
					continue
				}
				switch {
				case within(comments, at):
				case within(procs, at):
					return false
				case within(writes, at), within(seen, at):
				default:
					return false
				}
			}
		}
	}
	return true
}

// callbackExplained reports a host read made under a PortGroup callback
// buildCallbacks names, whose option the Portfile reads only in build
// positions (callbackProbe, optionReadsBenign).
func callbackExplained(declaration macports.Declaration, contents []byte) bool {
	callback, ok := callbackProbe(declaration)
	return ok && optionReadsBenign(contents, callback.Option)
}
