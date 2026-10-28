package buildlog

import "regexp"

// The lines that say make tried to regenerate a distfile's autotools
// output: make's error on one of the files automake's rules regenerate,
// "make: *** [aclocal.m4] Error 127", or GNU make's form that names the
// Makefile's line, "make: *** [Makefile:412: aclocal.m4] Error 127"; and
// one of the tools themselves speaking, as sh's "aclocal-1.16: command
// not found" or automake's missing script's "WARNING: 'aclocal-1.16' is
// missing on your system".
var (
	regenerated  = regexp.MustCompile(`make(\[\d+\])?: \*\*\* \[(?:[^\]]*: )?(?:\S*/)?(aclocal\.m4|configure|Makefile\.in|config\.h\.in)\] Error \d+`)
	autotoolSaid = regexp.MustCompile(`(?:^|[\s/'])(aclocal|automake|autoconf|autoheader)(?:-\d+(?:\.\d+)*)?(?:: |' is missing)`)
)

// AutotoolsNote is a note on a failure whose lines, as a check's report
// shows them, say make reran autotools: the distfile's generated files
// were older than what they're made from, or made from a file it doesn't
// ship. abook 0.6.2's tarball's aclocal.m4 includes m4/extensions.m4,
// which it leaves out, and its failure said only "make: *** [aclocal.m4]
// Error 127" (the abook session, 2026-10-28). It's read from the lines
// alone, nothing of MacPorts', and is a note, never a hold; empty where
// the lines say no such thing.
func AutotoolsNote(lines []string) string {
	for _, line := range lines {
		if regenerated.MatchString(line) || autotoolSaid.MatchString(line) {
			return "make tried to regenerate the distfile's autotools output, which is often upstream's packaging slip, a file not shipped or timestamps lost; use_autoreconf yes regenerates it, where every source it reads is shipped"
		}
	}
	return ""
}
