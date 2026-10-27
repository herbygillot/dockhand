package portfile

import (
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// What the review survey's other lint-shaped findings read of a Portfile's
// text (§8, §10, and §11): the notices commitrules makes of them, which
// hold nothing.

// A TouchedLine is a line a change adds or alters, counted from 1, with
// its text.
type TouchedLine struct {
	Line int
	Text string
}

// commentedCommand is a comment whose first word could start a Portfile
// command, and the words after it.
var commentedCommand = regexp.MustCompile(`^\s*#+\s*([a-z][a-z0-9_]*(?:[.-][a-z0-9_]+)*)\s+\S`)

// portfileCommands are the options and commands a Portfile writes that a
// commented-out line most often is, by name or by namespace, each
// -append, -delete, or -replace taken for its option; proseWords are
// those of them that are English words too, which a comment of prose
// starts with, "# version 5.5.1 of qtwebengine must…" or "# copy the
// license": one of them is code only with a sign of Tcl after it, a
// variable, a bracket or brace, a quote, a backslash, a port:
// dependency, or a flag (the measure over the Mac's tree).
var (
	portfileCommands = []string{
		"name", "version", "revision", "epoch", "categories", "maintainers", "license", "platforms", "supported_archs", "homepage",
		"description", "long_description", "master_sites", "distname", "distfiles", "checksums", "patchfiles", "worksrcdir",
		"use_configure", "use_autoreconf", "use_xz", "use_bzip2", "use_zip", "use_parallel_build", "universal_variant",
		"variant", "default_variants", "platform", "subport", "conflicts", "replaced_by", "notes", "installs_libs",
		"reinplace", "xinstall", "system", "delete", "copy", "move", "file", "ln",
	}
	portfileNamespaces = []string{"configure.", "build.", "destroot.", "test.", "extract.", "patch.", "fetch.", "livecheck.", "compiler.",
		"depends_", "pre-", "post-", "github.", "gitlab.", "python.", "cmake.", "cargo.", "go.", "perl5.", "ruby.", "qt5.", "qt6.", "meson.", "xcode."}
	proseWords = []string{"name", "version", "revision", "epoch", "categories", "maintainers", "license", "platforms", "homepage",
		"description", "variant", "platform", "subport", "conflicts", "notes", "system", "delete", "copy", "move", "file", "ln"}
	// proseFollowers are the words after one of them that make it
	// prose, whatever follows: "# conflicts with the file \"expat\"…".
	proseFollowers = []string{"a", "an", "the", "to", "of", "with", "from", "for", "and", "in", "on", "that", "this", "it", "is", "are", "was", "gets", "has", "needs", "must", "should", "will", "can"}
	// buildFiles are names in an option's namespace that are a build's
	// files, as "# meson.build uses the GTK+ 3 backend" names one.
	buildFiles = []string{"meson.build", "configure.ac", "configure.in", "build.gradle", "build.sh", "build.xml"}
	tclSign    = regexp.MustCompile(`[$\[{"\\]|\b(?:port|path|bin|lib):|\s--?[A-Za-z]`)
)

// CommentedCode are the lines a change adds that are commented-out Tcl: a
// `#` whose rest starts with an option or a command a Portfile writes, and
// goes on, as "# depends_lib-append port:zlib" does (the review survey's
// §11). A comment of prose, or the modeline, isn't one.
func CommentedCode(before, after []byte) []TouchedLine {
	lines := splitLines(after)
	from := matchLines(splitLines(before), lines)
	var found []TouchedLine
	for i, line := range lines {
		if from[i] >= 0 || i == 0 && isModeline(line) {
			continue
		}
		m := commentedCommand.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, rest := m[1], line[strings.Index(line, m[1])+len(m[1]):]
		if slices.Contains(buildFiles, name) {
			continue
		}
		for _, suffix := range []string{"-append", "-delete", "-replace", "-prepend", "-strsed"} {
			name = strings.TrimSuffix(name, suffix)
		}
		if words := strings.Fields(rest); slices.Contains(proseWords, m[1]) && (!tclSign.MatchString(rest) || slices.Contains(proseFollowers, words[0])) {
			continue
		}
		if slices.Contains(portfileCommands, name) || slices.ContainsFunc(portfileNamespaces, func(prefix string) bool { return strings.HasPrefix(name, prefix) && len(name) > len(prefix) }) {
			found = append(found, TouchedLine{Line: i + 1, Text: strings.TrimSpace(line)})
		}
	}
	return found
}

// NoarchWithoutAny reports, where a Portfile's top sets supported_archs
// noarch and its platforms are darwin, as written or by default, the line
// supported_archs is on:
// a port that builds nothing for an architecture runs on any platform,
// and MacPorts' buildbots build it once where platforms says so (the
// review survey's §10). touched is whether the change adds the port or
// alters either line; one it doesn't touch is the maintainer's as it was.
func NoarchWithoutAny(before, after []byte) (line int, touched, ok bool) {
	archs, archsLine, found := topCommand(after, "supported_archs")
	if !found || !slices.Contains(archs.words, "noarch") {
		return 0, false, false
	}
	// A platforms line that names a release, "darwin >= 20" as go's
	// does, is the maintainer's bound, which any would lift.
	platforms, _, declared := topCommand(after, "platforms")
	if declared && !slices.Equal(platforms.words, []string{"darwin"}) {
		return 0, false, false
	}
	if len(before) == 0 {
		return archsLine, true, true
	}
	oldArchs, _, _ := topCommand(before, "supported_archs")
	oldPlatforms, _, _ := topCommand(before, "platforms")
	return archsLine, oldArchs.text != archs.text || oldPlatforms.text != platforms.text, true
}

// command is a command's words, as written, and its text, its whitespace
// made single spaces.
type command struct {
	words []string
	text  string
}

// topCommand is the last command at a Portfile's top that name starts,
// and its line.
func topCommand(src []byte, name string) (command, int, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return command{}, 0, false
	}
	var found command
	line, ok := 0, false
	for _, item := range script.Items {
		cmd, isCommand := item.(syntax.Command)
		if !isCommand {
			continue
		}
		if first, _ := cmd.Name(src); first != name {
			continue
		}
		found = command{text: strings.Join(strings.Fields(string(src[cmd.Span.Start:cmd.Span.End])), " ")}
		for _, word := range cmd.Words[1:] {
			found.words = append(found.words, strings.Trim(string(src[word.Span.Start:word.Span.End]), "{}\""))
		}
		line, ok = 1+strings.Count(string(src[:cmd.Span.Start]), "\n"), true
	}
	return found, line, ok
}

// NamesFile reports whether a Portfile names a file of its files/, by its
// path there or its name, or by a word that builds it from variables, as
// "${filespath}/py${python.version}-sympy" names py315-sympy, or by the
// directory under files/ it's in, which a copy takes whole; a word under
// ${filespath} built of variables alone may name any. The rest are
// left over: a patch an earlier version needed, or a stray copy (the
// review survey's §11).
func NamesFile(src []byte, file string) bool {
	base := path.Base(file)
	if Mentions(src, file) || Mentions(src, base) {
		return true
	}
	if top, _, nested := strings.Cut(file, "/"); nested && Mentions(src, top) {
		return true
	}
	for _, word := range variableWord.FindAllString(string(src), -1) {
		word = strings.Trim(word, `"[]\`)
		for strings.HasSuffix(word, "}") && strings.Count(word, "}") > strings.Count(word, "{") {
			word = strings.TrimSuffix(word, "}")
		}
		for strings.HasPrefix(word, "{") && strings.Count(word, "{") > strings.Count(word, "}") {
			word = strings.TrimPrefix(word, "{")
		}
		last := word[strings.LastIndexByte(word, '/')+1:]
		literal := variable.ReplaceAllString(last, "")
		if len(literal) < 2 {
			// A file of files/ named by variables alone, as the select
			// PortGroup's ${filespath}/${subport}, may be any of them.
			if strings.Contains(word, "filespath") && !strings.Contains(file, "/") {
				return true
			}
			continue
		}
		var pattern strings.Builder
		at := 0
		for _, span := range variable.FindAllStringIndex(last, -1) {
			pattern.WriteString(regexp.QuoteMeta(last[at:span[0]]) + ".+")
			at = span[1]
		}
		pattern.WriteString(regexp.QuoteMeta(last[at:]))
		if matched, _ := regexp.MatchString("^"+pattern.String()+"$", base); matched {
			return true
		}
	}
	return false
}

var (
	variableWord = regexp.MustCompile(`\S*\$\S*`)
	variable     = regexp.MustCompile(`\$\{[^}]*\}|\$[A-Za-z_][A-Za-z0-9_:.]*|\[[^\]]*\]`)
)
