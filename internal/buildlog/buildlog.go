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
	"slices"
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

// rustError is rustc's error, "error: message" or "error[E0425]:
// message", which its next line places, "  --> src/lib.rs:542:1", as the
// Rust reference's diagnostics show them; Cargo's own "error: could not
// compile" places nothing, and isn't taken.
var (
	rustError    = regexp.MustCompile(`^error(\[E\d{4}\])?: \S`)
	rustLocation = regexp.MustCompile(`^\s*--> (\S+:\d+:\d+)$`)
)

// FirstFrom reads a log, from a line on, counting from 1, for the first
// line a reading takes as a failure's likely cause: in a C or C++ build,
// the first error is the one the rest follow from, and in a Rust one the
// first error rustc placed in a source file, with where (field testing's
// perry: llvm-sys's "No suitable version of LLVM was found", at its
// lib.rs:542, where the summary named Cargo's command alone). None where
// the log has none, or can't be read. The line is where the step that failed began: a dependency's build before it may
// have printed an error of its own and gone on (batch 14).
func FirstFrom(log io.Reader, from int) (Cause, bool) {
	lines := bufio.NewReaderSize(log, maxLine)
	var pending Cause
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
			if pending.Line != "" {
				if m := rustLocation.FindStringSubmatch(text); m != nil {
					pending.Line += " (at " + m[1] + ")"
					return pending, true
				}
				pending = Cause{}
			}
			if rustError.MatchString(text) {
				pending = Cause{Line: strings.TrimSpace(text), Number: number}
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

// Between is a log's lines from one of them to the line before to,
// counting from 1, or to the log's end where to is 0, as one step of a
// build is between where it began and where the next began; false where
// the log ends before from.
func Between(log []byte, from, to int) ([]byte, bool) {
	rest, ok := From(log, from)
	if !ok || to == 0 || to <= from {
		return rest, ok
	}
	if after, cut := From(rest, to-from+1); cut {
		return rest[:len(rest)-len(after)], true
	}
	return rest, true
}

// Last is a step's last lines in a log, at most n, from the line it began
// on to the line before to, or to the log's end where to is 0: what a
// failure that no reading explains printed last, as a person scrolling
// up from the end would look at it. MacPorts' own DEBUG lines are left
// out, since port -d writes them between every line that matters, and so
// are blank lines; whatever the build wrote to its error output is in the
// log beside the rest (the Vx port's field testing, 2026-10-03).
func Last(log io.Reader, from, to, n int) []string {
	if n <= 0 {
		return nil
	}
	lines := bufio.NewReaderSize(log, maxLine)
	var last []string
	for number := 1; to == 0 || number < to; number++ {
		line, err := lines.ReadSlice('\n')
		long := false
		for errors.Is(err, bufio.ErrBufferFull) {
			long = true
			_, err = lines.ReadSlice('\n')
		}
		if err != nil && !errors.Is(err, io.EOF) || len(line) == 0 && !long {
			break
		}
		text := strings.TrimRight(string(line), "\r\n")
		if long {
			text = "(a line too long to show)"
		}
		if number >= from && strings.TrimSpace(text) != "" && !strings.HasPrefix(text, "DEBUG: ") {
			if len(last) == n {
				last = last[1:]
			}
			last = append(last, text)
		}
		if err != nil {
			break
		}
	}
	return last
}

// ctestFailed opens CTest's list of the tests that failed, and
// ctestFailure is one of them, "  3 - bugfixes-test (Failed)", as ctest
// prints it at the end of a run; automakeFailure is a failed test in
// automake's parallel harness's output and its test-suite.log, "FAIL:
// tests/basic.sh" (or ERROR:), as its manual describes the summary.
var (
	ctestFailed     = regexp.MustCompile(`^The following tests FAILED:\s*$`)
	ctestFailure    = regexp.MustCompile(`^\s*\d+\s+-\s+(\S.*?)\s+\([^()]+\)\s*$`)
	automakeFailure = regexp.MustCompile(`^(?:FAIL|ERROR): (\S+)`)
)

// FailedTests are the tests a log, from a line on, says failed, each once,
// in the order it names them, as ctest and automake's harness list them:
// a test phase that failed said only "make: *** [test] Error 8" and where
// CTest's log was, so exiv2's Portfile was changed to run ctest
// --output-on-failure just to see which (the exiv2 field notes,
// 2026-10-19). None where the log names none, or can't be read.
func FailedTests(log io.Reader, from int) []string {
	lines := bufio.NewReaderSize(log, maxLine)
	var failed []string
	add := func(name string) {
		if !slices.Contains(failed, name) {
			failed = append(failed, name)
		}
	}
	listing := false
	for number := 1; ; number++ {
		line, err := lines.ReadSlice('\n')
		long := false
		for errors.Is(err, bufio.ErrBufferFull) {
			long = true
			_, err = lines.ReadSlice('\n')
		}
		if err != nil && !errors.Is(err, io.EOF) || len(line) == 0 && !long {
			break
		}
		if number < from || long {
			continue
		}
		text := strings.TrimRight(string(line), "\r\n")
		switch {
		case ctestFailed.MatchString(text):
			listing = true
		case listing:
			if match := ctestFailure.FindStringSubmatch(text); match != nil {
				add(match[1])
			} else {
				listing = false
			}
		}
		if match := automakeFailure.FindStringSubmatch(text); match != nil {
			add(match[1])
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return failed
}
