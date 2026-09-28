package macports_test

import (
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A maintainers value reads as MacPorts reads it: entries, each the
// spellings of one maintainer, an empty one dropped. (The private-helper
// review of 2026-09-28's table.)
func TestMaintainersReadAsMacPortsReadsThem(t *testing.T) {
	maintainers, err := macports.ReadMaintainers("{@ada example.org:ada} openmaintainer {} {bo {c d}}")
	require.NoError(t, err)
	require.Equal(t, []macports.Maintainer{{"@ada", "example.org:ada"}, {"openmaintainer"}, {"bo", "c d"}}, maintainers)
	for _, malformed := range []string{"{@ada example.org:ada", `{ada "bo}`} {
		_, err = macports.ReadMaintainers(malformed)
		require.Error(t, err, malformed)
	}
}

// Each spelling of a maintainer is one identity, as MacPorts reads
// spellings; a GitHub handle as Repology spells it is one too.
func TestAMaintainersSpellingsAreOneIdentity(t *testing.T) {
	for spelling, identity := range map[string]string{
		"@Ada":            "@ada",
		"ada@github":      "@ada",
		"Ada@Example.org": "ada@example.org",
		"example.org:ada": "ada@example.org",
		"example.org:a:b": "a:b@example.org",
		"ada":             "ada@macports.org",
		"openmaintainer":  "openmaintainer",
		"NoMaintainer":    "nomaintainer",
	} {
		require.Equal(t, identity, macports.MaintainerIdentity(spelling), spelling)
	}
	require.True(t, macports.MaintainerKeyword("openmaintainer"))
	require.True(t, macports.MaintainerKeyword("nomaintainer"))
	require.False(t, macports.MaintainerKeyword("ada"))
}

// checkedLines are maintainers lines as MacPorts writes them.
var checkedLines = []string{
	"{@ada example.org:ada} openmaintainer",
	"ada@example.org",
	"@ada",
	"nomaintainer",
	"{@ada\texample.org:ada}  {@bo bo} {}",
}

// A maintainers line is checked as MacPorts writes one, and holds nothing
// Tcl reads specially, since create writes it into a Portfile as it is.
func TestAMaintainersLineIsCheckedAsMacPortsWritesOne(t *testing.T) {
	for _, line := range checkedLines {
		require.NoError(t, macports.CheckMaintainers(line), line)
	}
	for line, problem := range map[string]string{
		"{@ada example.org:ada":  "leaves a group open",
		"@ada}":                  "has a stray brace",
		"a}b":                    "has a stray brace",
		"{a}b":                   "has a stray brace",
		"{a {b}}":                "opens a group inside another",
		"":                       "has no entries",
		"{} { }":                 "has no entries",
		"@ada$x":                 "has '$', which Tcl reads specially",
		"[exec rm -rf ~]":        "has '['",
		`"@ada example.org:ada"`: `has '"'`,
		"@ada\nopenmaintainer":   `has '\n'`,
		"@ada; openmaintainer":   "has ';'",
		`@ada\ bo`:               `has '\\'`,
	} {
		require.ErrorContains(t, macports.CheckMaintainers(line), problem, line)
	}
}

// A checked line means the same as a Portfile's words, as MacPorts' Tcl
// reads them, as it does read as the port index's list.
func TestACheckedMaintainersLineReadsAsItsWords(t *testing.T) {
	tclsh := testsupport.MacPortsTclsh(t)
	script := strings.Builder{}
	script.WriteString(`proc maintainers {args} {
    foreach entry $args {
        if {[llength $entry] == 0} continue
        set spellings {}
        foreach spelling $entry {
            lappend spellings [binary encode hex [encoding convertto utf-8 $spelling]]
        }
        puts [join $spellings ,]
    }
    puts --
}
`)
	var want strings.Builder
	for _, line := range checkedLines {
		script.WriteString("maintainers " + line + "\n")
		maintainers, err := macports.ReadMaintainers(line)
		require.NoError(t, err, line)
		for _, maintainer := range maintainers {
			spellings := make([]string, len(maintainer))
			for i, spelling := range maintainer {
				spellings[i] = hex.EncodeToString([]byte(spelling))
			}
			want.WriteString(strings.Join(spellings, ",") + "\n")
		}
		want.WriteString("--\n")
	}
	command := exec.CommandContext(t.Context(), tclsh)
	command.Stdin = strings.NewReader(script.String())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Equal(t, want.String(), string(output))
}
