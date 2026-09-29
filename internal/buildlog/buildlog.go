// Package buildlog reads a build's log for what most likely made it fail:
// the line a person would look for first. It's a reading of the log, best
// effort, and whoever shows it says so, beside MacPorts' own words (D10).
// More readings can join compilerError as they're wanted.
package buildlog

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// Cause is the line of a log that most likely says why a build failed.
type Cause struct {
	// Line is the log's line, as the tool that wrote it wrote it.
	Line string
	// Number is where it is in the log, counting from 1.
	Number int
}

// compilerError is a compiler's error at a place in a file, in the format
// clang documents for its diagnostics, which GCC and swiftc share:
// "file:line:column: error: message", or "fatal error:", the column
// optional. A file's name isn't digits alone, as a time's is. Warnings and
// notes aren't errors, and neither is a tool's word "error" elsewhere, as
// make's or MacPorts' own.
var compilerError = regexp.MustCompile(`^[^\s:]*[^\s:\d][^:]*:\d+:(\d+:)? (fatal )?error: \S`)

// First reads a log for the first line a reading takes as a failure's
// likely cause: in a C or C++ build, the first error is the one the rest
// follow from. None where the log has none, or can't be read.
func First(log io.Reader) (Cause, bool) {
	lines := bufio.NewScanner(log)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for number := 1; lines.Scan(); number++ {
		line := strings.TrimRight(lines.Text(), "\r")
		if compilerError.MatchString(line) {
			return Cause{Line: strings.TrimSpace(line), Number: number}, true
		}
	}
	return Cause{}, false
}
