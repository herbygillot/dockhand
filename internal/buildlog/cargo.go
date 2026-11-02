package buildlog

import (
	"bufio"
	"errors"
	"io"
	"regexp"
	"slices"
)

// cargoCompiling is the line cargo prints as it compiles a crate,
// "   Compiling libgit2-sys v0.17.0+1.8.1", the package's own with its
// path after, MacPorts' ":info:build" before it where its own log is
// read.
var cargoCompiling = regexp.MustCompile(`^(?::\w+:\w+\s)?\s*Compiling ([A-Za-z0-9_-]+) v[0-9]`)

// Compiled are the crates a log's lines, from one of them to the line
// before to (or to its end where to is 0), say cargo compiled, each once,
// in the order it compiled them: what a cargo port's build compiled of
// everything its Cargo.lock pins, the cargo PortGroup building with -v
// (the omnidiff session's note 6). None where no line says one.
func Compiled(log io.Reader, from, to int) []string {
	lines := bufio.NewReaderSize(log, maxLine)
	var crates []string
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
		if number >= from && !long {
			if match := cargoCompiling.FindSubmatch(line); match != nil && !slices.Contains(crates, string(match[1])) {
				crates = append(crates, string(match[1]))
			}
		}
		if err != nil {
			break
		}
	}
	return crates
}
