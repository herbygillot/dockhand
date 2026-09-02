package verdict

// The settle-time log readers, tabled. What these four say becomes the
// note: the state status renders, the detail the PR body quotes, the
// lint box's corroboration. Each is driven here over synthesized lines
// that reach every branch; corpus_test.go then drives all of them over
// the guest-log corpus, singly and composed through JudgeRun.
//
// These tables came with the readers from the lifecycle package. They
// are the same cases, asserting the same answers, run where the readers
// now live — and they now need no repository to run at all.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLintSummaryTable(t *testing.T) {
	cases := []struct{ name, log, want string }{
		{"empty log", "", ""},
		{"no lint line", "no lint ran here", ""},
		{"clean", "--->  0 errors and 0 warnings found.\n", "clean"},
		{"one warning, singular", "--->  0 errors and 1 warning found.\n", "1 warning"},
		{"one warning, unsingularized", "--->  0 errors and 1 warnings found.\n", "1 warning"},
		{"several warnings", "--->  0 errors and 3 warnings found.\n", "3 warnings"},
		{"a two-digit count", "--->  0 errors and 12 warnings found.\n", "12 warnings"},
		// Errors are not counted: port lint's exit code already failed
		// the run on them, so the summary answers the one question a
		// passing run leaves open.
		{"errors alone read clean", "--->  2 errors and 0 warnings found.\n", "clean"},
		{"errors beside warnings", "--->  1 error and 1 warning found.\n", "1 warning"},
		{"the line inside a full log",
			"--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n",
			"2 warnings"},
		{"the first summary wins",
			"--->  0 errors and 2 warnings found.\n--->  0 errors and 0 warnings found.\n",
			"2 warnings"},
		{"lint's own wording, not a lookalike", "found 3 warnings", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, LintSummary(tc.log))
		})
	}
}

func TestFailureSummaryTable(t *testing.T) {
	long := "Error: " + strings.Repeat("x", 200)
	cases := []struct{ name, log, want string }{
		{"empty log", "", ""},
		{"no Error line", "ld: symbol not found\n", ""},
		{"only the pointers",
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n" +
				"Error: Follow https://guide.macports.org/#project.tickets if you believe there is a bug.\n",
			""},
		{"the phase line, pointers dropped",
			"--->  Building jq\n" +
				"Error: Failed to build jq: command execution failed\n" +
				"Error: See /opt/local/var/macports/logs/x/main.log for details.\n",
			"Failed to build jq: command execution failed"},
		{"a pointer before the substantive line is skipped, not chosen",
			"Error: See /opt/local/var/macports/logs/x/main.log for details.\n" +
				"Error: Processing of port jq failed\n",
			"Processing of port jq failed"},
		{"the first substantive line wins",
			"Error: jq @1.8 cannot be built with this Xcode\n" +
				"Error: Failed to fetch jq: incompatible Xcode version\n",
			"jq @1.8 cannot be built with this Xcode"},
		{"DEBUG noise between the lines is ignored",
			"DEBUG: Executing org.macports.build (jq)\n" +
				"Error: Failed to build jq: command execution failed\n" +
				"DEBUG: Error code: CHILDSTATUS 4311 2\n",
			"Failed to build jq: command execution failed"},
		{"leading whitespace is trimmed", "   Error: boom\n", "boom"},
		{"a CRLF line is trimmed", "Error: boom\r\n", "boom"},
		{"a final line without a newline", "Error: boom", "boom"},
		{"the prefix wants its space", "Error:boom\n", ""},
		{"a bare prefix names nothing", "Error: \n", ""},
		{"a warning quoting an error is not one", "Warning: Error: boom\n", ""},
		// The cut lands before the prefix is trimmed, so a truncated
		// detail is at most 153 bytes and an ellipsis. Bytes, not runes.
		{"a line past 160 bytes is cut, with an ellipsis", long + "\n", strings.Repeat("x", 153) + "…"},
		{"a line of exactly 160 bytes is whole",
			"Error: " + strings.Repeat("x", 153) + "\n", strings.Repeat("x", 153)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FailureSummary(tc.log))
		})
	}
}

func TestPortDeclinedTable(t *testing.T) {
	cases := []struct {
		name, log string
		declined  bool
	}{
		{"empty log", "", false},
		{"a plain build failure", "Error: Failed to build jq: command execution failed\n", false},
		{"a linker failure", "ld: symbol not found\n", false},
		{"the port's own refusal", "Error: jq is known to fail on this platform\n", true},
		{"the option's name in the log", "DEBUG: known_fail yes\n", true},
		{"the marker inside a longer line", "--->  Skipping jq: known_fail on Monterey\n", true},
		{"the marker deep in a log",
			"--->  Verifying Portfile for jq\n--->  0 errors and 0 warnings found.\n" +
				"--->  Fetching distfiles for jq\nError: jq is known to fail on this platform\n" +
				"Error: Processing of port jq failed\n",
			true},
		// Conservative on purpose: an unrecognized refusal stays failed,
		// which is only a log read away from the truth; a false
		// unsupported releases the environment that could prove it.
		{"a capitalized lookalike", "Error: jq is Known To Fail here\n", false},
		{"a refusal in the port's own words", "Error: jq @1.8 cannot be built with this Xcode\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.declined, PortDeclined(tc.log))
		})
	}
}

// failedPortRE is the shape every MacPorts phase failure opens with,
// and the port it names is the whole blame decision.
func TestFailedPortRETable(t *testing.T) {
	cases := []struct{ name, line, want string }{
		{"the build phase", "Failed to build olm: command execution failed", "olm"},
		{"the configure phase", "Failed to configure py312-cryptography: configure failure", "py312-cryptography"},
		{"any phase word", "Failed to destroot libedit: command execution failed", "libedit"},
		{"a port name with a dot", "Failed to build R-data.table: command execution failed", "R-data.table"},
		{"a port name with pluses", "Failed to build libc++: command execution failed", "libc++"},
		{"a port name with an underscore", "Failed to activate py312-setuptools_scm: x", "py312-setuptools_scm"},
		{"no colon after the port", "Failed to build olm", ""},
		{"a version after the port breaks the shape", "Failed to build olm @3.2.16: x", ""},
		{"phase words are lowercase", "Failed to Build olm: x", ""},
		{"port's capitalization, not another", "failed to build olm: x", ""},
		{"anchored to the line start: the Error prefix must already be gone", "Error: Failed to build olm: x", ""},
		{"the trailer names no phase", "Processing of port gomuks failed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ""
			if m := failedPortRE.FindStringSubmatch(tc.line); m != nil {
				got = m[1]
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDependencyFailureTable(t *testing.T) {
	cases := []struct {
		name, summary, port string
		dep                 string
		ok                  bool
	}{
		{"a dependency breaking first", "Failed to build olm: command execution failed", "gomuks", "olm", true},
		{"the port failing itself", "Failed to build gomuks: command execution failed", "gomuks", "", false},
		{"every phase carries the shape", "Failed to configure py312-cryptography: configure failure", "py312-pyopenssl", "py312-cryptography", true},
		{"a line naming no port", "ld: symbol not found", "gomuks", "", false},
		{"an empty summary", "", "gomuks", "", false},
		{"the trailer, when it is all the log had", "Processing of port gomuks failed", "gomuks", "", false},
		{"a refusal in the port's own words", "jq is known to fail on this platform", "jq", "", false},
		{"the summary is compared whole, not by prefix", "Failed to build olm-devel: x", "olm", "olm-devel", true},
		{"a subport failing itself", "Failed to build pcre2: x", "pcre2", "", false},
		{"the parent failing under a subport's verification", "Failed to build pcre: x", "pcre2", "pcre", true},
		{"a truncated summary still opens with the shape",
			"Failed to build olm: " + strings.Repeat("x", 139) + "…", "gomuks", "olm", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dep, ok := DependencyFailure(tc.summary, tc.port)
			assert.Equal(t, tc.dep, dep)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

// A nomaintainer dependency means there is no one to nudge; the detail
// says so when the tree could prove it, and stays silent when it could
// not. The lookup itself is the caller's — this is the sentence it
// produces either way.
func TestBlockedDetailAnnotatesNomaintainer(t *testing.T) {
	assert.Equal(t, "dependency olm (nomaintainer) fails to build; the change itself is untested",
		BlockedDetail("olm", true))
	assert.Equal(t, "dependency zlib fails to build; the change itself is untested",
		BlockedDetail("zlib", false), "an unfindable port is simply not annotated")
}

// BlamedDependency is the guard order a caller must not get wrong: a
// port refusing the platform is not a blocked run, so it must not send
// anyone globbing the tree for a dependency that was never blamed.
func TestBlamedDependencyKeepsTheRefusalGuardFirst(t *testing.T) {
	blocked := "Error: Failed to build olm: command execution failed\n" +
		"Error: Processing of port gomuks failed\n"
	dep, ok := BlamedDependency(blocked, "gomuks")
	assert.True(t, ok)
	assert.Equal(t, "olm", dep)

	// The same blame shape, in a log that also carries a refusal: the
	// refusal wins, and nothing is blamed.
	declined := "--->  Skipping jq: known_fail on Monterey\n" + blocked
	dep, ok = BlamedDependency(declined, "gomuks")
	assert.False(t, ok, "a declined platform is not a blocked run")
	assert.Empty(t, dep)

	// And the port failing itself blames nobody.
	_, ok = BlamedDependency("Error: Failed to build gomuks: command execution failed\n", "gomuks")
	assert.False(t, ok)
}
