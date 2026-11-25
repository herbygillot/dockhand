package buildlog

import (
	"bufio"
	"errors"
)

// readLine returns one physical line, draining any part beyond maxLine.
// long says the caller should not treat the discarded line as complete.
func readLine(lines *bufio.Reader) (line []byte, long bool, err error) {
	line, err = lines.ReadSlice('\n')
	for errors.Is(err, bufio.ErrBufferFull) {
		long = true
		_, err = lines.ReadSlice('\n')
	}
	return line, long, err
}
