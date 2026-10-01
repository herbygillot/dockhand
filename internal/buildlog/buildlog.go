// Package buildlog reads a build's log for what most likely made it fail:
// the line a person would look for first. It's a reading of the log, best
// effort, and whoever shows it says so, beside MacPorts' own words (D10).
// More readings can join compilerError as they're wanted. It also finds a
// log's lines from where a step of the build began, as its provider
// recorded it (model.LogStep).
package buildlog

import (
	"bufio"
	"bytes"
	"errors"
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
func First(log io.Reader) (Cause, bool) { return FirstFrom(log, 1) }

// FirstFrom is First from a line of the log on, counting from 1, as from
// where the step that failed began: a dependency's build before it may
// have printed an error of its own and gone on (batch 14).
func FirstFrom(log io.Reader, from int) (Cause, bool) {
	lines := bufio.NewReaderSize(log, maxLine)
	for number := 1; ; number++ {
		line, err := lines.ReadSlice('\n')
		long := false
		for errors.Is(err, bufio.ErrBufferFull) {
			long = true
			_, err = lines.ReadSlice('\n')
		}
		if err != nil && !errors.Is(err, io.EOF) || len(line) == 0 && !long {
			return Cause{}, false
		}
		if !long && number >= from {
			text := strings.TrimRight(string(line), "\r\n")
			if compilerError.MatchString(text) {
				return Cause{Line: strings.TrimSpace(text), Number: number}, true
			}
		}
		if err != nil {
			return Cause{}, false
		}
	}
}

// maxLine is the longest line of a log read for a cause, which a
// compiler's error line is well within. A longer one, as a build may
// print, is passed over, still counted, and the log read on: the scanner
// before stopped at it without saying, and every cause after it went
// unseen (the limits sweep, 2026-10-01).
const maxLine = 1 << 20

// From is a log from the start of one of its lines, counting from 1, as
// a provider records where each step of a build begins: nothing after a
// log's last line, and false where the log ends before it.
func From(log []byte, line int) ([]byte, bool) {
	if line < 1 {
		return nil, false
	}
	start := 0
	for range line - 1 {
		end := bytes.IndexByte(log[start:], '\n')
		if end < 0 {
			return nil, false
		}
		start += end + 1
	}
	return log[start:], true
}
