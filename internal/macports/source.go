package macports

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Facts about MacPorts that every layer shares.
const (
	// DefaultBaseVersion is the MacPorts Base release Dockhand installs and expects.
	DefaultBaseVersion = "2.12.6"
	// DefaultPrefix is where MacPorts installs on hosts and in guests.
	DefaultPrefix = "/opt/local"
	// TclShell is the MacPorts Tcl interpreter under the prefix.
	TclShell = "port-tclsh"
)

// FindTclsh is this Mac's MacPorts Tcl interpreter: port-tclsh where look
// finds it on a PATH,
// else under the default prefix; empty where there's neither. The
// evaluator, setup's report, and the comparison of an image's MacPorts
// with this Mac's all read Portfiles with it.
func FindTclsh(look func(name string) (string, error)) string {
	for _, name := range []string{TclShell, filepath.Join(DefaultPrefix, "bin", TclShell)} {
		if path, err := look(name); err == nil {
			return path
		}
	}
	return ""
}

// BaseAtLeast reports a MacPorts Base version that is want or newer,
// compared release number by release number. MacPorts' installer runs
// selfupdate, which takes a guest past the version it installed where a
// newer one is out: 2.12.7 came out on 2026-10-13, and setup, which asked
// for 2.12.6, rejected the image it had just made (the v0.3.1 quick stage,
// 2026-10-15).
func BaseAtLeast(have, want string) bool {
	a, b := strings.Split(have, "."), strings.Split(want, ".")
	for i := range max(len(a), len(b)) {
		x, y := 0, 0
		if i < len(a) {
			x, _ = strconv.Atoi(a[i])
		}
		if i < len(b) {
			y, _ = strconv.Atoi(b[i])
		}
		if x != y {
			return x > y
		}
	}
	return true
}

// DarwinFloor is the oldest Darwin a port's platforms admit, as "darwin >=
// 20" or "darwin > 19" says it; zero where they name none.
func DarwinFloor(platforms string) int {
	words := strings.Fields(strings.NewReplacer("{", " ", "}", " ").Replace(platforms))
	floor := 0
	for i := 0; i+2 < len(words); i++ {
		if words[i] != "darwin" {
			continue
		}
		n, err := strconv.Atoi(words[i+2])
		if err != nil {
			continue
		}
		switch words[i+1] {
		case ">=":
			floor = max(floor, n)
		case ">":
			floor = max(floor, n+1)
		}
	}
	return floor
}
