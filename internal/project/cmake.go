package project

import (
	"regexp"
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
// and cmake_dependent_option() commands, and less each if() block that
// gates on one of gates alone, as if(FLB_PROTOBUF_ENCODER) does, to its
// endif(), with the lines that leaves empty, for comparing what else
// changed beside options a version adds: the person decided an added
// option holds nothing, built as its default, and that what one off by
// default gates is not reached by the default build (D12, revisited
// 2026-10-01).
func CMakeWithout(data []byte, options, gates map[string]bool) []byte {
	return cmakeLess(data, func(command cmakeCommand) bool {
		return (command.name == "option" || command.name == "cmake_dependent_option") && len(command.args) > 0 && options[command.args[0]]
	}, gates)
}

// CMakeRest is a CMakeLists.txt less what ReadCMake reads of it, every
// option and every find_package, and less each if() block gating on one of
// gates alone, for saying what else changed beside what it says.
func CMakeRest(data []byte, gates map[string]bool) []byte {
	return cmakeLess(data, func(command cmakeCommand) bool {
		switch command.name {
		case "option", "cmake_dependent_option", "find_package":
			return true
		}
		return false
	}, gates)
}

// cmakeLess is a CMakeLists.txt less the commands leave names, and the
// if() blocks gating on one of gates alone, with the lines that leaves
// empty.
func cmakeLess(data []byte, leave func(cmakeCommand) bool, gates map[string]bool) []byte {
	text := string(data)
	commands := cmakeCommands(text)
	var cuts [][2]int
	for i := 0; i < len(commands); i++ {
		command := commands[i]
		switch {
		case leave(command):
			cuts = append(cuts, [2]int{command.start, command.end})
		case command.name == "if" && len(command.args) == 1 && gates[strings.TrimSuffix(strings.TrimPrefix(command.args[0], "${"), "}")]:
			depth, end := 0, len(text)
			for j := i; j < len(commands); j++ {
				switch commands[j].name {
				case "if":
					depth++
				case "endif":
					depth--
				}
				if depth == 0 {
					end, i = commands[j].end, j
					break
				}
			}
			cuts = append(cuts, [2]int{command.start, end})
			if depth != 0 {
				i = len(commands)
			}
		}
	}
	var kept strings.Builder
	at := 0
	for _, cut := range cuts {
		kept.WriteString(text[at:cut[0]])
		at = cut[1]
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
	var commands []cmakeCommand
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '#':
			i = skipCMakeComment(text, i)
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
			args, end := cmakeArgs(text, i+1)
			commands = append(commands, cmakeCommand{name: name, args: args, start: start, end: end})
			i = end
		default:
			i++
		}
	}
	return commands
}

// cmakeArgs reads a command's arguments from just inside its opening
// parenthesis to its closing one, and gives where it ended. Nested
// parentheses, as an if() condition's, are kept in its words.
func cmakeArgs(text string, i int) ([]string, int) {
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
			i = skipCMakeComment(text, i)
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
