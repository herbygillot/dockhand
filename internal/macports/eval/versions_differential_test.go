package eval

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// versions.tcl mirrors the matching loop of Base's portlivecheck.tcl rather
// than sourcing it, because livecheck_main is one procedure that fetches,
// prints, and matches. This test runs Base's own loop, sliced out of the
// installed portlivecheck.tcl, and the mirror over the same documents and
// expressions, and requires the same version and the same comparison, so
// the mirror cannot drift from Base silently. A match that ends where it
// began traps Base's loop; the mirror steps past it, and that case is the
// one difference allowed.
func TestVersionsMirrorBaseLivecheckMatching(t *testing.T) {
	t.Parallel()
	executable := testsupport.MacPortsTclsh(t)
	// Base 2.12 keeps livecheck's matching loop in portlivecheck.tcl;
	// master moved the code the target runs to portlivecheck_run.tcl.
	prefix := filepath.Dir(filepath.Dir(executable))
	var base string
	for _, name := range []string{"portlivecheck_run.tcl", "portlivecheck.tcl"} {
		candidate := filepath.Join(prefix, "libexec/macports/lib/port1.0", name)
		if _, err := os.Stat(candidate); err == nil {
			base = candidate
			break
		}
	}
	if base == "" {
		t.Skipf("Base's livecheck is not under %s", prefix)
	}
	corpus, err := filepath.Abs("testdata/livecheck")
	require.NoError(t, err)
	type example struct {
		// Regex is the evaluated livecheck.regex option, a Tcl list Base joins.
		Name, Document, Type, Regex, Current string
		// Base is what Base's loop is expected to say: the version, and
		// updated as Base sets it, 1 newer, 0 current, -1 none or older.
		Version string
		Updated int
		// Traps marks a document Base's loop never leaves; only the mirror runs.
		Traps bool
	}
	examples := []example{
		{Name: "flyctl latest release", Document: "flyctl-latest.json", Type: "regex", Regex: `{"tag_name": "v(\d+(?:\.\d+)+)"}`, Current: "0.4.96", Version: "0.4.105", Updated: 1},
		{Name: "flyctl already current", Document: "flyctl-latest.json", Type: "regex", Regex: `{"tag_name": "v(\d+(?:\.\d+)+)"}`, Current: "0.4.105", Version: "0.4.105", Updated: 0},
		{Name: "flyctl older", Document: "flyctl-latest.json", Type: "regex", Regex: `{"tag_name": "v(\d+(?:\.\d+)+)"}`, Current: "0.5.0", Version: "0.4.105", Updated: -1},
		{Name: "cmake 3.x no longer listed", Document: "cmake-tags.json", Type: "regex", Regex: `{"name": "v(3\.[0-9.]+)"}`, Current: "3.31.0", Version: "", Updated: -1},
		{Name: "cmake 4.x newest of the page", Document: "cmake-tags.json", Type: "regex", Regex: `{"name": "v(4\.[0-9.]+)"}`, Current: "4.0.0", Version: "4.4.3", Updated: 1},
		{Name: "MyLoss regexm", Document: "myloss-info.plist", Type: "regexm", Regex: `{<key>CFBundleShortVersionString</key>\s*<string>([^<]+)</string>}`, Current: "1.0", Version: "1.0", Updated: 0},
		{Name: "matches sharing a character", Document: "overlap.txt", Type: "regex", Regex: `{release-([0-9.]+)\.tar\.gz}`, Current: "1.0.0", Version: "1.10.0", Updated: 1},
		{Name: "two-digit overlap", Document: "overlap.txt", Type: "regex", Regex: `{(\d)\d}`, Current: "0", Version: "8", Updated: 1},
		{Name: "single character match traps Base", Document: "overlap.txt", Type: "regex", Regex: `{single (\d)}`, Current: "1", Version: "5", Updated: 1, Traps: true},
	}
	input, err := json.Marshal(examples)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "examples.json"), input, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "versions.tcl"), []byte(versionScript), 0600))
	script := `
package require Pextlib 1.0
package require json
namespace eval ::tclrpc {proc register {args} {}}
namespace eval ::dockhand {}
source [file join $env(TEST_ROOT) versions.tcl]
set fd [open $env(TEST_BASE) r]; set text [read $fd]; close $fd
set needle "if \{\$\{livecheck.type\} eq \"regexm\"\} \{"
set begin [string first $needle $text]
# 2.12 ends the loop by closing $chan; master reads $tempfd and leaves it
# open, so its loop ends where the no-match report begins.
set end [string first "close \$chan" $text $begin]
if {$end < 0} {set end [string first "if \{!\$foundmatch\}" $text $begin]}
if {$begin < 0 || $end < 0} {error "Base's livecheck matching loop was not found in $env(TEST_BASE)"}
set block [string range $text $begin [expr {$end - 1}]]
proc ui_debug {args} {}
proc ui_error {args} {}
set subport test
set fd [open [file join $env(TEST_ROOT) examples.json] r]; set examples [json::json2dict [read $fd]]; close $fd
set report {}
foreach example $examples {
    set path [file join $env(TEST_CORPUS) [dict get $example Document]]
    set the_re [join [dict get $example Regex]]
    set livecheck.type [dict get $example Type]
    set livecheck.version [dict get $example Current]
    set base ""
    set baseUpdated -1
    if {![dict get $example Traps]} {
        set updated -1; set foundmatch 0; set updated_version 0
        set chan [open $path r]
        set tempfd $chan
        eval $block
        close $chan
        if {$foundmatch} {set base $updated_version}
        set baseUpdated $updated
    }
    set fd [open $path r]; set page [read $fd]; close $fd
    set mode [expr {${livecheck.type} eq "regexm" ? "page" : "line"}]
    set versions [::dockhand::extract_versions [dict get $example Regex] $page $mode]
    set mirror ""
    set mirrorUpdated -1
    if {[llength $versions]} {
        set args {}
        foreach version $versions {lappend args $version $version $version}
        set selection [::dockhand::select_version ${livecheck.version} {^(.*)$} {*}$args]
        set mirror [lindex $versions [lindex $selection 1]]
        set comparison [lindex $selection 0]
        set mirrorUpdated [expr {$comparison > 0 ? 1 : ($comparison == 0 ? 0 : -1)}]
    }
    lappend report [list [dict get $example Name] $base $baseUpdated $mirror $mirrorUpdated]
}
foreach row $report {puts [join $row "\t"]}
`
	filename := filepath.Join(root, "differential.tcl")
	require.NoError(t, os.WriteFile(filename, []byte(script), 0600))
	command := exec.CommandContext(t.Context(), executable, filename)
	command.Env = append(os.Environ(), "TEST_ROOT="+root, "TEST_BASE="+base, "TEST_CORPUS="+corpus)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	rows := strings.Split(strings.TrimSpace(string(output)), "\n")
	require.Len(t, rows, len(examples))
	for i, row := range rows {
		fields := strings.Split(row, "\t")
		require.Len(t, fields, 5, row)
		example := examples[i]
		require.Equal(t, example.Name, fields[0])
		if !example.Traps {
			require.Equal(t, example.Version, fields[1], "Base's version for %s", example.Name)
			require.Equal(t, example.Updated, atoi(t, fields[2]), "Base's verdict for %s", example.Name)
		}
		require.Equal(t, example.Version, fields[3], "the mirror's version for %s", example.Name)
		require.Equal(t, example.Updated, atoi(t, fields[4]), "the mirror's verdict for %s", example.Name)
	}
}

func atoi(t *testing.T, value string) int {
	t.Helper()
	number, err := strconv.Atoi(value)
	require.NoError(t, err, value)
	return number
}
