package command

import (
	"go/ast"
	"go/parser"
	"go/token"
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

// rootForReference is the command tree Run builds.
func rootForReference(t *testing.T) *cobra.Command {
	var root *cobra.Command
	inspectRoot = func(cmd *cobra.Command) { root = cmd }
	t.Cleanup(func() { inspectRoot = nil })
	_, _, err := dockhand(t, "--help")
	require.NoError(t, err)
	require.NotNil(t, root)
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
var pastReferences = map[string]string{
	"../../README.md: dockhand auth names no command": "v0.3.1's known issues name its auth status",
}

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

// sourceLiterals are the string literals of dockhand's own source, apart
// from tests, keyed by where each is: the Next: lines and refusals that
// name a command.
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
		ast.Inspect(file, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				if text, err := strconv.Unquote(literal.Value); err == nil && strings.Contains(text, "dockhand ") {
					literals[files.Position(literal.Pos()).String()] = text
				}
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	return literals
}

// referenceProblems names each "dockhand <command> [--flag]" in text whose
// command or flag doesn't exist. In code every one is a command; in prose,
// a help or a message, one is where a command is offered, after "run",
// "then", a colon, and the like, and elsewhere a word after dockhand
// that names no command is a sentence about it ("what dockhand runs
// with"), whose flags are still checked where it does name one.
func referenceProblems(root *cobra.Command, text string, code bool) []string {
	var problems []string
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
// parenthesis in prose, as "(dockhand built jq with +doc)", never a
// command.
var sentenceWords = map[string]bool{
	"builds": true, "built": true, "could": true, "evaluates": true,
	"has": true, "stopped": true, "was": true,
}

// What the reference test refuses, and what it reads as a sentence.
func TestReferenceProblems(t *testing.T) {
	root := rootForReference(t)
	require.Equal(t, []string{"dockhand providers names no command", "dockhand check has no --frobnicate", "dockhand setup tart has no -q"},
		referenceProblems(root, "dockhand providers setup tart\ndockhand check -b jq --frobnicate\ndockhand setup tart -q", true))
	require.Equal(t, []string{"dockhand auth names no command", "dockhand status has no --everything"},
		referenceProblems(root, "run dockhand auth login, then dockhand status --everything", false))
	require.Empty(t, referenceProblems(root, "what dockhand runs with; dockhand can't. --on says where (dockhand built jq with +doc); Next: dockhand check -b jq --on tart:26", false))
}
