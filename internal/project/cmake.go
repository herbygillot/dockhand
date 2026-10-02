package project

import (
	"regexp"
	"slices"
	"strings"
)

// CMakeFacts are what a CMakeLists.txt declares that a port's build may
// need to follow: the options it offers, with their defaults, and the
// packages it finds, each with the if() conditions it's found under.
// fluent-bit's "CMakeLists.txt changed; the build may need the Portfile to
// follow" sent the person to the diff, where nothing concerned the port
// (the fluent-bit run, batch 23); these let the comparison say what
// changed. It's CMake's own commands, read as written: what a variable
// holds, or a file include()s, isn't followed.
type CMakeFacts struct {
	// Options are each option() and cmake_dependent_option(), by name.
	Options map[string]CMakeOption
	// Packages are each find_package(), in the order the file declares
	// them.
	Packages []CMakePackage
}

// CMakeOption is an option a CMakeLists.txt offers: its default as
// written, OFF where option() names none, and whether it depends on other
// options, as cmake_dependent_option()'s does.
type CMakeOption struct {
	Default   string
	Dependent bool
}

// CMakePackage is a package a CMakeLists.txt finds: its name, the version
// it asks for, where it names one, whether it's REQUIRED, and the if()
// conditions it's found under, outermost first.
type CMakePackage struct {
	Name, Version string
	Required      bool
	Under         []string
}

// cmakeVersion is how find_package names the version it asks for.
var cmakeVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*(\.\.\.[0-9.]+)?$`)

// ReadCMake reads a CMakeLists.txt's options and the packages it finds,
// tolerantly: what isn't one of those commands, or can't be read as one,
// is passed over.
func ReadCMake(data []byte) CMakeFacts {
	facts := CMakeFacts{Options: map[string]CMakeOption{}}
	var under []string
	for _, command := range cmakeCommands(string(data)) {
		args := command.args
		switch command.name {
		case "if":
			under = append(under, strings.Join(args, " "))
		case "elseif":
			if len(under) > 0 {
				under[len(under)-1] = strings.Join(args, " ")
			}
		case "else":
			if len(under) > 0 {
				under[len(under)-1] = "not (" + under[len(under)-1] + ")"
			}
		case "endif":
			if len(under) > 0 {
				under = under[:len(under)-1]
			}
		case "option":
			if len(args) > 0 {
				option := CMakeOption{Default: "OFF"}
				if len(args) > 2 {
					option.Default = cmakeBool(args[2])
				}
				facts.Options[args[0]] = option
			}
		case "cmake_dependent_option":
			if len(args) > 2 {
				facts.Options[args[0]] = CMakeOption{Default: cmakeBool(args[2]), Dependent: true}
			}
		case "find_package":
			if len(args) == 0 {
				continue
			}
			found := CMakePackage{Name: args[0], Under: append([]string(nil), under...)}
			for i, arg := range args[1:] {
				switch {
				case i == 0 && cmakeVersion.MatchString(arg):
					found.Version = arg
				case arg == "REQUIRED":
					found.Required = true
				}
			}
			facts.Packages = append(facts.Packages, found)
		}
	}
	return facts
}

// CMakeWithout is a CMakeLists.txt less the options named, their option()
// and cmake_dependent_option() commands, less each if() branch the default
// build can't take, as CMakeLess has it, and less its comments, for
// comparing what else changed beside options a version adds: the person
// decided an added option holds nothing, built as its default, and that
// what an option off by default gates, which nothing turns on, is not
// reached by the default build (D12, revisited 2026-10-01).
func CMakeWithout(data []byte, options, off map[string]bool) []byte {
	return cmakeLess(data, func(command cmakeCommand) bool {
		return (command.name == "option" || command.name == "cmake_dependent_option") && len(command.args) > 0 && options[command.args[0]]
	}, off)
}

// CMakeRest is a CMakeLists.txt less what ReadCMake reads of it, every
// option and every find_package, less each if() branch the default build
// can't take, and less its comments, for saying what else changed beside
// what it says.
func CMakeRest(data []byte, off map[string]bool) []byte {
	return cmakeLess(data, func(command cmakeCommand) bool {
		switch command.name {
		case "option", "cmake_dependent_option", "find_package":
			return true
		}
		return false
	}, off)
}

// CMakeOff are the options a CMakeLists.txt declares off by default that
// nothing turns on: each declared only by option(), off in each, whose name
// no other command but the if()s that test it, message(), and a
// cmake_dependent_option() depending on it, takes as an argument, as
// set(), or fluent-bit's FLB_OPTION() macro, would to set it,
// and that named, the Portfile's mention, doesn't name. A name built from
// a variable, as set(FLB_${x} ON), or set in a file it include()s, isn't
// seen; the person took that bound with the guards (D12, revisited
// 2026-10-01).
func CMakeOff(data []byte, named func(string) bool) map[string]bool {
	commands := cmakeCommands(string(data))
	off := map[string]bool{}
	for _, command := range commands {
		if command.name != "option" || len(command.args) == 0 {
			continue
		}
		value := "OFF"
		if len(command.args) > 2 {
			value = cmakeBool(command.args[2])
		}
		if was, seen := off[command.args[0]]; !seen || was {
			off[command.args[0]] = value == "OFF"
		}
	}
	for _, command := range commands {
		switch command.name {
		case "option", "if", "elseif", "else", "endif", "while", "endwhile", "message":
			continue
		}
		args := command.args
		if command.name == "cmake_dependent_option" && len(args) > 0 {
			// It sets the option it declares; the rest it only tests.
			args = args[:1]
		}
		for _, arg := range args {
			if _, ok := off[arg]; ok {
				off[arg] = false
			}
		}
	}
	for name, unset := range off {
		if !unset || named(name) {
			delete(off, name)
		}
	}
	return off
}

// cmakeEdit replaces a span of a CMake file's text, to cut it where with
// is empty.
type cmakeEdit struct {
	start, end int
	with       string
}

// cmakeLess is a CMakeLists.txt less the commands leave names, less each
// if() branch whose condition is false where each option of off is OFF,
// as if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER) is with both off, and less
// its comments, with the lines that leaves empty. A branch taken in its
// place is left as the default build reads it: an elseif() after a cut
// if() opens the block, and an else() after one is no longer conditional.
func cmakeLess(data []byte, leave func(cmakeCommand) bool, off map[string]bool) []byte {
	text := string(data)
	commands, comments := cmakeScan(text)
	var edits []cmakeEdit
	var dead [][2]int
	gone := func(at int) bool {
		return slices.ContainsFunc(dead, func(span [2]int) bool { return at >= span[0] && at < span[1] })
	}
	for i, command := range commands {
		switch {
		case gone(command.start):
		case leave(command):
			edits = append(edits, cmakeEdit{start: command.start, end: command.end})
		case command.name == "if":
			heads, end := cmakeBranches(commands, i)
			if end < 0 {
				continue
			}
			var kept []int
			for k, head := range heads {
				next := end
				if k+1 < len(heads) {
					next = heads[k+1]
				}
				if commands[head].name != "else" && cmakeCondition(commands[head].args, off) == cmakeFalse {
					edits = append(edits, cmakeEdit{start: commands[head].start, end: commands[next].start})
					dead = append(dead, [2]int{commands[head].start, commands[next].start})
					continue
				}
				kept = append(kept, head)
			}
			switch {
			case len(kept) == len(heads):
			case len(kept) == 0:
				edits = append(edits, cmakeEdit{start: commands[end].start, end: commands[end].end})
			case kept[0] == heads[0]:
			case commands[kept[0]].name == "elseif":
				edits = append(edits, cmakeEdit{start: commands[kept[0]].start, end: commands[kept[0]].start + len("elseif"), with: "if"})
			default:
				edits = append(edits, cmakeEdit{start: commands[kept[0]].start, end: commands[kept[0]].end},
					cmakeEdit{start: commands[end].start, end: commands[end].end})
			}
		}
	}
	for _, comment := range comments {
		edits = append(edits, cmakeEdit{start: comment[0], end: comment[1]})
	}
	slices.SortStableFunc(edits, func(a, b cmakeEdit) int { return a.start - b.start })
	var kept strings.Builder
	at := 0
	for _, edit := range edits {
		if edit.start < at {
			continue
		}
		kept.WriteString(text[at:edit.start])
		kept.WriteString(edit.with)
		at = edit.end
	}
	kept.WriteString(text[at:])
	var lines []string
	for _, line := range strings.Split(kept.String(), "\n") {
		if line = strings.TrimRight(line, " \t\r"); strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// cmakeBranches are the commands heading each branch of the if() at i, it
// and each elseif() and else() of its own, and its endif(); -1 for an
// if() never closed.
func cmakeBranches(commands []cmakeCommand, i int) ([]int, int) {
	heads, depth := []int{i}, 0
	for j := i; j < len(commands); j++ {
		switch commands[j].name {
		case "if":
			depth++
		case "elseif", "else":
			if depth == 1 {
				heads = append(heads, j)
			}
		case "endif":
			if depth--; depth == 0 {
				return heads, j
			}
		}
	}
	return heads, -1
}

// cmakeTruth is what an if() condition is known to be where the options
// off are OFF: false, true, or unknown, as one that tests anything else is.
type cmakeTruth int

const (
	cmakeUnknown cmakeTruth = iota
	cmakeFalse
	cmakeTrue
)

// cmakeCondition is what an if() condition is where each of off is OFF,
// read as CMake reads it: parentheses first, then a test, NOT, AND, and
// OR. A variable is false where it's one of off, and a constant is what it
// says; any other variable, and any test, as STREQUAL or DEFINED, is
// unknown, which a branch is kept for.
func cmakeCondition(args []string, off map[string]bool) cmakeTruth {
	var tokens []string
	for _, arg := range args {
		tokens = append(tokens, strings.Fields(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(arg))...)
	}
	reader := cmakeConditionReader{tokens: tokens, off: off}
	truth := reader.or()
	if reader.at != len(tokens) {
		return cmakeUnknown
	}
	return truth
}

// cmakeConditionReader reads an if() condition's tokens, from at.
type cmakeConditionReader struct {
	tokens []string
	at     int
	off    map[string]bool
}

func (r *cmakeConditionReader) next() string {
	if r.at < len(r.tokens) {
		return r.tokens[r.at]
	}
	return ""
}

func (r *cmakeConditionReader) or() cmakeTruth {
	truth := r.and()
	for r.next() == "OR" {
		r.at++
		other := r.and()
		switch {
		case truth == cmakeTrue || other == cmakeTrue:
			truth = cmakeTrue
		case truth == cmakeFalse && other == cmakeFalse:
		default:
			truth = cmakeUnknown
		}
	}
	return truth
}

func (r *cmakeConditionReader) and() cmakeTruth {
	truth := r.not()
	for r.next() == "AND" {
		r.at++
		other := r.not()
		switch {
		case truth == cmakeFalse || other == cmakeFalse:
			truth = cmakeFalse
		case truth == cmakeTrue && other == cmakeTrue:
		default:
			truth = cmakeUnknown
		}
	}
	return truth
}

func (r *cmakeConditionReader) not() cmakeTruth {
	if r.next() != "NOT" {
		return r.term()
	}
	r.at++
	switch r.not() {
	case cmakeFalse:
		return cmakeTrue
	case cmakeTrue:
		return cmakeFalse
	}
	return cmakeUnknown
}

// term is a parenthesized condition, a variable or constant, or a test of
// several words, up to the AND, OR, or closing parenthesis after it.
func (r *cmakeConditionReader) term() cmakeTruth {
	if r.next() == "(" {
		r.at++
		truth := r.or()
		if r.next() != ")" {
			r.at = len(r.tokens) + 1
			return cmakeUnknown
		}
		r.at++
		return truth
	}
	start := r.at
	for r.at < len(r.tokens) && r.next() != "AND" && r.next() != "OR" && r.next() != ")" {
		r.at++
	}
	if r.at-start != 1 {
		return cmakeUnknown
	}
	word := r.tokens[start]
	if name, ok := strings.CutPrefix(word, "${"); ok && strings.HasSuffix(name, "}") {
		word = strings.TrimSuffix(name, "}")
	} else {
		switch cmakeBool(word) {
		case "ON":
			return cmakeTrue
		case "OFF":
			return cmakeFalse
		}
	}
	if r.off[word] {
		return cmakeFalse
	}
	return cmakeUnknown
}

// cmakeBool is an option's default as CMake reads a boolean constant, ON
// or OFF, whichever of its spellings it's written in: fluent-bit's "No"
// read as "added, no by default" (the dogfood run with 58e2d7eb). One that
// isn't a constant, as a variable's reference, is kept as written.
func cmakeBool(value string) string {
	switch upper := strings.ToUpper(value); {
	case upper == "ON", upper == "YES", upper == "TRUE", upper == "Y", upper == "1":
		return "ON"
	case upper == "OFF", upper == "NO", upper == "FALSE", upper == "N", upper == "0", upper == "", upper == "IGNORE",
		upper == "NOTFOUND", strings.HasSuffix(upper, "-NOTFOUND"):
		return "OFF"
	}
	return value
}

// cmakeCommand is one command invocation: its name, lower-cased, as CMake
// matches names, and its arguments, unquoted.
type cmakeCommand struct {
	name string
	args []string
	// start and end are where the invocation is in the text, its name to
	// its closing parenthesis.
	start, end int
}

// cmakeCommands splits a CMake file into its command invocations, as
// CMake's grammar has them: an identifier, then its arguments in
// parentheses, which may nest, be quoted, or be bracketed; with # comments
// and bracket comments passed over.
func cmakeCommands(text string) []cmakeCommand {
	commands, _ := cmakeScan(text)
	return commands
}

// cmakeScan is a CMake file's command invocations, as cmakeCommands has
// them, and where its comments are, each to its line's end.
func cmakeScan(text string) ([]cmakeCommand, [][2]int) {
	var commands []cmakeCommand
	var comments [][2]int
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '#':
			i = cmakeComment(text, i, &comments)
		case isIdentStart(c):
			start := i
			for i < len(text) && isIdentPart(text[i]) {
				i++
			}
			name := strings.ToLower(text[start:i])
			for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
				i++
			}
			if i >= len(text) || text[i] != '(' {
				continue
			}
			args, end := cmakeArgs(text, i+1, &comments)
			commands = append(commands, cmakeCommand{name: name, args: args, start: start, end: end})
			i = end
		default:
			i++
		}
	}
	return commands, comments
}

// cmakeComment passes over the comment at i, as skipCMakeComment does, and
// notes where it is, short of the newline that ends it.
func cmakeComment(text string, i int, comments *[][2]int) int {
	end := skipCMakeComment(text, i)
	stop := end
	if stop > i && text[stop-1] == '\n' {
		stop--
	}
	*comments = append(*comments, [2]int{i, stop})
	return end
}

// cmakeArgs reads a command's arguments from just inside its opening
// parenthesis to its closing one, and gives where it ended. Nested
// parentheses, as an if() condition's, are kept in its words.
func cmakeArgs(text string, i int, comments *[][2]int) ([]string, int) {
	var args []string
	var word strings.Builder
	depth := 0
	flush := func() {
		if word.Len() > 0 {
			args = append(args, word.String())
			word.Reset()
		}
	}
	for i < len(text) {
		c := text[i]
		switch {
		case c == '#':
			flush()
			i = cmakeComment(text, i, comments)
			continue
		case c == '"':
			end := i + 1
			for end < len(text) && text[end] != '"' {
				if text[end] == '\\' {
					end++
				}
				end++
			}
			word.WriteString(text[i+1 : min(end, len(text))])
			i = end + 1
			continue
		case c == '[' && bracketLevel(text, i) >= 0:
			level := bracketLevel(text, i)
			closing := "]" + strings.Repeat("=", level) + "]"
			start := i + 2 + level
			end := strings.Index(text[start:], closing)
			if end < 0 {
				return args, len(text)
			}
			word.WriteString(text[start : start+end])
			i = start + end + len(closing)
			continue
		case c == '(':
			depth++
			word.WriteByte(c)
		case c == ')' && depth == 0:
			flush()
			return args, i + 1
		case c == ')':
			depth--
			word.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if depth == 0 {
				flush()
			} else {
				word.WriteByte(' ')
			}
		default:
			word.WriteByte(c)
		}
		i++
	}
	flush()
	return args, len(text)
}

// skipCMakeComment passes over a comment from its #: a bracket comment to
// its closing bracket, any other to the line's end.
func skipCMakeComment(text string, i int) int {
	if level := bracketLevel(text, i+1); level >= 0 {
		closing := "]" + strings.Repeat("=", level) + "]"
		if end := strings.Index(text[i+3+level:], closing); end >= 0 {
			return i + 3 + level + end + len(closing)
		}
		return len(text)
	}
	if end := strings.IndexByte(text[i:], '\n'); end >= 0 {
		return i + end + 1
	}
	return len(text)
}

// bracketLevel is the number of = in a bracket's opening at i, [[ or
// [==[; -1 where there's none.
func bracketLevel(text string, i int) int {
	if i >= len(text) || text[i] != '[' {
		return -1
	}
	level := 0
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '=':
			level++
		case '[':
			return level
		default:
			return -1
		}
	}
	return -1
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }
