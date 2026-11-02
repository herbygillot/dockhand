package portindex

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"
)

// failurePrefix starts portindex's line for a Portfile it couldn't parse,
// in portindex.tcl's words: "Failed to parse file
// security/KeePassXC/Portfile: invalid command name "versio"", or, for a
// subport, "… Portfile with subport 'name': …". It writes the line, leaves
// the port out, and carries on; only -e makes the run's exit say so, 2,
// with the index written all the same.
const failurePrefix = "Failed to parse file "

// subportFailure follows "<directory>/Portfile" in a subport's line.
const subportFailure = " with subport "

// parseFailures are portindex's failure lines in its output, by the port
// directory each names, the main port's line before a subport's where a
// directory has both. Lines that name no port directory are passed over.
func parseFailures(output []byte) map[string]string {
	failed := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		directory, rest, ok := failureOf(line)
		if !ok {
			continue
		}
		if previous, seen := failed[directory]; seen {
			_, earlier, _ := failureOf(previous)
			if !strings.HasPrefix(earlier, subportFailure) || strings.HasPrefix(rest, subportFailure) {
				continue
			}
		}
		failed[directory] = line
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

// failureOf splits a failure line into the port directory it names and
// what follows "<directory>/Portfile".
func failureOf(line string) (directory, rest string, ok bool) {
	named, found := strings.CutPrefix(line, failurePrefix)
	if !found {
		return "", "", false
	}
	directory, rest, found = strings.Cut(named, "/Portfile")
	if !found || directory == "" || strings.Count(directory, "/") != 1 || !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, subportFailure) {
		return "", "", false
	}
	return directory, rest, true
}

// Failed is portindex's reason for leaving out the Portfile of a port
// directory when it built this index, such as invalid command name
// "versio", read from its line (parseFailures). False where it recorded
// none: a directory it parsed, an index opened outside the cache, or a
// generation cached before failures were kept.
func (i *Index) Failed(portdir string) (string, bool) {
	meta, found := readGeneration(filepath.Dir(i.path))
	if !found {
		return "", false
	}
	line, ok := meta.Failed[portdir]
	if !ok {
		return "", false
	}
	_, rest, _ := failureOf(line)
	if strings.HasPrefix(rest, subportFailure) {
		if _, after, cut := strings.Cut(rest, "': "); cut {
			return after, true
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, ":")), true
}
