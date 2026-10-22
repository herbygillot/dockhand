package cmdtest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/term"
)

// saidOnceWidth is the width a default output's lines fit, the width off
// a terminal without COLUMNS (term.Streams.Width).
const saidOnceWidth = 100

// The names "said once" counts: a pull request's URL, a run, and a
// commit, which a command names at most once outside a table.
var (
	pullRequestURL = regexp.MustCompile(`https://github\.com/[^\s/]+/[^\s/]+/pull/\d+`)
	runName        = regexp.MustCompile(`\bcheck-\d+\b`)
	shortCommit    = regexp.MustCompile(`\b[0-9a-f]{7}\b`)
	tableLine      = regexp.MustCompile(`\S {2,}\S`)
)

// SaidOnce is what a command line's default output breaks of "said once"
// (part 5, item 3, from the output review's §2): a pull request's URL, a
// run, or a commit named more than once outside a table, and more than
// one Next:. A line in a table, its cells set apart by two spaces or more,
// names what it's about in a column.
func SaidOnce(out string) []string {
	var broken []string
	named := map[string]int{}
	nexts := 0
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "Next: ") {
			nexts++
		}
		if tableLine.MatchString(strings.TrimSpace(line)) {
			continue
		}
		seen := map[string]bool{}
		for _, pattern := range []*regexp.Regexp{pullRequestURL, runName, shortCommit} {
			for _, at := range pattern.FindAllStringIndex(line, -1) {
				// A name in a path, a log's ~/.dockhand/logs/check-1/…,
				// is where a file is, not the thing named again.
				if at[0] > 0 && line[at[0]-1] == '/' || at[1] < len(line) && line[at[1]] == '/' {
					continue
				}
				name := line[at[0]:at[1]]
				if !seen[name] {
					seen[name] = true
					named[name]++
				}
			}
		}
	}
	if nexts > 1 {
		broken = append(broken, fmt.Sprintf("%d Next: lines", nexts))
	}
	var names []string
	for name, count := range named {
		if count > 1 {
			names = append(names, fmt.Sprintf("%s named %d times", name, count))
		}
	}
	slices.Sort(names)
	return append(broken, names...)
}

// Overwidth are a command line's lines wider than the width, the rest of
// the rule, which item 4's prose writer and table cells bring the
// commands to: until then it's reported, where DOCKHAND_TEST_SAID_ONCE_REPORT
// asks, and not enforced.
func Overwidth(out string) []string {
	var wide []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if width := utf8.RuneCountInString(line); width > saidOnceWidth {
			wide = append(wide, fmt.Sprintf("%d columns, over %d: %q", width, saidOnceWidth, line))
		}
	}
	return wide
}

// saidOnceChecked reports whether a command line's output is the default
// level's, which "said once" holds of: not -v's, --json's, or a preview's,
// which is meant to be read in full, nor help's; nor a question's on a
// terminal, which says the report it asks about; nor serve's log, or
// status --watch's redrawn view, which name what each line is about; nor
// review --markdown's, a comment's text for pasting.
func saidOnceChecked(args []string, streams term.Streams) bool {
	if streams.Terminal() || len(args) > 0 && args[0] == "serve" {
		return false
	}
	for _, arg := range args {
		switch {
		case arg == "--watch", arg == "--markdown":
			return false
		case arg == "--json", arg == "--plan", arg == "--help", arg == "-h", arg == "help":
			return false
		case arg == "--verbose", strings.HasPrefix(arg, "--verbose="):
			return false
		case strings.HasPrefix(arg, "-v") && strings.Trim(arg[1:], "v") == "":
			return false
		}
	}
	return true
}

// saidOnceOut gathers a command line's standard output beside where the
// test reads it, for SaidOnce.
type saidOnceOut struct {
	mu  sync.Mutex
	buf bytes.Buffer
	to  io.Writer
}

func (w *saidOnceOut) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	w.mu.Unlock()
	return w.to.Write(p)
}

// checkSaidOnce reports what the output broke: in the test, or where
// DOCKHAND_TEST_SAID_ONCE_REPORT names a file, appended there with what's over
// the width.
func checkSaidOnce(t testing.TB, args []string, out *saidOnceOut) {
	t.Helper()
	out.mu.Lock()
	said := out.buf.String()
	out.mu.Unlock()
	broken := SaidOnce(said)
	if report := os.Getenv("DOCKHAND_TEST_SAID_ONCE_REPORT"); report != "" {
		if file, err := os.OpenFile(report, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			for _, b := range append(broken, Overwidth(said)...) {
				fmt.Fprintf(file, "%s\t%s\t%s\n", t.Name(), strings.Join(args, " "), b)
			}
			file.Close()
		}
		return
	}
	if len(broken) == 0 {
		return
	}
	t.Errorf("dockhand %s broke said once:\n  %s", strings.Join(args, " "), strings.Join(broken, "\n  "))
}
