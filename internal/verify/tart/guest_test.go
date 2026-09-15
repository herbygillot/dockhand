package tart

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// Execute the shipped Tcl program with mocked external commands and MacPorts
// metadata. All file writes stay under the test root; no VM or host port runs.
func TestGuestBuildsEditedRootBeforeDependent(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts Tcl required")
	}
	for _, failure := range []string{"", "rootport", "external"} {
		t.Run("failure="+failure, func(t *testing.T) {
			root := t.TempDir()
			prefix := filepath.Join(root, "prefix")
			require.NoError(t, os.MkdirAll(filepath.Join(prefix, "etc/macports"), 0700))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "ports"), 0700))
			for _, name := range []string{"PortIndex", "PortIndex.quick"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, "ports", name), nil, 0600))
			}
			input := guestInput{Protocol: 1, ID: "request", Digest: "digest", Prefix: prefix, Spec: record.BuildSpec{Target: record.Target{Name: "downstream", Portfile: "devel/downstream/Portfile"}, Preinstall: []record.Target{{Name: "rootport", Portfile: "devel/rootport/Portfile", Variants: map[string]bool{"debug": true}}}, Config: record.BuildConfig{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Tests: record.TestDeclared}}}
			data, err := json.Marshal(input)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, "input.json"), data, 0600))
			prelude := `
package require json
package require json::write
package provide macports 1.0
namespace eval macports {variable os_platform darwin; variable os_major 25; variable build_arch arm64}
proc mportinit {} {}
proc mportopen {args} {return handle}
proc mportinfo {handle} {return {name downstream}}
proc ditem_key {args} {return worker}
proc worker {args} {return 1}
proc mportclose {args} {}
rename file realFile
proc file {op args} {
 if {$op eq "exists" && [lindex $args 0] in {/opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg}} {return 0}
 return [realFile $op {*}$args]
}
rename exec realExec
proc exec {args} {
 if {[lindex $args 0] eq "/usr/bin/id"} {return root}
 if {[lindex $args 0] eq "/usr/bin/xcode-select"} {return /Library/Developer/CommandLineTools}
 if {[lsearch -exact $args build]>=0} {
  set selection [expr {[lsearch -exact $args subport=rootport]>=0 ? "rootport" : "downstream"}]
  set failure $::env(TEST_FAILURE)
  if {($failure eq "rootport" && $selection eq "rootport") || ($failure eq "external" && $selection eq "downstream")} {
   puts $::log "Error: Failed to build $failure: test failure"
   return -code error "test failure"
  }
 }
 return ""
}
`
			script := prelude + strings.Replace(string(guestScript), "set root /var/tmp/dockhand2", "set root $env(TEST_ROOT)", 1)
			filename := filepath.Join(root, "guest.tcl")
			require.NoError(t, os.WriteFile(filename, []byte(script), 0600))
			command := exec.CommandContext(t.Context(), executable, filename)
			command.Env = append(os.Environ(), "TEST_ROOT="+root, "TEST_FAILURE="+failure)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			data, err = os.ReadFile(filepath.Join(root, "result.json"))
			require.NoError(t, err)
			var result guestResult
			require.NoError(t, json.Unmarshal(data, &result))
			require.NotEmpty(t, result.Steps)
			require.Equal(t, "rootport", result.Steps[0].Package)
			require.Equal(t, "build", result.Steps[0].Phase)
			require.Contains(t, result.Steps[0].Command, "+debug")
			require.Contains(t, result.Steps[0].Command, "-d")
			require.NotContains(t, result.Steps[0].Command, "-s")
			if failure == "" {
				require.Equal(t, record.VerdictPassed, result.Verdict)
				require.Len(t, result.Steps, 6)
				require.Equal(t, "install", result.Steps[1].Phase)
				require.Equal(t, "downstream", result.Steps[2].Package)
				require.Equal(t, "lint", result.Steps[2].Phase)
				require.NotContains(t, result.Steps[2].Command, "-d")
			} else {
				require.Equal(t, record.VerdictFailed, result.Verdict)
				require.NotNil(t, result.Failure)
				require.Equal(t, "dependency", string(result.Failure.Kind))
				require.Equal(t, failure, result.Failure.Package)
				require.Equal(t, "unknown", string(result.Failure.Attribution))
			}
		})
	}
}
