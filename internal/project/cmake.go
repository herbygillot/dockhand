package project

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// CMakeDocument is a CMakeLists.txt read once, as CMake reads it: its
// commands, the conditional blocks they're within, each if() with its
// elseif() and else() branches, and the files it include()s, read in
// their place, since include() reads a file into the same scope. What
// it declares (Facts), the options nothing turns on (Off), and what the
// default build reads of it, each statement with the conditions it's
// under (Statements), are all read from it, so a block has one identity
// whether it's judged unreached or named where a change is: the
// comparison had read conditions again, line by line, and a multiline
// if( read as "outside any if()" (the architecture review's finding 3).
// It's CMake's own commands, read as written: what a variable holds, as
// in set(FLB_${x} ON), isn't followed.
type CMakeDocument struct {
	nodes    []cmakeNode
	commands []cmakeCommand
	// source is each file's text as read, the root's first, then each it
	// include()s, by its path; included are those files' paths.
	source   []string
	included []string
}

// cmakeNode is a command, or an if() block.
type cmakeNode struct {
	command int
	block   *cmakeBlock
}

// cmakeBlock is an if() block: its branches, the if() and each elseif()
// and else() of its own, and its endif(), -1 for one never closed.
type cmakeBlock struct {
	branches []cmakeBranch
	end      int
}

// cmakeBranch is one branch of a block: the command heading it, and what
// it holds.
type cmakeBranch struct {
	head int
	body []cmakeNode
}

// CMakeStatement is a command as the default build reads it, written in
// one line, with the conditions of the if() blocks it's within, outermost
// first, and the file it's in, empty for the CMakeLists.txt itself. A
// block's if(), elseif(), else(), and endif() are within it.
type CMakeStatement struct {
	Text  string
	Under []CMakeCondition
	File  string
}

// CMakeCondition is a branch's condition: as written, "FLB_TLS AND (NOT
// FLB_SYSTEM_WINDOWS)", and the names it tests. An else() is the branch
// before it negated, "NOT (FLB_TLS)", and Else.
type CMakeCondition struct {
	Text  string
	Names []string
	Else  bool
}

// Tests reports whether a condition tests a name, as itself and not
// negated by an else().
func (c CMakeCondition) Tests(name string) bool {
	return !c.Else && slices.Contains(c.Names, name)
}

// cmakeIncludeDepth is how deep include()s are followed, an included file
// including another; a cycle stops at it too.
const cmakeIncludeDepth = 8

// ParseCMake reads a CMakeLists.txt alone, its include()s not followed.
func ParseCMake(data []byte) CMakeDocument {
	return parseCMake(string(data), "", nil)
}

// CMakeDocument reads the CMakeLists.txt at name with each file it
// include()s that the reading has (cmakeIncludes), each read in its place.
func (r Reading) CMakeDocument(name string) CMakeDocument {
	file := r.Files[name]
	return parseCMake(string(file.Data), path.Dir(name), func(include string) (string, bool) {
		found, ok := r.Files[include]
		if !ok || found.Truncated {
			return "", false
		}
		return string(found.Data), true
	})
}

// parseCMake reads a CMake file whose directory is dir, below the
// archive's top, splicing in what include() reads, where read finds it.
func parseCMake(text, dir string, read func(string) (string, bool)) CMakeDocument {
	var d CMakeDocument
	d.source = append(d.source, text)
	seen := map[string]bool{}
	var splice func(text, file string, depth int)
	splice = func(text, file string, depth int) {
		commands, _ := cmakeScan(text)
		var modules []string
		for _, command := range commands {
			command.file = file
			d.commands = append(d.commands, command)
			if read == nil || depth >= cmakeIncludeDepth {
				continue
			}
			modules = append(modules, cmakeModulePath(command, dir, file)...)
			for _, candidate := range cmakeIncluded(command, dir, file, modules) {
				if seen[candidate] {
					break
				}
				if data, ok := read(candidate); ok {
					seen[candidate] = true
					d.source = append(d.source, candidate+"\n"+data)
					d.included = append(d.included, candidate)
					splice(data, candidate, depth+1)
					break
				}
			}
		}
	}
	splice(text, "", 0)
	at := 0
	d.nodes = cmakeTree(d.commands, &at, false)
	return d
}

// Included are the files the document include()s that were read, by
// their paths below the archive's top.
func (d CMakeDocument) Included() []string { return slices.Clone(d.included) }

// cmakeSourceVariables are what a file's path may start from: the source
// directory, as the root's, or the directory of the file being read.
var cmakeSourceVariables = []string{"${CMAKE_CURRENT_SOURCE_DIR}", "${CMAKE_SOURCE_DIR}", "${PROJECT_SOURCE_DIR}"}

// cmakePath is a path an include() or the module path names, below the
// archive's top, from dir, the CMakeLists.txt's directory, and file, the
// one naming it; false for one built from any other variable.
func cmakePath(written, dir, file string) (string, bool) {
	for _, variable := range cmakeSourceVariables {
		written = strings.ReplaceAll(written, variable, dir)
	}
	current := dir
	if file != "" {
		current = path.Dir(file)
	}
	written = strings.ReplaceAll(written, "${CMAKE_CURRENT_LIST_DIR}", current)
	if strings.Contains(written, "${") {
		return "", false
	}
	if !path.IsAbs(written) {
		written = path.Join(dir, written)
	}
	written = strings.TrimPrefix(path.Clean(written), "/")
	return written, written != "" && !strings.HasPrefix(written, "..")
}

// cmakeModulePath are the directories a set() or list() of
// CMAKE_MODULE_PATH adds, where include() looks for a module by its name.
func cmakeModulePath(command cmakeCommand, dir, file string) []string {
	args := command.args
	switch {
	case command.name == "set" && len(args) > 1 && args[0] == "CMAKE_MODULE_PATH":
		args = args[1:]
	case command.name == "list" && len(args) > 2 && args[1] == "CMAKE_MODULE_PATH" && slices.Contains([]string{"APPEND", "PREPEND", "INSERT"}, args[0]):
		args = args[2:]
		if command.args[0] == "INSERT" && len(args) > 0 {
			args = args[1:]
		}
	default:
		return nil
	}
	var dirs []string
	for _, arg := range args {
		for _, part := range strings.Split(arg, ";") {
			if part == "${CMAKE_MODULE_PATH}" {
				continue
			}
			if found, ok := cmakePath(part, dir, file); ok {
				dirs = append(dirs, found)
			}
		}
	}
	return dirs
}

// cmakeIncluded are where the file an include() names may be, in the
// order CMake looks (the cmake-commands manual, include): a file's path,
// from the source directory, or a module's name, as name.cmake in each
// directory of the module path. CMake's own modules, as GNUInstallDirs,
// aren't the archive's, and aren't found.
func cmakeIncluded(command cmakeCommand, dir, file string, modules []string) []string {
	if command.name != "include" || len(command.args) == 0 {
		return nil
	}
	written := command.args[0]
	if strings.Contains(written, "/") || strings.HasSuffix(written, ".cmake") {
		if found, ok := cmakePath(written, dir, file); ok {
			return []string{found}
		}
		return nil
	}
	if strings.Contains(written, "${") {
		return nil
	}
	var candidates []string
	for _, module := range modules {
		candidates = append(candidates, path.Join(module, written+".cmake"))
	}
	return candidates
}

// cmakeTree is the commands from at as nodes, an if() through its endif()
// one block, until the end, or, inside a block, the elseif(), else(), or
// endif() that ends its branch. One of those outside any block is a
// command like any other.
func cmakeTree(commands []cmakeCommand, at *int, inside bool) []cmakeNode {
	var nodes []cmakeNode
	for *at < len(commands) {
		switch commands[*at].name {
		case "elseif", "else", "endif":
			if inside {
				return nodes
			}
		case "if":
			block := &cmakeBlock{end: -1}
			head := *at
			for {
				*at++
				block.branches = append(block.branches, cmakeBranch{head: head, body: cmakeTree(commands, at, true)})
				if *at >= len(commands) {
					break
				}
				if commands[*at].name == "endif" {
					block.end = *at
					*at++
					break
				}
				head = *at
			}
			nodes = append(nodes, cmakeNode{block: block})
			continue
		}
		nodes = append(nodes, cmakeNode{command: *at})
		*at++
	}
	return nodes
}

// conditions are each branch's condition, as written, an else() the
// branch before it negated.
func (d CMakeDocument) conditions(branches []cmakeBranch) []CMakeCondition {
	var conditions []CMakeCondition
	for k, branch := range branches {
		head := d.commands[branch.head]
		if head.name == "else" && k > 0 {
			before := conditions[k-1]
			conditions = append(conditions, CMakeCondition{Text: "NOT (" + before.Text + ")", Names: before.Names, Else: true})
			continue
		}
		conditions = append(conditions, CMakeCondition{Text: strings.Join(head.args, " "), Names: cmakeNames(head.args)})
	}
	return conditions
}

// cmakeNames are the names a condition tests: its words but for CMake's
// operators and constants, a variable's reference as its name.
func cmakeNames(args []string) []string {
	var names []string
	for _, token := range cmakeTokens(args) {
		if name, ok := strings.CutPrefix(token, "${"); ok {
			token = strings.TrimSuffix(name, "}")
		}
		if token == "(" || token == ")" || cmakeOperators[token] || !isIdentStart(token[0]) {
			continue
		}
		if b := cmakeBool(token); b == "ON" || b == "OFF" {
			continue
		}
		if !slices.Contains(names, token) {
			names = append(names, token)
		}
	}
	return names
}

// cmakeOperators are the words of an if() condition that are CMake's own.
var cmakeOperators = map[string]bool{
	"NOT": true, "AND": true, "OR": true, "COMMAND": true, "POLICY": true, "TARGET": true, "TEST": true, "DEFINED": true,
	"EXISTS": true, "IS_NEWER_THAN": true, "IS_DIRECTORY": true, "IS_SYMLINK": true, "IS_ABSOLUTE": true, "IS_READABLE": true,
	"IS_WRITABLE": true, "IS_EXECUTABLE": true, "MATCHES": true, "LESS": true, "GREATER": true, "EQUAL": true,
	"LESS_EQUAL": true, "GREATER_EQUAL": true, "STRLESS": true, "STRGREATER": true, "STREQUAL": true, "STRLESS_EQUAL": true,
	"STRGREATER_EQUAL": true, "VERSION_LESS": true, "VERSION_GREATER": true, "VERSION_EQUAL": true,
	"VERSION_LESS_EQUAL": true, "VERSION_GREATER_EQUAL": true, "IN_LIST": true, "PATH_EQUAL": true,
}

// Facts are the options the document offers and the packages it finds.
func (d CMakeDocument) Facts() CMakeFacts {
	facts := CMakeFacts{Options: map[string]CMakeOption{}}
	d.walk(d.nodes, nil, nil, func(command cmakeCommand, under []CMakeCondition) {
		args := command.args
		switch command.name {
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
				return
			}
			found := CMakePackage{Name: args[0]}
			for _, condition := range under {
				found.Under = append(found.Under, condition.Text)
			}
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
	})
	return facts
}

// walk visits each command of nodes the default build reads where each
// option of off is OFF, with the conditions it's under; every command
// where off is nil. A branch whose condition is false there isn't
// walked; one taken in a cut branch's place is read as the default build
// reads it: an elseif() after a cut if() opens the block, and an else()
// after one is no longer conditional. Block heads and endif() are visited
// too, as they read.
func (d CMakeDocument) walk(nodes []cmakeNode, under []CMakeCondition, off map[string]bool, visit func(cmakeCommand, []CMakeCondition)) {
	for _, node := range nodes {
		if node.block == nil {
			visit(d.commands[node.command], under)
			continue
		}
		block := node.block
		var kept []cmakeBranch
		for _, branch := range block.branches {
			head := d.commands[branch.head]
			if head.name != "else" && cmakeCondition(head.args, off) == cmakeFalse {
				continue
			}
			kept = append(kept, branch)
		}
		if len(kept) == 0 {
			continue
		}
		if d.commands[kept[0].head].name == "else" && kept[0].head != block.branches[0].head {
			d.walk(kept[0].body, under, off, visit)
			continue
		}
		conditions := d.conditions(kept)
		for k, branch := range kept {
			head := d.commands[branch.head]
			if k == 0 && head.name == "elseif" {
				head.name = "if"
			}
			inner := append(slices.Clone(under), conditions[k])
			visit(head, inner)
			d.walk(branch.body, inner, off, visit)
		}
		if block.end >= 0 {
			visit(d.commands[block.end], append(slices.Clone(under), conditions[len(conditions)-1]))
		}
	}
}

// Statements are what the default build reads of the document, where
// each option of off is OFF, less the commands leave names, in order,
// each written in one line. Comments aren't commands, and aren't read.
func (d CMakeDocument) Statements(leave func(name string, args []string) bool, off map[string]bool) []CMakeStatement {
	var statements []CMakeStatement
	d.walk(d.nodes, nil, off, func(command cmakeCommand, under []CMakeCondition) {
		if leave != nil && leave(command.name, command.args) {
			return
		}
		statements = append(statements, CMakeStatement{Text: command.line(), Under: under, File: command.file})
	})
	return statements
}

// Without are the document's statements less the options named, their
// option() and cmake_dependent_option() commands, where each option of
// off is OFF, for comparing what else changed beside options a version
// adds: the person decided an added option holds nothing, built as its
// default, and that what an option off by default gates, which nothing
// turns on, is not reached by the default build (D12, revisited
// 2026-10-01).
func (d CMakeDocument) Without(options, off map[string]bool) []CMakeStatement {
	return d.Statements(func(name string, args []string) bool {
		return (name == "option" || name == "cmake_dependent_option") && len(args) > 0 && options[args[0]]
	}, off)
}

// Rest are the document's statements less what Facts reads of it, every
// option and every find_package, where each option of off is OFF, for
// saying what else changed beside what it says.
func (d CMakeDocument) Rest(off map[string]bool) []CMakeStatement {
	return d.Statements(func(name string, _ []string) bool {
		switch name {
		case "option", "cmake_dependent_option", "find_package":
			return true
		}
		return false
	}, off)
}

// Source is the document's text as read, the CMakeLists.txt's and then
// each file it include()s, each after its path, for telling whether
// anything of it changed.
func (d CMakeDocument) Source() string {
	return strings.Join(d.source, "\n")
}

// StatementsText are statements one to a line.
func StatementsText(statements []CMakeStatement) string {
	lines := make([]string, len(statements))
	for i, statement := range statements {
		lines[i] = statement.Text
	}
	return strings.Join(lines, "\n")
}

// line is a command written in one line: its name, and its arguments
// separated by a space, one that's empty or holds a space quoted.
func (c cmakeCommand) line() string {
	args := make([]string, len(c.args))
	for i, arg := range c.args {
		if arg == "" || strings.ContainsAny(arg, " \t") && c.name != "if" && c.name != "elseif" && c.name != "while" {
			arg = `"` + arg + `"`
		}
		args[i] = arg
	}
	return c.name + "(" + strings.Join(args, " ") + ")"
}

// CMakeFacts are what a CMakeLists.txt declares that a port's build may
// need to follow: the options it offers, with their defaults, and the
// packages it finds, each with the if() conditions it's found under.
// fluent-bit's "CMakeLists.txt changed; the build may need the Portfile to
// follow" sent the person to the diff, where nothing concerned the port
// (the fluent-bit run, batch 23); these let the comparison say what
// changed.
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

// Off are the options the document declares off by default that nothing
// turns on: each declared only by option(), off in each, whose name no
// other command but the if()s that test it, message(), and a
// cmake_dependent_option() depending on it, takes as an argument, as
// set(), or fluent-bit's FLB_OPTION() macro, would to set it, in the file
// or one it include()s, and that named, the Portfile's mention, doesn't
// name. A name built from a variable, as set(FLB_${x} ON), isn't seen;
// the person took that bound with the guards (D12, revisited 2026-10-01).
func (d CMakeDocument) Off(named func(string) bool) map[string]bool {
	off := map[string]bool{}
	for _, command := range d.commands {
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
	for _, command := range d.commands {
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

// cmakeTruth is what an if() condition is known to be where the options
// off are OFF: false, true, or unknown, as one that tests anything else is.
type cmakeTruth int

const (
	cmakeUnknown cmakeTruth = iota
	cmakeFalse
	cmakeTrue
)

// cmakeTokens are a condition's words, its parentheses apart.
func cmakeTokens(args []string) []string {
	var tokens []string
	for _, arg := range args {
		tokens = append(tokens, strings.Fields(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(arg))...)
	}
	return tokens
}

// cmakeCondition is what an if() condition is where each of off is OFF,
// read as CMake reads it: parentheses first, then a test, NOT, AND, and
// OR. A variable is false where it's one of off, and a constant is what it
// says; any other variable, and any test, as STREQUAL or DEFINED, is
// unknown, which a branch is kept for.
func cmakeCondition(args []string, off map[string]bool) cmakeTruth {
	tokens := cmakeTokens(args)
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
	// its closing parenthesis, and file the file it's in, empty for the
	// CMakeLists.txt itself.
	start, end int
	file       string
}

// cmakeScan splits a CMake file into its command invocations, as CMake's
// grammar has them: an identifier, then its arguments in parentheses,
// which may nest, be quoted, or be bracketed; with # comments and bracket
// comments passed over, and where they are, each to its line's end.
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
