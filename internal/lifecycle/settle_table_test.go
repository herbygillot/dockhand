package lifecycle

// The settle-time log readers, tabled. SettleRuns reads a finished
// guest log with four small functions — LintSummary, failureSummary,
// portDeclined, dependencyFailure (over failedPortRE) — and what they
// say becomes the note: the state status renders, the detail the PR
// body quotes, the lint box's corroboration. Each reader is driven
// here over synthesized lines that reach every branch, and then all of
// them over testdata/logs, a corpus of tart-shaped guest logs whose
// sidecar .expect files state the judgment the readers must reach —
// singly, and composed through SettleRuns itself. The corpus is swept
// by directory listing, so a real `dockhand log` capture dropped there
// is picked up with no code change; testdata/logs/README.md says how.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
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
		{"a line past 160 bytes is cut, with an ellipsis", long + "\n", strings.Repeat("x", 153) + "…"},
		{"a line of exactly 160 bytes is whole",
			"Error: " + strings.Repeat("x", 153) + "\n", strings.Repeat("x", 153)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, failureSummary(tc.log))
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
			assert.Equal(t, tc.declined, portDeclined(tc.log))
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
			dep, ok := dependencyFailure(tc.summary, tc.port)
			assert.Equal(t, tc.dep, dep)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

// corpusExpect is a .expect sidecar: the two inputs a log alone cannot
// carry — the port under test, and what the guest's state file said —
// and the judgment the readers must reach from it.
type corpusExpect struct {
	port, outcome, state, blamed, detail, lint string
}

// readCorpusExpect parses the key: value sidecar. It is strict about
// keys and enumerations because a typo in a hand-written expectation
// must fail the sweep by name, never pass as an empty string.
func readCorpusExpect(t *testing.T, path string) corpusExpect {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "every corpus log needs its sidecar; see testdata/logs/README.md")
	var e corpusExpect
	seen := map[string]bool{}
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		require.True(t, ok, "%s: %q is not a key: value line", path, line)
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		require.False(t, seen[key], "%s: %s given twice", path, key)
		seen[key] = true
		switch key {
		case "port":
			e.port = value
		case "outcome":
			e.outcome = value
		case "state":
			e.state = value
		case "blamed":
			e.blamed = value
		case "detail":
			e.detail = value
		case "lint":
			e.lint = value
		default:
			require.Failf(t, "unknown sidecar key", "%s: %q; the keys are port, outcome, state, blamed, detail, lint", path, key)
		}
	}
	require.NotEmpty(t, e.port, "%s: port names what the note names", path)
	require.Contains(t, []string{"passed", "failed"}, e.outcome, "%s: outcome is the guest's state file", path)
	require.Contains(t, []string{"passed", "failed", "blocked", "unsupported"}, e.state, "%s: state", path)
	if e.outcome == "passed" {
		require.Equal(t, "passed", e.state, "%s: a passing guest settles as passed", path)
		require.Empty(t, e.detail, "%s: a pass carries no detail", path)
	}
	require.Equal(t, e.state == "blocked", e.blamed != "", "%s: blamed is set exactly when blocked", path)
	return e
}

// corpusNote seeds the tip with one running, linted job for the
// corpus port, the way RecordRun leaves it after a submit.
func corpusNote(t *testing.T, repo *git.Repo, sha, port string) Note {
	t.Helper()
	return seededNote(t, repo, sha, port, true)
}

// seededNote is corpusNote with the lint record chosen: a run
// submitted without lint never reads the log on a pass.
func seededNote(t *testing.T, repo *git.Repo, sha, port string, linted bool) Note {
	t.Helper()
	ctx := context.Background()
	n, err := LoadOrStartNote(ctx, repo, sha, port)
	require.NoError(t, err)
	n.Runs["Testos"] = Run{State: "running",
		Job: verify.Job{Provider: "fake", ID: "fake-1"}, Linted: linted}
	require.NoError(t, WriteNote(ctx, repo, n))
	return n
}

// The composition branches no log can reach: SettleRuns's own handling
// of a provider that fails once the verdict is known, and of a run
// that never linted. A pass whose worker cannot be released still
// settles passed and says so in its detail; a failure whose log cannot
// be read still settles failed, handle kept, with no diagnosis to
// quote; a pass whose log cannot be read settles passed with no lint
// evidence; and a pass that never linted reads no log at all, so a
// lint line in it is not evidence.
func TestSettleRunsProviderFailuresTable(t *testing.T) {
	passed := map[string]verify.Status{"fake-1": {State: verify.Passed, Handle: "fake-1"}}
	failed := map[string]verify.Status{"fake-1": {State: verify.Failed, Handle: "fake-1"}}
	lintLog := map[string]string{"fake-1": "--->  Verifying Portfile for jq\n--->  0 errors and 2 warnings found.\n--->  Activating jq\n"}
	unreadable := map[string]error{"fake-1": errors.New("ssh: connection reset by peer")}
	cases := []struct {
		name     string
		fake     *verifytest.Fake
		linted   bool
		state    string
		detail   string
		lint     string
		handle   string
		released []string
	}{
		{name: "a pass whose release fails",
			fake: &verifytest.Fake{States: passed, Logs: lintLog,
				ReleaseErr: map[string]error{"fake-1": errors.New("tart delete: vm is busy")}},
			linted: true, state: "passed", detail: "worker not released: tart delete: vm is busy", lint: "2 warnings"},
		{name: "a failure whose log cannot be read",
			fake:   &verifytest.Fake{States: failed, LogErr: unreadable},
			linted: true, state: "failed", handle: "fake-1"},
		{name: "a pass whose log cannot be read",
			fake:   &verifytest.Fake{States: passed, LogErr: unreadable},
			linted: true, state: "passed", released: []string{"fake-1"}},
		{name: "a pass that never linted reads no log",
			fake:   &verifytest.Fake{States: passed, Logs: lintLog},
			linted: false, state: "passed", released: []string{"fake-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, sha := lifecycleRepo(t)
			n := seededNote(t, repo, sha, "jq", tc.linted)
			require.NoError(t, SettleRuns(context.Background(), testState(t, repo, tc.fake), repo, &n))
			r := n.Runs["Testos"]
			assert.Equal(t, tc.state, r.State, "state")
			assert.Equal(t, tc.detail, r.Detail, "detail")
			assert.Equal(t, tc.lint, r.Lint, "lint evidence")
			assert.Equal(t, tc.handle, r.Handle, "handle")
			assert.Equal(t, tc.released, tc.fake.Released, "released")

			// The settle was written back whatever the provider did: a
			// fresh read agrees with the one in hand.
			again, err := ReadNote(context.Background(), repo, sha)
			require.NoError(t, err)
			assert.Equal(t, r, again.Runs["Testos"])
		})
	}
}

func TestLogCorpus(t *testing.T) {
	dir := filepath.Join("testdata", "logs")
	logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, logs, "%s must hold at least the reconstructed shapes", dir)

	for _, path := range logs {
		name := strings.TrimSuffix(filepath.Base(path), ".log")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			log := string(raw)
			exp := readCorpusExpect(t, strings.TrimSuffix(path, ".log")+".expect")

			// Each reader on its own, consulted as the settle consults
			// it: lint is what the log's lint line says whatever the
			// outcome; the failure-side readers answer only for a
			// failed guest.
			assert.Equal(t, exp.lint, LintSummary(log), "lint summary")
			if exp.outcome == "failed" {
				assert.Equal(t, exp.state == "unsupported", portDeclined(log), "refusal")
				summary := failureSummary(log)
				dep, blocked := dependencyFailure(summary, exp.port)
				assert.Equal(t, exp.blamed, dep, "blamed port")
				assert.Equal(t, exp.state == "blocked", blocked, "blocked")
				if exp.state == "failed" {
					assert.Equal(t, exp.detail, summary, "a failure's detail is the first substantive Error line")
				}
			}

			// And composed, through SettleRuns itself: the state the
			// note records, the detail it carries, and what happens to
			// the worker — kept for a failure, released for anything
			// else, because only one's own breakage is worth a slot.
			t.Run("settle", func(t *testing.T) {
				repo, sha := lifecycleRepo(t)
				st := verify.Status{State: verify.Failed, Handle: "fake-1"}
				if exp.outcome == "passed" {
					st = verify.Status{State: verify.Passed, Handle: "fake-1"}
				}
				fake := &verifytest.Fake{
					States: map[string]verify.Status{"fake-1": st},
					Logs:   map[string]string{"fake-1": log},
				}
				n := corpusNote(t, repo, sha, exp.port)

				require.NoError(t, SettleRuns(context.Background(), testState(t, repo, fake), repo, &n))
				r := n.Runs["Testos"]
				assert.Equal(t, exp.state, r.State, "state")
				assert.Equal(t, exp.detail, r.Detail, "detail")
				if exp.outcome == "passed" {
					assert.Equal(t, exp.lint, r.Lint, "lint evidence is read before the release")
				} else {
					assert.Empty(t, r.Lint, "lint is corroborated on a pass; a failed run's log stays reachable")
				}
				if exp.state == "failed" {
					assert.Equal(t, "fake-1", r.Handle, "the failure's environment is the debug handle")
					assert.Empty(t, fake.Released, "a failed run's worker is kept")
				} else {
					assert.Empty(t, r.Handle, "nothing of this branch's to debug")
					assert.Equal(t, []string{"fake-1"}, fake.Released, "the worker is released")
				}
			})
		})
	}
}
