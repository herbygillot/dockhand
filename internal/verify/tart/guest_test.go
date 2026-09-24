package tart

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// Execute the shipped Tcl program with mocked external commands and MacPorts
// metadata. All file writes stay under the test root; no VM or host port runs.
func TestGuestBuildsEditedRootBeforeDependent(t *testing.T) {
	t.Parallel()
	executable := testsupport.MacPortsTclsh(t)
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
proc launch {argv} {return [open |[list /bin/sh -c {echo "tests ran"; exit 0}] r]}
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
			script := prelude + testGuestScript(t)
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

// The declared policy runs the port's tests and records how they went without
// letting them decide the verdict, as the MacPorts workflow does; required
// makes them decisive; a hung test is stopped at the timeout either way.
func TestGuestTestPolicyDecidesWhetherTestsAreAdvisory(t *testing.T) {
	t.Parallel()
	executable := testsupport.MacPortsTclsh(t)
	for _, tc := range []struct {
		policy  record.TestPolicy
		mode    string
		verdict record.Verdict
		failure string
	}{
		{record.TestDeclared, "pass", record.VerdictPassed, ""},
		{record.TestDeclared, "fail", record.VerdictPassed, "exit"},
		{record.TestDeclared, "hang", record.VerdictPassed, "timed out after 1s"},
		{record.TestRequired, "pass", record.VerdictPassed, ""},
		{record.TestRequired, "fail", record.VerdictFailed, ""},
		{record.TestRequired, "hang", record.VerdictFailed, ""},
	} {
		t.Run(string(tc.policy)+"/"+tc.mode, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			prefix := filepath.Join(root, "prefix")
			require.NoError(t, os.MkdirAll(filepath.Join(prefix, "etc/macports"), 0700))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "ports"), 0700))
			for _, name := range []string{"PortIndex", "PortIndex.quick"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, "ports", name), nil, 0600))
			}
			input := guestInput{Protocol: 1, ID: "request", Digest: "digest", Prefix: prefix, TestTimeoutSeconds: 1, Spec: record.BuildSpec{Target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, Config: record.BuildConfig{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Tests: tc.policy}}}
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
proc mportinfo {handle} {return {name fixture}}
proc ditem_key {args} {return worker}
proc worker {args} {return 1}
proc mportclose {args} {}
rename file realFile
proc file {op args} {
 if {$op eq "exists" && [lindex $args 0] in {/opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg}} {return 0}
 return [realFile $op {*}$args]
}
proc launch {argv} {
 switch $::env(TEST_MODE) {
  hang {return [open |[list /bin/sh -c {echo "tests running"; exec sleep 60}] r]}
  fail {return [open |[list /bin/sh -c {echo "a test failed"; exit 3}] r]}
 }
 return [open |[list /bin/sh -c {echo "tests ran"; exit 0}] r]
}
rename exec realExec
proc exec {args} {
 if {[lindex $args 0] eq "kill"} {return [realExec {*}$args]}
 if {[lindex $args 0] eq "/usr/bin/id"} {return root}
 if {[lindex $args 0] eq "/usr/bin/xcode-select"} {return /Library/Developer/CommandLineTools}
 return ""
}
`
			script := prelude + testGuestScript(t)
			filename := filepath.Join(root, "guest.tcl")
			require.NoError(t, os.WriteFile(filename, []byte(script), 0600))
			command := exec.CommandContext(t.Context(), executable, filename)
			command.Env = append(os.Environ(), "TEST_ROOT="+root, "TEST_MODE="+tc.mode)
			started := time.Now()
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			data, err = os.ReadFile(filepath.Join(root, "result.json"))
			require.NoError(t, err)
			var result guestResult
			require.NoError(t, json.Unmarshal(data, &result))
			require.Equal(t, tc.verdict, result.Verdict)
			var test *record.StepResult
			for i := range result.Steps {
				if result.Steps[i].Phase == "test" {
					test = &result.Steps[i]
				}
			}
			require.NotNil(t, test, "the test phase ran")
			log, err := os.ReadFile(filepath.Join(root, "build.log"))
			require.NoError(t, err)
			require.Contains(t, string(log), map[string]string{"pass": "tests ran", "fail": "a test failed", "hang": "tests running"}[tc.mode], "test output reaches the build log")
			if tc.mode == "hang" {
				require.Less(t, time.Since(started), 40*time.Second, "the hung test was stopped")
				require.Contains(t, test.Detail, "timed out after 1s")
			}
			if tc.mode == "pass" {
				require.Equal(t, record.VerdictPassed, test.Verdict)
				require.Empty(t, result.TestFailure)
				return
			}
			require.Equal(t, record.VerdictFailed, test.Verdict)
			if tc.policy == record.TestDeclared {
				require.Contains(t, result.TestFailure, tc.failure)
				require.Nil(t, result.Failure)
				require.Equal(t, "install", result.Steps[len(result.Steps)-1].Phase, "the build went on to install")
			} else {
				require.Empty(t, result.TestFailure)
				require.Contains(t, result.Detail, "test failed")
				require.Equal(t, "test", result.Steps[len(result.Steps)-1].Phase, "the build stopped at the test phase")
			}
		})
	}
}

// A failed step keeps what MacPorts said in the log rather than the exit
// message: its Error lines up to the one naming the package and phase, and
// for a distfile that failed to fetch each mirror tried for it with its
// reason. Lines from an earlier step, and a fallback that succeeded for
// another distfile, stay out.
func TestGuestKeepsMacPortsErrorLinesAndMirrorAttempts(t *testing.T) {
	t.Parallel()
	executable := testsupport.MacPortsTclsh(t)
	root := t.TempDir()
	prefix := filepath.Join(root, "prefix")
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "etc/macports"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ports"), 0700))
	for _, name := range []string{"PortIndex", "PortIndex.quick"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "ports", name), nil, 0600))
	}
	input := guestInput{Protocol: 1, ID: "request", Digest: "digest", Prefix: prefix, Spec: record.BuildSpec{Target: record.Target{Name: "downstream", Portfile: "devel/downstream/Portfile"}, Config: record.BuildConfig{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Tests: record.TestDeclared}}}
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
 if {[lsearch -exact $args lint]>=0} {
  puts $::log "Error: Failed to build stale: an earlier step's line"
 }
 if {[lsearch -exact $args build]>=0} {
  puts $::log "Attempting to fetch https://a.example/other-1.0.tar.gz"
  puts $::log "DEBUG: Fetching https://a.example/other-1.0.tar.gz failed: The requested URL returned error: 404"
  puts $::log "Attempting to fetch https://b.example/other-1.0.tar.gz"
  puts $::log "--->  Attempting to fetch https://a.example/jxrlib-1.4.3.tar.gz"
  puts $::log "DEBUG: Fetching https://a.example/jxrlib-1.4.3.tar.gz failed: The requested URL returned error: 404"
  puts $::log "Attempting to fetch https://b.example/jxrlib-1.4.3.tar.gz"
  puts $::log "DEBUG: Fetching https://b.example/jxrlib-1.4.3.tar.gz failed: The requested URL returned error: 403"
  puts $::log "Error: Failed to fetch jxrlib-1.4.3.tar.gz: The requested URL returned error: 404"
  puts $::log "Error: Failed to fetch jxrlib: Failed to fetch distfiles"
  puts $::log "DEBUG: Backtrace: Failed to fetch distfiles"
  puts $::log "Error: See /opt/local/var/macports/logs/jxrlib/main.log for details."
  puts $::log "Error: Follow https://guide.macports.org/#project.tickets if you believe there is a bug."
  puts $::log "Error: Processing of port downstream failed"
  return -code error "child process exited abnormally"
 }
 return ""
}
`
	script := prelude + testGuestScript(t)
	filename := filepath.Join(root, "guest.tcl")
	require.NoError(t, os.WriteFile(filename, []byte(script), 0600))
	command := exec.CommandContext(t.Context(), executable, filename)
	command.Env = append(os.Environ(), "TEST_ROOT="+root)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	data, err = os.ReadFile(filepath.Join(root, "result.json"))
	require.NoError(t, err)
	var result guestResult
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, record.VerdictFailed, result.Verdict)
	const cause = "Failed to fetch jxrlib-1.4.3.tar.gz: The requested URL returned error: 404; Failed to fetch jxrlib: Failed to fetch distfiles"
	require.Equal(t, "build failed: "+cause, result.Detail)
	require.Len(t, result.Steps, 2)
	require.Equal(t, record.VerdictFailed, result.Steps[1].Verdict)
	require.Equal(t, cause, result.Steps[1].Detail)
	require.NotNil(t, result.Failure)
	require.Equal(t, record.DependencyFailure, result.Failure.Kind)
	require.Equal(t, "jxrlib", result.Failure.Package)
	require.Equal(t, "fetch", result.Failure.Phase)
	require.Equal(t, cause, result.Failure.Detail)
	require.Equal(t, []record.FetchAttempt{
		{URL: "https://a.example/jxrlib-1.4.3.tar.gz", Reason: "The requested URL returned error: 404"},
		{URL: "https://b.example/jxrlib-1.4.3.tar.gz", Reason: "The requested URL returned error: 403"},
	}, result.Failure.Fetches)
}

// testGuestScript is the guest script rooted at the test's directory. The
// replacement is checked, so a guest directory spelled differently cannot
// leave the script writing to the real one.
func testGuestScript(t *testing.T) string {
	t.Helper()
	root := "set root " + guestDirectory
	require.Contains(t, string(guestScript), root)
	return strings.Replace(string(guestScript), root, "set root $env(TEST_ROOT)", 1)
}
