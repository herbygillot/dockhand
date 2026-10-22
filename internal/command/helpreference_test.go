package command

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestEveryNamedCommandExists reads each "dockhand <command> --flag" the
// help, the README, docs/*.md, and the messages in dockhand's source name,
// and refuses one whose command or flag doesn't exist (part 1's hygiene: v0.3.0's docs taught
// auth login and providers setup tart after both had moved).
func TestEveryNamedCommandExists(t *testing.T) {
	t.Parallel()
	root := rootForReference(t)
	var problems []string
	check := func(where, text string, code bool) {
		for _, problem := range referenceProblems(root, text, code) {
			if _, ok := pastReferences[where+": "+problem]; !ok {
				problems = append(problems, where+": "+problem)
			}
		}
	}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		check("help of "+cmd.CommandPath(), cmd.Short+"\n"+cmd.Long+"\n"+cmd.Example, false)
		check("flags of "+cmd.CommandPath(), cmd.LocalFlags().FlagUsages(), false)
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
	for _, doc := range referenceDocs(t) {
		data, err := os.ReadFile(doc)
		require.NoError(t, err)
		check(doc, markdownCode(string(data)), true)
	}
	for where, literal := range sourceLiterals(t) {
		check(where, literal, false)
	}
	require.Empty(t, problems)
}

// rootForReference is the command tree Run builds (newRoot).
func rootForReference(t *testing.T) *cobra.Command {
	t.Helper()
	var s settings
	streams := runStreams(Streams{Out: io.Discard, Err: io.Discard}, nil)
	s.streams = streams
	root := newRoot(&s, streams)
	// Cobra adds --help and --version as it executes.
	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()
	return root
}

// referenceDocs are the README and docs/*.md that teach dockhand, apart
// from the roadmap, reviews, activity, and v2's, which record what was,
// and the design records below, which name commands proposed or retired.
func referenceDocs(t *testing.T) []string {
	docs := []string{"../../README.md"}
	matches, err := filepath.Glob("../../docs/*.md")
	require.NoError(t, err)
	for _, doc := range matches {
		if !designRecords[filepath.Base(doc)] {
			docs = append(docs, doc)
		}
	}
	return docs
}

// designRecords are docs/*.md that record a design rather than teach the
// commands: the roadmap; Design v3, whose sketches name info, db, logs
// --errors, and setup prefix, never built, and whose v2 table names old
// spellings on purpose; and the directions and v2-era notes, which name
// verify, publish, doctor, and why.
var designRecords = map[string]bool{
	"roadmap.md":         true,
	"design-v3.md":       true,
	"directions.md":      true,
	"build-platforms.md": true,
	"bump-coverage.md":   true,
}

// pastReferences are a doc's references to a command of an earlier
// release, on purpose.
var pastReferences = map[string]string{}

// markdownCode is a Markdown file's code: its fenced blocks and inline
// spans, where commands are named, one a line.
func markdownCode(text string) string {
	var code []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			code = append(code, line)
			continue
		}
		for _, span := range inlineCode.FindAllStringSubmatch(line, -1) {
			code = append(code, span[1])
		}
	}
	return strings.Join(code, "\n")
}

var inlineCode = regexp.MustCompile("`([^`]+)`")

// sourceLiterals are the strings of dockhand's own source, apart from
// tests, keyed by where each is: the Next: lines and refusals that name a
// command. A string is a literal, or one built by + or a format call (as
// fmt.Errorf("... %s --submit", next)), where a function's variable that
// holds a command stands for it and anything else for one word; built
// strings escaped the literals alone (the rc1 full stage: update --submit
// was offered after v0.4.0 removed it).
func sourceLiterals(t *testing.T) map[string]string {
	literals := map[string]string{}
	err := filepath.WalkDir("../../internal", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		for where, text := range fileStrings(files, file) {
			literals[where] = text
		}
		return nil
	})
	require.NoError(t, err)
	return literals
}

// fileStrings are a parsed file's strings that name dockhand, keyed by
// where each is.
func fileStrings(files *token.FileSet, file *ast.File) map[string]string {
	strs := map[string]string{}
	for _, decl := range file.Decls {
		built := builtStrings{commands: map[string]string{}}
		ast.Inspect(decl, func(node ast.Node) bool {
			text, ok := built.collect(node)
			if ok && (strings.Contains(text, "dockhand ") || bareCommand.MatchString(text)) {
				strs[files.Position(node.Pos()).String()] = text
			}
			return true
		})
	}
	return strs
}

// builtStrings renders one function's strings, keeping each variable
// assigned a command, in source order.
type builtStrings struct {
	commands map[string]string
}

// placeholder stands for a value the source doesn't spell: one word that's
// neither a command nor a flag.
const placeholder = "…"

// collect is the string node is, where it's one to check: a literal, a
// concatenation, or a format call. Assigning a command to a variable
// keeps it for later strings.
func (b builtStrings) collect(node ast.Node) (string, bool) {
	switch node := node.(type) {
	case *ast.BasicLit:
		return b.render(node)
	case *ast.BinaryExpr:
		if node.Op == token.ADD {
			return b.render(node)
		}
	case *ast.CallExpr:
		return b.format(node)
	case *ast.AssignStmt:
		if len(node.Lhs) == 1 && len(node.Rhs) == 1 {
			if name, ok := node.Lhs[0].(*ast.Ident); ok {
				text, ok := b.render(node.Rhs[0])
				held, holds := b.commands[name.Name]
				switch {
				case node.Tok == token.ADD_ASSIGN && holds:
					b.commands[name.Name] = held + b.word(text, ok)
				case ok && strings.Contains(text, "dockhand "):
					b.commands[name.Name] = text
				}
			}
		}
	}
	return "", false
}

// render is a string expression's text, where it has a literal in it.
func (b builtStrings) render(expr ast.Expr) (string, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			text, err := strconv.Unquote(expr.Value)
			return text, err == nil
		}
	case *ast.ParenExpr:
		return b.render(expr.X)
	case *ast.BinaryExpr:
		if expr.Op != token.ADD {
			return "", false
		}
		x, xok := b.render(expr.X)
		y, yok := b.render(expr.Y)
		if !xok && !yok {
			return "", false
		}
		return b.word(x, xok) + b.word(y, yok), true
	case *ast.Ident:
		text, ok := b.commands[expr.Name]
		return text, ok
	case *ast.CallExpr:
		return b.format(expr)
	}
	return "", false
}

func (b builtStrings) word(text string, ok bool) string {
	if ok {
		return text
	}
	return placeholder
}

// format is a format call's text, a function named …f whose first
// literal argument is the format and those after it its arguments.
func (b builtStrings) format(call *ast.CallExpr) (string, bool) {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	}
	if !strings.HasSuffix(name, "f") {
		return "", false
	}
	for i, arg := range call.Args {
		literal, ok := arg.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			continue
		}
		format, err := strconv.Unquote(literal.Value)
		if err != nil || !strings.Contains(format, "%") {
			return "", false
		}
		args := call.Args[i+1:]
		return verb.ReplaceAllStringFunc(format, func(v string) string {
			if v == "%%" {
				return "%"
			}
			if len(args) == 0 {
				return placeholder
			}
			arg := args[0]
			args = args[1:]
			if v == "%s" || v == "%v" || v == "%w" {
				if text, ok := b.render(arg); ok {
					return text
				}
			}
			return placeholder
		}), true
	}
	return "", false
}

// verb is a format's verb, as %s, %-8s, or %%.
var verb = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)

// referenceProblems names each "dockhand <command> [--flag]" in text whose
// command or flag doesn't exist. In code every one is a command; in prose,
// a help or a message, one is where a command is offered, after "run",
// "then", a colon, and the like, and elsewhere a word after dockhand
// that names no command is a sentence about it ("what dockhand runs
// with"), whose flags are still checked where it does name one.
func referenceProblems(root *cobra.Command, text string, code bool) []string {
	var problems []string
	if !code {
		text = bareCommand.ReplaceAllStringFunc(text, func(line string) string {
			if child, _, err := root.Find(strings.Fields(line)[:1]); err == nil && child != root {
				return "dockhand " + line
			}
			return line
		})
	}
	for _, at := range named.FindAllStringSubmatchIndex(text, -1) {
		match := []string{text[at[0]:at[1]], text[at[2]:at[3]]}
		offered := code || offering.MatchString(text[:at[0]])
		cmd := root
		words := strings.Fields(match[1])
		i := 0
		for ; i < len(words); i++ {
			child, _, err := cmd.Find([]string{words[i]})
			if err != nil || child == cmd || child.Name() != words[i] && !child.HasAlias(words[i]) {
				break
			}
			cmd = child
		}
		if cmd == root && i < len(words) && !strings.HasPrefix(words[i], "-") {
			// A word that names no command: offered, it's a missing one;
			// otherwise a sentence about dockhand, whose words aren't flags.
			if offered && commandWord.MatchString(words[i]) && (code || !sentenceWords[words[i]]) {
				problems = append(problems, "dockhand "+words[i]+" names no command")
			}
			continue
		}
		for _, word := range words[i:] {
			name, short := flagName(word)
			switch {
			case name != "" && cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil:
				problems = append(problems, cmd.CommandPath()+" has no --"+name)
			case short != "" && cmd.Flags().ShorthandLookup(short) == nil && cmd.InheritedFlags().ShorthandLookup(short) == nil:
				problems = append(problems, cmd.CommandPath()+" has no -"+short)
			}
		}
	}
	return problems
}

// named is "dockhand" and what follows it up to where a sentence or a
// shell command goes on.
var named = regexp.MustCompile(`\bdockhand((?: +[^ \n,;|()"` + "`" + `]+)*)`)

var commandWord = regexp.MustCompile(`^[a-z][a-z-]*$`)

// bareCommand is a line of prose that starts with a command and a flag,
// without "dockhand", as outdated's help offered "update --outdated
// --mine" after --outdated had gone (the architecture review of 5897a82,
// R3-27); where its first word names one of dockhand's commands, it's
// read as one.
var bareCommand = regexp.MustCompile(`(?m)^[a-z][a-z-]* +-`)

// flagName is a word's long or short flag, as --name, --name=value, or -x.
func flagName(word string) (long, short string) {
	switch {
	case strings.HasPrefix(word, "--") && len(word) > 2:
		long, _, _ = strings.Cut(word[2:], "=")
		long = strings.TrimRight(long, ".:")
		if !commandWord.MatchString(long) {
			return "", ""
		}
		return long, ""
	case len(word) == 2 && word[0] == '-' && word[1] != '-':
		return "", word[1:]
	}
	return "", ""
}

// offering is the text before a command offered in prose: a colon or
// parenthesis, or a word that offers one. A literal that starts with
// dockhand is as often a sentence about it as a command.
var offering = regexp.MustCompile(`(?:[:(]\s*|\b(?:[Rr]un|then|[Tt]ry|[Uu]se|[Ss]ee|[Nn]ext:?)\s+)$`)

// sentenceWords begin a sentence about dockhand after a colon or a
// parenthesis in prose, as "(dockhand built jq with +doc)", or after
// "use", as "if you use dockhand at least once", never a command.
var sentenceWords = map[string]bool{
	"at": true, "builds": true, "built": true, "could": true, "evaluates": true,
	"has": true, "stopped": true, "was": true,
}

// What the reference test refuses, and what it reads as a sentence.
func TestReferenceProblems(t *testing.T) {
	t.Parallel()
	root := rootForReference(t)
	require.Equal(t, []string{"dockhand providers names no command", "dockhand check has no --frobnicate", "dockhand setup tart has no -q"},
		referenceProblems(root, "dockhand providers setup tart\ndockhand check -b jq --frobnicate\ndockhand setup tart -q", true))
	require.Equal(t, []string{"dockhand auth names no command", "dockhand status has no --everything"},
		referenceProblems(root, "run dockhand auth login, then dockhand status --everything", false))
	require.Equal(t, []string{"dockhand update has no --outdated"},
		referenceProblems(root, "Lists the ports.\n\nupdate --outdated --mine starts on them.\nport -v installs", false))
	require.Empty(t, referenceProblems(root, "what dockhand runs with; dockhand can't. --on says where (dockhand built jq with +doc); Next: dockhand check -b jq --on tart:26", false))
}

// A command built in a variable and offered by a format call is read
// whole: v0.4.0's held update offered update --submit so, after --submit
// had gone (the rc1 full stage).
func TestBuiltStringsAreRead(t *testing.T) {
	t.Parallel()
	root := rootForReference(t)
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "held.go", `package command
func held(port, version string) error {
	next := "dockhand update " + port
	if version != "" {
		next += " " + version
	}
	return exitf(3, "%s waits for your look\nOnce it's fine: %s --new --submit", port, next)
}`, 0)
	require.NoError(t, err)
	var problems []string
	for _, text := range fileStrings(files, file) {
		problems = append(problems, referenceProblems(root, text, false)...)
	}
	require.Equal(t, []string{"dockhand update has no --submit"}, problems)
}
