package observe

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// Following a host answer's value through the Portfile and the PortGroups
// on the read's stack (architecture's point 3 on #110's finding 1, and its
// bounds A1 to A6 and B on the round's survey): an option, variable or
// procedure the value reaches is watched, to a fixed point, and every
// mention of a watched name must be a read in a position whose value can
// reach no field the model acts on, or a write of it. Anything else leaves
// the context inconclusive, never explained.

// optionSuffix is what a Portfile command adds to an option's name to
// change its value rather than set it.
var optionSuffix = regexp.MustCompile(`-(?:append|prepend|delete|replace|strsed)$`)

// optionSink is the option a benign sink command sets, where it sets one:
// test.cmd, configure.args-append's configure.args. Commands that run or
// print, as system and ui_msg, set none; patchfiles is no sink for a host
// answer at all (sinkFor).
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

// sinkFor reports whether a command is a benign sink for a host answer's
// value: patchfiles is not, since dockhand fetches, checksums and
// rewrites the patch list (architecture's ruling on case n).
func sinkFor(name string) bool {
	return benignSink(name) && !strings.HasPrefix(name, "patchfiles")
}

// read is one mention a walk reads: a variable, an option by name
// ([option name], [exists name], [tbool name], [istrue name], [set name]),
// or a call of a procedure.
type read struct {
	name string
	kind int
	span textedit.Span
}

const (
	readVariable = iota
	readOption
	readCall
)

// namedReads are the commands that read the option their argument names.
var namedReads = map[string]bool{"option": true, "exists": true, "tbool": true, "istrue": true}

// procedure is a definition the sources hold, by literal name.
type procedure struct {
	source int
	name   string
	body   *syntax.Script
	span   textedit.Span
}

// scope is where a walk is: the top level, or a watched procedure's body
// with its locals and the globals it declares.
type scope struct {
	proc    *procedure
	tainted map[string]bool
	globals map[string]bool
	// returns is set where a watched value reaches what the procedure
	// returns.
	returns bool
	// phase is set inside a post-extraction phase or hook's body, which
	// runs after the source is fetched.
	phase bool
}

// follower holds one following's state across its passes.
type follower struct {
	srcs    [][]byte
	scripts []*syntax.Script
	acted   func(string) bool
	// watched are the options, procs the procedures, and carried the
	// top-level variables a watched value reaches.
	watched, procs, carried map[string]bool
	// returning are the watched procedures whose bodies return a watched
	// value; a statement call is placed only for one that doesn't (A2).
	returning map[string]bool
	defs      []procedure
	// placed are the spans, per source, of the mentions the walk placed:
	// reads it accepted, writes, declarations and definitions; literal
	// the whole literal words of benign sinks' arguments (B), which place
	// options and variables, never procedures. locals are a procedure
	// body's carried variables, by the body's span.
	placed, literal [][]textedit.Span
	locals          map[textedit.Span]map[string]bool
	changed         bool
}

// followedReadsBenign follows option's value through the Portfile and the
// PortGroups in sources (architecture's point 3 on #110's finding 1, with
// its bounds A1 to A5 and B). The second result says whether every read of
// it is benign; the first is the options watched. base answers which
// options are Base's own fields, which a default the value reaches fails
// the context on (A4).
func followedReadsBenign(sources [][]byte, option string, base *baseCode) ([]string, bool) {
	f := &follower{srcs: sources, watched: map[string]bool{option: true}, procs: map[string]bool{}, returning: map[string]bool{}}
	options := baseOptions(base.library)
	// A field the model acts on, by the same set the walk refuses a read
	// in: a command no benign sink, of Base's own options or those the
	// evaluator reads (macports.ReadOptions); patchfiles among them.
	f.acted = func(name string) bool {
		return !sinkFor(name) && (options[name] || slices.Contains(macports.ReadOptions, name))
	}
	for i, src := range sources {
		script, errs := syntax.Parse(src)
		if len(errs) > 0 {
			return nil, false
		}
		f.scripts = append(f.scripts, script)
		for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
			if name, _ := cmd.Name(src); name != "proc" || len(cmd.Words) != 4 {
				continue
			}
			proc, ok := cmd.Words[1].Literal(src)
			body, isScript := cmd.Words[3].BracedScript(src)
			if ok && isScript {
				f.defs = append(f.defs, procedure{source: i, name: proc, body: body, span: cmd.Words[3].Span})
			}
		}
	}
	// To a fixed point, every pass afresh; the watched sets only grow, so
	// the passes end, and a bound guards it all the same (A3).
	for pass := 0; ; pass++ {
		if pass > 64 {
			return nil, false
		}
		f.changed = false
		if f.carried == nil {
			f.carried = map[string]bool{}
		}
		f.placed = make([][]textedit.Span, len(sources))
		f.literal = make([][]textedit.Span, len(sources))
		f.locals = map[textedit.Span]map[string]bool{}
		// A procedure whose body mentions a watched name is watched; two
		// definitions of one name are watched where either is (A3).
		for _, def := range f.defs {
			if !f.procs[def.name] && f.mentions(def) {
				f.procs[def.name], f.changed = true, true
			}
		}
		// Each pass carries its own top-level variables; the last pass's
		// told which procedures mention them.
		f.carried = map[string]bool{}
		returning := map[string]bool{}
		for _, def := range f.defs {
			if !f.procs[def.name] {
				continue
			}
			sc := &scope{proc: &def, tainted: map[string]bool{}, globals: map[string]bool{}}
			if !f.walk(def.source, def.body, sc) {
				return nil, false
			}
			f.locals[def.span] = sc.tainted
			returning[def.name] = returning[def.name] || sc.returns
		}
		for name := range returning {
			if returning[name] != f.returning[name] {
				f.changed = true
			}
		}
		f.returning = returning
		for i := range sources {
			if !f.walk(i, f.scripts[i], &scope{tainted: f.carried}) {
				return nil, false
			}
		}
		if !f.changed {
			break
		}
	}
	for i := range sources {
		if !f.accounted(i) {
			return nil, false
		}
	}
	names := make([]string, 0, len(f.watched))
	for name := range f.watched {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, true
}

// mentions reports whether a procedure's body names a watched option or
// procedure, or a top-level variable a watched value reached.
func (f *follower) mentions(def procedure) bool {
	text := def.span.Text(f.srcs[def.source])
	for _, names := range []map[string]bool{f.watched, f.procs, f.carried} {
		for name := range names {
			if len(mentionsOf(text, name)) > 0 {
				return true
			}
		}
	}
	return false
}

// watches reports whether a read is of a watched name in the scope.
func (f *follower) watches(r read, sc *scope) bool {
	switch r.kind {
	case readCall:
		return f.procs[r.name]
	case readOption:
		if sc.proc != nil && sc.tainted[r.name] {
			return true
		}
		return f.watched[r.name] || f.carried[r.name]
	}
	if sc.tainted[r.name] {
		return true
	}
	if sc.proc != nil && !sc.globals[r.name] {
		return false
	}
	return f.watched[r.name] || f.carried[r.name]
}

// carries reports whether any read is watched; where one is, every read
// is placed, for the accounting.
func (f *follower) carries(si int, reads []read, sc *scope) bool {
	found := false
	for _, r := range reads {
		if f.watches(r, sc) {
			found = true
		}
	}
	if found {
		for _, r := range reads {
			f.placed[si] = append(f.placed[si], r.span)
		}
	}
	return found
}

func (f *follower) watch(name string) {
	if !f.watched[name] {
		f.watched[name], f.changed = true, true
	}
}

func (f *follower) taint(sc *scope, variable string, carries bool) {
	if sc.proc != nil && !sc.globals[variable] {
		sc.tainted[variable] = sc.tainted[variable] || carries
		return
	}
	if carries && !f.carried[variable] {
		f.carried[variable], f.changed = true, true
	}
}

// walk judges a script's commands in a scope, and reports whether every
// watched read in it is benign.
func (f *follower) walk(si int, script *syntax.Script, sc *scope) bool {
	src := f.srcs[si]
	for _, cmd := range script.Direct() {
		if len(cmd.Words) == 0 {
			continue
		}
		name, literal := cmd.Name(src)
		if !literal {
			// A command named by a substitution or a variable, as $cmd or
			// [p], runs whatever it names.
			if f.carries(si, f.wordReads(src, cmd.Words[0]), sc) {
				return false
			}
		}
		// A dynamic call, a command word that is no literal or eval of
		// built text, can't be shown not to be a watched procedure: where
		// one exists, it fails the context outside a benign sink's
		// arguments and phase bodies (architecture's condition 5).
		if len(f.procs) > 0 && !sc.phase && f.dynamicOutsideSinks(src, cmd) {
			return false
		}
		// A watched option or procedure named to a handler, a trace or a
		// callback runs whatever that is when it's set or read, as the
		// github group's option handlers write master_sites and distname
		// (architecture's condition 1, as A2 for a procedure).
		if registrations[name] {
			for _, word := range cmd.Words[1:] {
				if named, ok := word.Literal(src); ok {
					if f.watched[named] || f.procs[named] || f.carried[named] {
						return false
					}
					f.placed[si] = append(f.placed[si], word.Span)
				}
			}
			continue
		}
		switch {
		case name == "proc":
			// Definitions are judged apart, the watched ones' bodies by
			// the rules of a procedure (A1).
			f.placed[si] = append(f.placed[si], cmd.Words[0].Span)
			if len(cmd.Words) > 1 {
				f.placed[si] = append(f.placed[si], cmd.Words[1].Span)
			}
			continue
		case name == "global" || name == "options" || name == "variable":
			// Declarations: a procedure's globals are watched where the
			// option is; an option declared is no read of it.
			for _, word := range cmd.Words[1:] {
				if variable, ok := word.Literal(src); ok {
					if sc.proc != nil && name == "global" {
						sc.globals[variable] = true
					}
					f.placed[si] = append(f.placed[si], word.Span)
				}
			}
			continue
		}
		if controls, bodies, ok := cmd.Control(src); ok {
			var reads []read
			expression := name == "if" || name == "while" || name == "for"
			for _, word := range controls {
				if expression {
					reads = append(reads, f.expressionReads(src, word.Inner())...)
				} else {
					reads = append(reads, f.wordReads(src, word)...)
				}
			}
			guarded := f.carries(si, reads, sc)
			if guarded && name == "foreach" {
				for i := 0; i+1 < len(controls); i += 2 {
					if names, ok := controls[i].Literal(src); ok {
						for _, variable := range strings.Fields(names) {
							f.taint(sc, variable, true)
						}
					}
				}
			}
			for _, body := range bodies {
				if guarded && !f.benignBody(si, body, sc) {
					return false
				}
				if script, ok := body.BracedScript(src); ok {
					if !f.walk(si, script, sc) {
						return false
					}
				}
			}
			continue
		}
		switch {
		case name == "set" && len(cmd.Words) == 3:
			if variable, ok := cmd.Words[1].Literal(src); ok {
				f.placed[si] = append(f.placed[si], cmd.Words[1].Span)
				f.taint(sc, variable, f.carries(si, f.wordReads(src, cmd.Words[2]), sc))
				continue
			}
		case name == "default" && len(cmd.Words) == 3:
			// A default's text is substituted where its option is read:
			// a watched value reaching it watches the option, which fails
			// the context where it's a field the model acts on (A4).
			if option, ok := cmd.Words[1].Literal(src); ok {
				f.placed[si] = append(f.placed[si], cmd.Words[1].Span)
				var reads []read
				if text, isScript := cmd.Words[2].BracedScript(src); isScript {
					for _, inner := range text.Direct() {
						for _, word := range inner.Words {
							reads = append(reads, f.wordReads(src, word)...)
						}
					}
				} else {
					reads = f.wordReads(src, cmd.Words[2])
				}
				if f.carries(si, reads, sc) {
					if f.acted(option) {
						return false
					}
					f.watch(option)
				}
				continue
			}
		case name == "return":
			var reads []read
			for _, word := range cmd.Words[1:] {
				reads = append(reads, f.wordReads(src, word)...)
			}
			if f.carries(si, reads, sc) && sc.proc != nil {
				sc.returns = true
			}
			continue
		case f.procs[name]:
			// A statement call is placed only for a procedure whose body
			// touches benign sinks alone; its arguments are its own reads
			// (A2).
			if f.returning[name] {
				return false
			}
			f.placed[si] = append(f.placed[si], cmd.Words[0].Span)
			for _, word := range cmd.Words[1:] {
				if f.carries(si, f.wordReads(src, word), sc) {
					return false
				}
			}
			continue
		}
		if f.procs[name] || f.watched[optionSuffix.ReplaceAllString(name, "")] {
			f.placed[si] = append(f.placed[si], cmd.Words[0].Span)
		}
		for i, word := range cmd.Words[1:] {
			if body, ok := word.BracedScript(src); ok {
				phase := sc.phase
				sc.phase = phase || phaseHook.MatchString(name)
				ok := f.walk(si, body, sc)
				sc.phase = phase
				if !ok {
					return false
				}
				continue
			}
			// A variant's, subport's or platform's body written as a
			// quoted string, as wireshark3's python variants are made in a
			// loop, is the script it runs once substituted: walked as one,
			// its reads judged where it puts them.
			if body, ok := quotedBody(src, cmd, name, i+1); ok {
				if !f.walk(si, body, sc) {
					return false
				}
				continue
			}
			if sinkFor(name) && name != "set" && name != "option" {
				if _, ok := word.Literal(src); ok {
					f.literal[si] = append(f.literal[si], word.Span)
				}
			}
			if f.carries(si, f.wordReads(src, word), sc) {
				if !sinkFor(name) {
					return false
				}
				if option, ok := optionSink(name); ok {
					f.watch(option)
				}
			}
		}
	}
	return true
}

// registrations are the commands that name an option, variable or
// procedure to run something when it's set or read, or to call it by
// another name.
var registrations = map[string]bool{"option_proc": true, "trace": true, "port::register_callback": true, "rename": true, "interp": true}

// dynamicOutsideSinks reports a command that calls what can't be named
// from the source, outside a benign sink's arguments: a command word that
// is no literal, eval or uplevel of anything but a braced script, or such
// a call in a command substitution of its arguments.
func (f *follower) dynamicOutsideSinks(src []byte, cmd syntax.Command) bool {
	dynamic := func(cmd syntax.Command) bool {
		name, literal := cmd.Name(src)
		if !literal {
			return true
		}
		if name == "eval" || name == "uplevel" {
			for _, word := range cmd.Words[1:] {
				if _, braced := word.BracedScript(src); !braced {
					if _, plain := word.Literal(src); !plain || name == "eval" {
						return true
					}
				}
			}
		}
		return false
	}
	if dynamic(cmd) {
		return true
	}
	name, _ := cmd.Name(src)
	if sinkFor(name) {
		return false
	}
	var inScript func(*syntax.Script) bool
	var inSegments func([]syntax.Segment) bool
	var inCommand func(syntax.Command, bool) bool
	// inExpression reports a dynamic call in an expression's window; one
	// that doesn't parse counts as one.
	inExpression := func(window textedit.Span) bool {
		e, errs := syntax.ParseExpr(src, window)
		if len(errs) > 0 {
			return true
		}
		found := false
		syntax.WalkExpr(e, func(node syntax.Expr) bool {
			switch node := node.(type) {
			case syntax.Call:
				found = found || inScript(node.Script)
				return false
			case syntax.Text:
				found = found || inSegments(node.Word.Segments)
				return false
			}
			return true
		})
		return found
	}
	inSegments = func(segments []syntax.Segment) bool {
		for _, segment := range segments {
			switch value := segment.(type) {
			case syntax.Quoted:
				if inSegments(value.Segments) {
					return true
				}
			case syntax.CmdSub:
				if inScript(value.Script) {
					return true
				}
			}
		}
		return false
	}
	// inCommand reports a dynamic call in a command's substitutions, its
	// braced conditions and expressions, and its control bodies; own
	// includes the command itself.
	inCommand = func(cmd syntax.Command, own bool) bool {
		if len(cmd.Words) == 0 {
			return false
		}
		if own && dynamic(cmd) {
			return true
		}
		for _, word := range cmd.Words[1:] {
			if inSegments(word.Segments) {
				return true
			}
		}
		name, _ := cmd.Name(src)
		if name == "expr" {
			for _, word := range cmd.Words[1:] {
				if _, braced := word.BracedScript(src); braced || len(word.Segments) == 1 {
					if inExpression(word.Inner()) {
						return true
					}
				}
			}
		}
		if controls, bodies, ok := cmd.Control(src); ok {
			if name == "if" || name == "while" || name == "for" {
				for _, word := range controls {
					if inExpression(word.Inner()) {
						return true
					}
				}
			}
			if own {
				for _, body := range bodies {
					if script, ok := body.BracedScript(src); ok && inScript(script) {
						return true
					}
				}
			}
		}
		return false
	}
	inScript = func(script *syntax.Script) bool {
		for _, nested := range script.Direct() {
			if inCommand(nested, true) {
				return true
			}
		}
		return false
	}
	// The command's own bodies are walked as scripts in their turn.
	return inCommand(cmd, false)
}

// quotedBody is the script a quoted last word of a variant, subport or
// platform is, where it parses as one.
func quotedBody(src []byte, cmd syntax.Command, name string, at int) (*syntax.Script, bool) {
	if name != "variant" && name != "subport" && name != "platform" || at != len(cmd.Words)-1 {
		return nil, false
	}
	word := cmd.Words[at]
	if word.Expand || len(word.Segments) != 1 {
		return nil, false
	}
	if _, quoted := word.Segments[0].(syntax.Quoted); !quoted {
		return nil, false
	}
	script, errs := syntax.ParseScript(src, word.Inner())
	return script, len(errs) == 0
}

// benignBody accepts a branch a watched read selects only where every
// command in it, at any depth, is a benign sink, a control structure whose
// bodies are, a post-extraction hook whose body is, a set, a return, or a
// statement call of a watched procedure that returns nothing watched; an
// option it sets is watched, its value chosen by the read.
func (f *follower) benignBody(si int, body syntax.Word, sc *scope) bool {
	src := f.srcs[si]
	script, ok := body.BracedScript(src)
	if !ok {
		return false
	}
	for _, cmd := range script.Direct() {
		name, _ := cmd.Name(src)
		_, nested, control := cmd.Control(src)
		if !control && phaseHook.MatchString(name) {
			control = true
			for _, word := range cmd.Words[1:] {
				if _, ok := word.BracedScript(src); ok {
					nested = append(nested, word)
				}
			}
		}
		switch {
		case control:
			for _, word := range nested {
				if !f.benignBody(si, word, sc) {
					return false
				}
			}
		case name == "set" && len(cmd.Words) == 3:
			if variable, ok := cmd.Words[1].Literal(src); ok {
				f.taint(sc, variable, true)
				continue
			}
			return false
		case name == "return":
			if sc.proc != nil {
				sc.returns = true
			}
		case f.procs[name] && !f.returning[name]:
		case sinkFor(name):
			if option, ok := optionSink(name); ok {
				f.watch(option)
			}
		default:
			return false
		}
	}
	return true
}

// wordReads are the reads in a word: its variables, and in its command
// substitutions, at any depth, theirs, the options read by name and the
// procedures called.
func (f *follower) wordReads(src []byte, word syntax.Word) []read {
	return f.segmentReads(src, word.Segments)
}

func (f *follower) segmentReads(src []byte, segments []syntax.Segment) []read {
	var reads []read
	for _, segment := range segments {
		switch value := segment.(type) {
		case syntax.VarSub:
			reads = append(reads, read{name: value.Name.Text(src), kind: readVariable, span: value.Span})
		case syntax.Quoted:
			reads = append(reads, f.segmentReads(src, value.Segments)...)
		case syntax.CmdSub:
			reads = append(reads, f.scriptReads(src, value.Script)...)
		}
	}
	return reads
}

func (f *follower) scriptReads(src []byte, script *syntax.Script) []read {
	var reads []read
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		if len(cmd.Words) == 0 {
			continue
		}
		name, _ := cmd.Name(src)
		if name != "" {
			reads = append(reads, read{name: name, kind: readCall, span: cmd.Words[0].Span})
		}
		if (namedReads[name] && len(cmd.Words) >= 2) || (name == "set" && len(cmd.Words) == 2) {
			if option, ok := cmd.Words[1].Literal(src); ok {
				reads = append(reads, read{name: option, kind: readOption, span: cmd.Words[1].Span})
			}
		}
		for _, word := range cmd.Words {
			reads = append(reads, f.segmentReads(src, word.Segments)...)
		}
	}
	return reads
}

// expressionReads are the reads in an expression's window, or, where it
// doesn't parse as one, in it as a script.
func (f *follower) expressionReads(src []byte, window textedit.Span) []read {
	e, errs := syntax.ParseExpr(src, window)
	if len(errs) > 0 {
		script, errs := syntax.ParseScript(src, window)
		if len(errs) > 0 {
			// Unreadable: every name in it counts as read.
			return []read{{name: "", kind: readVariable, span: window}}
		}
		return f.scriptReads(src, script)
	}
	var reads []read
	syntax.WalkExpr(e, func(node syntax.Expr) bool {
		switch node := node.(type) {
		case syntax.Variable:
			reads = append(reads, read{name: node.Name.Text(src), kind: readVariable, span: node.Span})
		case syntax.Call:
			reads = append(reads, f.scriptReads(src, node.Script)...)
			return false
		case syntax.Text:
			reads = append(reads, f.wordReads(src, node.Word)...)
			return false
		}
		return true
	})
	return reads
}

// accounted reports whether every mention in a source of a watched option,
// procedure, or carried variable, outside comments, is one the walk
// placed; a whole literal word of a benign sink's arguments places an
// option or variable, never a procedure (B). A local carried variable is
// accounted within its procedure's body.
func (f *follower) accounted(si int) bool {
	src := f.srcs[si]
	var comments []textedit.Span
	var walk func(*syntax.Script)
	walk = func(script *syntax.Script) {
		for _, item := range script.Items {
			switch item := item.(type) {
			case syntax.Comment:
				comments = append(comments, item.Span)
			case syntax.Command:
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
	walk(f.scripts[si])
	within := func(spans []textedit.Span, offset int) bool {
		return slices.ContainsFunc(spans, func(span textedit.Span) bool { return span.Start <= offset && offset < span.End })
	}
	text := string(src)
	check := func(name string, from, to int) bool {
		for _, at := range mentionsOf(text[from:to], name) {
			at += from
			switch {
			case within(comments, at), within(f.placed[si], at):
			case !f.procs[name] && within(f.literal[si], at):
			default:
				return false
			}
		}
		return true
	}
	for _, names := range []map[string]bool{f.watched, f.procs, f.carried} {
		for name := range names {
			if !check(name, 0, len(text)) {
				return false
			}
		}
	}
	for _, def := range f.defs {
		if def.source != si {
			continue
		}
		for name, carries := range f.locals[def.span] {
			if carries && !check(name, def.span.Start, def.span.End) {
				return false
			}
		}
	}
	return true
}

// mentionsOf are the offsets in text where name stands as a name: not
// inside a longer one.
func mentionsOf(text, name string) []int {
	nameByte := func(c byte, dot bool) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || dot && c == '.'
	}
	var found []int
	if name == "" {
		return nil
	}
	for from := 0; ; {
		at := strings.Index(text[from:], name)
		if at < 0 {
			return found
		}
		at += from
		from = at + 1
		if at > 0 && nameByte(text[at-1], true) || at+len(name) < len(text) && nameByte(text[at+len(name)], false) {
			continue
		}
		found = append(found, at)
	}
}

var (
	baseOptionsMu    sync.Mutex
	baseOptionsCache = map[string]map[string]bool{}
)

// baseOptions are the options Base's port1.0 declares, read from the
// library's own source: its fields.
func baseOptions(library string) map[string]bool {
	if library == "" {
		return nil
	}
	baseOptionsMu.Lock()
	defer baseOptionsMu.Unlock()
	if found, ok := baseOptionsCache[library]; ok {
		return found
	}
	found := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(library, "port1.0", "*.tcl"))
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		script, errs := syntax.Parse(src)
		if len(errs) > 0 {
			continue
		}
		for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
			if name, _ := cmd.Name(src); name != "options" {
				continue
			}
			for _, word := range cmd.Words[1:] {
				if option, ok := word.Literal(src); ok {
					found[option] = true
				}
			}
		}
	}
	baseOptionsCache[library] = found
	return found
}
