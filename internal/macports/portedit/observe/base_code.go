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
)

// Whose code a recorded frame is, decided by the frame's file against the
// library the evaluator loaded Base from (PortObservation.BaseLibrary),
// never by a procedure's name or a substring of a path (#110's finding 1,
// architecture's point 4). A frame naming a file under that library is
// Base's code; one with no file, as compiled code and evaluated strings
// leave, says nothing; one naming any other file is a Portfile's,
// PortGroup's, or someone else's code, and makes the read theirs.

// baseCode answers whose code frames are for one observation.
type baseCode struct {
	library  string
	resolved string
	memo     map[string]bool
}

func newBaseCode(library string) *baseCode {
	b := &baseCode{memo: map[string]bool{}}
	if library == "" {
		return b
	}
	b.library = filepath.Clean(library)
	b.resolved = b.library
	if resolved, err := filepath.EvalSymlinks(b.library); err == nil {
		b.resolved = resolved
	}
	return b
}

// has reports whether file is under Base's library, by path prefix, its
// links resolved where the plain path isn't.
func (b *baseCode) has(file string) bool {
	if b.library == "" || file == "" {
		return false
	}
	if known, ok := b.memo[file]; ok {
		return known
	}
	under := func(path, dir string) bool { return strings.HasPrefix(path, dir+string(filepath.Separator)) }
	found := under(filepath.Clean(file), b.library)
	if !found {
		if resolved, err := filepath.EvalSymlinks(file); err == nil {
			found = under(resolved, b.resolved)
		}
	}
	b.memo[file] = found
	return found
}

// passThrough reports a frame of Base's that runs a script its caller
// gave: catch's and macports_try's uplevel, default_check's subst. It
// shows where the script ran, not who wrote it.
func passThrough(frame macports.SourceFrame) bool {
	return strings.HasPrefix(frame.Command, "uplevel ")
}

// nobodyElse reports whether no frame in frames names a file outside
// Base's library.
func (b *baseCode) nobodyElse(frames []macports.SourceFrame) bool {
	for _, frame := range frames {
		if frame.File != "" && !b.has(frame.File) {
			return false
		}
	}
	return true
}

// bodyIsBases reports whether the code frames run is Base's own: no
// frame names a file outside Base's library, every default evaluated
// among them is one Base wrote (substitutedBases), and the first frame
// naming a file is Base's own code or default_check's substitution of
// such a default, never a pass-through that runs a script its caller
// gave. A procedure a Portfile defined has its file for its body's
// frames; one whose body was built as a string has none until Base's
// catch, which is a pass-through.
func (b *baseCode) bodyIsBases(frames []macports.SourceFrame) bool {
	if !b.nobodyElse(frames) {
		return false
	}
	first := -1
	for i, frame := range frames {
		if substitution(frame) {
			if !b.substitutedBases(frames, i) {
				return false
			}
		}
		if frame.File != "" && first < 0 {
			first = i
		}
	}
	return first >= 0 && (!passThrough(frames[first]) || substitution(frames[first]))
}

// substitution reports default_check's frame substituting an option's
// default (portutil.tcl's "uplevel #0 [list subst ...]").
func substitution(frame macports.SourceFrame) bool {
	return frame.File != "" && strings.HasPrefix(frame.Command, "uplevel #0 [list subst ")
}

// substitutedBases reports whether the default substituted at frames[at]
// is one Base's source writes, by the default_check before it and the
// subst after it.
func (b *baseCode) substitutedBases(frames []macports.SourceFrame, at int) bool {
	option := ""
	for i := at - 1; i >= 0 && option == ""; i-- {
		if match := defaultCheck.FindStringSubmatch(frames[i].Command); match != nil {
			option = match[1]
		}
	}
	for _, frame := range frames[at+1:] {
		if strings.HasPrefix(frame.Command, "subst ") {
			words, errs := syntax.ListValues(frame.Command)
			return option != "" && len(errs) == 0 && len(words) == 2 && slices.Contains(baseDefaults(b.library)[option], words[1])
		}
	}
	return false
}

// innermostDefault is the index of the innermost default_check frame
// before end, the option whose default is evaluated there, and the text
// default_check substitutes, from the subst frame after it.
func innermostDefault(frames []macports.SourceFrame, end int) (int, string, string, bool) {
	for i := min(end, len(frames)) - 1; i >= 0; i-- {
		match := defaultCheck.FindStringSubmatch(frames[i].Command)
		if match == nil {
			continue
		}
		for _, frame := range frames[i+1 : min(end, len(frames))] {
			if !strings.HasPrefix(frame.Command, "subst ") {
				continue
			}
			words, errs := syntax.ListValues(frame.Command)
			if len(errs) > 0 || len(words) != 2 {
				return i, match[1], "", false
			}
			return i, match[1], words[1], true
		}
		return i, match[1], "", false
	}
	return -1, "", "", false
}

// defaultIsBases reports whether the innermost default evaluated before
// end is one Base wrote, as its own source writes it, and runs only
// Base's code below it: Base's build.cmd default finding make, not a
// default a Portfile or PortGroup wrote that looks for a program itself,
// which has no file frame and Base's default_check and catch on its stack
// (architecture's point 2). A default a Portfile or PortGroup set to
// Base's own text is Base's code.
func (b *baseCode) defaultIsBases(frames []macports.SourceFrame, end int) bool {
	i, option, text, ok := innermostDefault(frames, end)
	if !ok || !slices.Contains(baseDefaults(b.library)[option], text) {
		return false
	}
	below := frames[i+1 : min(end, len(frames))]
	if !b.nobodyElse(below) {
		return false
	}
	// Below the substitution, the first frame with a file is Base's own
	// code, or there is none: use_xcode's default asks file exists itself.
	for j, frame := range below {
		if strings.HasPrefix(frame.Command, "subst ") {
			for _, frame := range below[j+1:] {
				if frame.File != "" {
					return !passThrough(frame)
				}
			}
			return true
		}
	}
	return false
}

var (
	baseDefaultsMu    sync.Mutex
	baseDefaultsCache = map[string]map[string][]string{}
)

// continuedLine is a backslash-newline, which Tcl reads as one space even
// inside braces.
var continuedLine = regexp.MustCompile(`\\\n[ \t]*`)

// baseDefaults are the defaults Base's port1.0 writes, by option, as the
// texts default_check substitutes, read from the library's own source.
func baseDefaults(library string) map[string][]string {
	if library == "" {
		return nil
	}
	baseDefaultsMu.Lock()
	defer baseDefaultsMu.Unlock()
	if found, ok := baseDefaultsCache[library]; ok {
		return found
	}
	found := map[string][]string{}
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
			if name, _ := cmd.Name(src); name != "default" || len(cmd.Words) != 3 {
				continue
			}
			option, ok := cmd.Words[1].Literal(src)
			if !ok {
				continue
			}
			text, ok := cmd.Words[2].Literal(src)
			if !ok && len(cmd.Words[2].Segments) == 1 {
				if braced, isBraced := cmd.Words[2].Segments[0].(syntax.Braced); isBraced {
					text, ok = continuedLine.ReplaceAllString(braced.Body.Text(src), " "), true
				}
			}
			if ok {
				found[option] = append(found[option], text)
			}
		}
	}
	baseDefaultsCache[library] = found
	return found
}
