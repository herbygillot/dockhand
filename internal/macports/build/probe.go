package build

import (
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/verify"
)

// probePrefix frames a probe sweep, for the reason the manifest's own
// prefix exists: the output crosses a transport that merges streams, and
// a frame keyed on anything a program could plausibly print would let a
// version banner be read as the next probe's argv.
const probePrefix = "===> dockhand probe: "

const (
	probeBinary = "binary"
	probeArgv   = "argv"
	probeOutput = "output"
	probeDone   = "done"
)

// ProbeLimit is how many of a port's binaries are asked. It is a small
// number on purpose: a probe is the cheapest possible evidence that a
// build which succeeded also produced something that runs, and a port
// that ships two hundred executables does not become two hundred times
// better evidenced by running all of them. Five is enough to catch the
// install that laid down a binary the loader refuses.
const ProbeLimit = 5

// ProbeTimeout is how long one binary is given, in seconds. A program
// asked for its version and answering with an interactive prompt would
// otherwise hold the environment until the whole verification is torn
// down, and it is the environment's own kill rather than a cancelled
// exec because a cancelled exec loses every probe that already
// succeeded.
const ProbeTimeout = 10

// ProbeScript runs an installed port's own binaries and reports what
// they said.
//
// The subject is read from the roster file by index, exactly as
// ManifestScript reads it: a port name is data here too.
//
// Only the executables the port itself laid down under the prefix's bin
// and sbin are asked. The filter is on where the file sits rather than
// on what it is, because `port contents` lists manual pages, headers and
// pkg-config files, and "is it executable" alone would find a shipped
// shell script in a share directory as readily as a program.
//
// --version and nothing else. A probe that guessed at a port's own verbs
// would be running a program's real work in an environment that exists
// to answer one question, and a binary that does not recognize the flag
// answers with its usage, which is still the evidence being asked for:
// that the thing loads and runs at all.
func ProbeScript(portCmd, dest, roster string, index int, pfx string) string {
	return `set -u
n=0
subject=
while IFS= read -r line; do
  if [ "$n" -eq ` + strconv.Itoa(index) + ` ]; then subject=$line; break; fi
  n=$((n+1))
done < ` + roster + `
[ -n "$subject" ] || { echo "no subject at line ` + strconv.Itoa(index+1) + ` of ` + roster + `" >&2; exit 1; }
{
  seen=0
  ` + portCmd + ` -q contents "$subject" 2>/dev/null | sed 's/^  //' | while IFS= read -r f; do
    case "$f" in ` + pfx + `/bin/*|` + pfx + `/sbin/*) ;; *) continue;; esac
    [ -f "$f" ] && [ -x "$f" ] || continue
    seen=$((seen+1))
    [ "$seen" -le ` + strconv.Itoa(ProbeLimit) + ` ] || break
    printf '` + probePrefix + probeBinary + `\n%s\n' "$f"
    printf '` + probePrefix + probeArgv + `\n%s --version\n' "$f"
    printf '` + probePrefix + probeOutput + `\n'
    "$f" --version </dev/null 2>&1 &
    p=$!
    ( sleep ` + strconv.Itoa(ProbeTimeout) + `; kill -9 "$p" 2>/dev/null ) 2>/dev/null &
    k=$!
    wait "$p"
    kill "$k" 2>/dev/null
  done
  printf '` + probePrefix + probeDone + `\n'
} > ` + dest + `.part 2>/dev/null && mv -f ` + dest + `.part ` + dest + `
`
}

// ParseProbes reads a framed probe sweep.
//
// A sweep with no closing marker is a sweep that was cut off, and what
// it has is still returned: a probe is corroboration rather than a
// measurement, so half of it is half the corroboration and not a
// falsehood. That is the opposite of ParseManifest's rule, deliberately
// — a truncated manifest reads as libraries that vanished, where a
// truncated probe reads as fewer binaries asked.
func ParseProbes(out string) []verify.ProbeLine {
	var lines []verify.ProbeLine
	var cur *verify.ProbeLine
	section := ""
	flush := func() {
		if cur != nil {
			cur.Output = strings.TrimRight(cur.Output, "\n")
			lines = append(lines, *cur)
			cur = nil
		}
	}
	for line := range strings.Lines(out) {
		if rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), probePrefix); ok {
			section = strings.TrimSpace(rest)
			if section == probeBinary {
				flush()
				cur = &verify.ProbeLine{}
			}
			if section == probeDone {
				flush()
			}
			continue
		}
		if cur == nil {
			continue
		}
		switch section {
		case probeBinary:
			if cur.Binary == "" {
				cur.Binary = strings.TrimSpace(line)
			}
		case probeArgv:
			if cur.Argv == "" {
				cur.Argv = strings.TrimSpace(line)
			}
		case probeOutput:
			cur.Output += line
		}
	}
	flush()
	return lines
}
