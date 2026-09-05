package tart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// The runner script is one argv word the guest executes, so these bytes
// are the build. Nothing pinned them before the cohort existed, which
// meant the claim "a single-subject verification runs what it always
// ran" rested on review alone. It rests on this literal now: the script
// is spelled out here rather than compared against the function that
// produced it, because agreeing with itself would prove only that string
// concatenation works.
//
// If this test fails, a guest is being asked to run something no
// verification in the field has ever run. That is a finding, not a
// golden to re-record.
const frozenRunner = `set -u
mkdir -p /tmp/dockhand-verify
echo running > /tmp/dockhand-verify/state
: > /tmp/dockhand-verify/log
nohup /bin/sh -c '
  ok=yes
  for f in /tmp/dockhand-verify/argv.lint /tmp/dockhand-verify/argv.test /tmp/dockhand-verify/argv; do
    [ -f "$f" ] || continue
    set --
    while IFS= read -r a; do set -- "$@" "$a"; done < "$f"
    sudo -n /opt/local/bin/port "$@" >> /tmp/dockhand-verify/log 2>&1 || { ok=no; break; }
  done
  if [ "$ok" = yes ]
  then echo passed > /tmp/dockhand-verify/state
  else echo failed > /tmp/dockhand-verify/state
  fi
' >/dev/null 2>&1 &
`

func TestRunnerScriptIsFrozen(t *testing.T) {
	assert.Equal(t, frozenRunner, runner("/opt/local/bin/port"))
	assert.Len(t, runner("/opt/local/bin/port"), 577,
		"the single-subject runner is 577 bytes and has been since it was written")
}

// runnerAt is what makes the script runnable in a test, and it earns
// that only if naming the conventional directory reproduces the frozen
// bytes exactly.
func TestRunnerAtReproducesTheFrozenScript(t *testing.T) {
	assert.Equal(t, frozenRunner, runnerAt(stateDir, "/opt/local/bin/port"))
}

// The whole of what launch hands a guest for a one-port request: three
// tart calls, two of them carrying an argv file on stdin, and the frozen
// runner. Captured from the tree before the cohort was written and
// asserted unchanged after — the file names, their order, their bodies,
// and the shell strings the names are interpolated into.
const soloTranscript = `ARGV exec dockhand-worker-cafe /bin/sh -c mkdir -p /tmp/dockhand-verify
ARGV exec -i dockhand-worker-cafe /bin/sh -c cat > /tmp/dockhand-verify/argv
STDIN<<
-d
-N
install
jq
STDIN>>
ARGV exec -i dockhand-worker-cafe /bin/sh -c cat > /tmp/dockhand-verify/argv.lint
STDIN<<
lint
jq
STDIN>>
ARGV exec dockhand-worker-cafe /bin/sh -c ` + frozenRunner + "\n"

func TestLaunchSendsTodaysBytesAtOneSubject(t *testing.T) {
	tools, transcript := stubTartIO(t)
	p := Provider{Tools: tools}

	require.NoError(t, p.launch(t.Context(), "dockhand-worker-cafe", verify.Request{Ports: []string{"jq"}}))

	assert.Equal(t, soloTranscript, transcript())
}

// Everything else a request can ask for still gets the frozen runner. A
// second port is the only thing that reaches the cohort, and that is
// worth asserting one option at a time: a gate written on Test, or on
// Manifest, or on a non-empty Variants would be invisible until the
// first field run of the option it caught.
func TestOnlyASecondPortReachesTheCohortRunner(t *testing.T) {
	v, err := info.Variants("+ssl")
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		req  verify.Request
	}{
		{"a bare request", verify.Request{Ports: []string{"jq"}}},
		{"with a test", verify.Request{Ports: []string{"jq"}, Test: true}},
		{"with a manifest", verify.Request{Ports: []string{"jq"}, Manifest: true}},
		{"with a variant frame", verify.Request{Ports: []string{"jq"}, Variants: v}},
		{"from source", verify.Request{Ports: []string{"jq"}, FromSource: []string{"jq"}}},
		{"needing xcode", verify.Request{Ports: []string{"jq"}, NeedsXcode: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, frozenRunner, launchScript("/opt/local/bin/port", tc.req))
		})
	}

	assert.NotEqual(t, frozenRunner,
		launchScript("/opt/local/bin/port", verify.Request{Ports: []string{"jq", "oniguruma"}}),
		"two ports is the cohort, and the cohort is a different script")
}

// A cohort writes the same three files per member under position-derived
// names, each preceded by the marker that says whose output follows and
// followed by the prerequisites the runner consults before attempting
// it — empty here, because the request declares no edges. The install
// of the headline comes first, then each subject in the order it is to
// be built.
func TestArgvFilesAtACohort(t *testing.T) {
	files := argvFiles(verify.Request{Ports: []string{"jq", "oniguruma"}})

	var got []string
	for _, f := range files {
		got = append(got, f.Dest()+" => "+strings.ReplaceAll(f.Body, "\n", "|"))
	}
	assert.Equal(t, []string{
		"/tmp/dockhand-verify/subject.0 => ===> dockhand subject: jq|",
		"/tmp/dockhand-verify/argv.0 => -d|-N|install|jq|",
		"/tmp/dockhand-verify/argv.0.lint => lint|jq|",
		"/tmp/dockhand-verify/requires.0 => ",
		"/tmp/dockhand-verify/subject.1 => ===> dockhand subject: oniguruma|",
		"/tmp/dockhand-verify/argv.1 => -d|-N|install|oniguruma|",
		"/tmp/dockhand-verify/argv.1.lint => lint|oniguruma|",
		"/tmp/dockhand-verify/requires.1 => ",
	}, got)
}

// fileNamed picks one instruction file out of the set by its name in
// the guest, so a test about one member's body does not depend on how
// many files the members before it were given.
func fileNamed(t *testing.T, files []argvFile, name string) argvFile {
	t.Helper()
	for _, f := range files {
		if f.Name == name {
			return f
		}
	}
	require.Failf(t, "no such instruction file", "%s is not among the files launch writes", name)
	return argvFile{}
}

// The prerequisite file carries positions and not names: the request
// speaks in port names, as the kernel does, and this is where they
// become the numbers the runner compares against its own state files.
// A name outside the request is not a member and is dropped, and so is
// a member naming itself; a member with nothing to wait for gets an
// empty file, not a missing one.
func TestRequiresFilesCarryCohortPositions(t *testing.T) {
	req := verify.Request{
		Ports: []string{"libfoo", "bar", "baz", "qux"},
		Requires: [][]string{
			nil,
			{"libfoo"},
			{"libfoo", "pkgconfig"},
			{"bar", "libfoo", "qux"},
		},
	}
	files := argvFiles(req)

	assert.Empty(t, fileNamed(t, files, "requires.0").Body, "the headline waits for nobody")
	assert.Equal(t, "0\n", fileNamed(t, files, "requires.1").Body)
	assert.Equal(t, "0\n", fileNamed(t, files, "requires.2").Body, "pkgconfig is not in the request, so it is not a prerequisite here")
	assert.Equal(t, "1\n0\n", fileNamed(t, files, "requires.3").Body,
		"in the request's own order, and never naming itself")

	// A request that carries no graph at all, or a shorter one than its
	// ports, declares no edges and still writes a file per member.
	short := argvFiles(verify.Request{Ports: []string{"jq", "oniguruma", "mise"}, Requires: [][]string{nil, {"jq"}}})
	assert.Equal(t, "0\n", fileNamed(t, short, "requires.1").Body)
	assert.Empty(t, fileNamed(t, short, "requires.2").Body, "a member past the end of the graph waits for nobody")
}

// One subject has no cohort to be ordered within, so a graph on a
// one-port request changes nothing about what the guest is told: the
// same two files, under the same names, with the same bodies.
func TestOneSubjectIgnoresTheGraph(t *testing.T) {
	plain := argvFiles(verify.Request{Ports: []string{"jq"}})
	graphed := argvFiles(verify.Request{Ports: []string{"jq"}, Requires: [][]string{{"oniguruma"}}})
	assert.Equal(t, plain, graphed)
	for _, f := range graphed {
		assert.NotContains(t, f.Name, "requires", "the single-subject runner never reads one")
	}
}

// The marker the runner prints and the marker the splitter reads are
// the same bytes because both come from verify.SubjectMarker. Asserting
// the file's body against that function is the one place agreeing with
// the producer is the point: the claim is that the guest and the judge
// cannot drift apart, not that the string is any particular string.
func TestTheSubjectFileCarriesExactlyTheMarkerTheSplitterReads(t *testing.T) {
	ports := []string{"jq", "oniguruma"}
	files := argvFiles(verify.Request{Ports: ports})

	var marked strings.Builder
	var seen int
	for _, f := range files {
		if strings.HasPrefix(f.Name, "subject.") {
			assert.Equal(t, verify.SubjectMarker(ports[seen])+"\n", f.Body)
			seen++
			marked.WriteString(f.Body)
			marked.WriteString("output of that port\n")
		}
	}
	require.Len(t, ports, seen, "every member is announced, once")
	assert.Equal(t, map[string]string{
		"jq":        "output of that port\n",
		"oniguruma": "output of that port\n",
	}, verify.SplitSubjects(marked.String()))
}

// A variant frame is the request's, and the request is about its
// headline. Handing it to a dependent that declares no such variant is
// a refusal from port(1) rather than a build, so the frame stops at
// subject zero.
func TestTheVariantFrameStopsAtTheHeadline(t *testing.T) {
	v, err := info.Variants("+ssl", "-doc")
	require.NoError(t, err)

	for _, f := range argvFiles(verify.Request{Ports: []string{"jq", "oniguruma"}, Variants: v, Test: true}) {
		if strings.HasSuffix(f.Name, ".0") || strings.HasSuffix(f.Name, ".0.test") {
			continue
		}
		assert.NotContains(t, f.Body, "+ssl", "%s carries a frame that is not its own", f.Name)
	}
	files := argvFiles(verify.Request{Ports: []string{"jq", "oniguruma"}, Variants: v})
	assert.Equal(t, "-d\n-N\ninstall\njq\n-doc\n+ssl\n", fileNamed(t, files, "argv.0").Body)
	assert.Equal(t, "-d\n-N\ninstall\noniguruma\n", fileNamed(t, files, "argv.1").Body)
}

// -s asks about the member and not about the list. A cohort whose
// headline is a re-derivation and whose dependents are untouched must
// build exactly the headline from source: reading the list as a flag
// would drag every member into a source build.
func TestFromSourceIsPerMember(t *testing.T) {
	files := argvFiles(verify.Request{
		Ports:      []string{"jq", "oniguruma"},
		FromSource: []string{"oniguruma"},
	})

	assert.Equal(t, "-d\n-N\ninstall\njq\n", fileNamed(t, files, "argv.0").Body, "the headline was not named")
	assert.Equal(t, "-d\n-N\n-s\ninstall\noniguruma\n", fileNamed(t, files, "argv.1").Body, "the member that was named builds from source")
}

// The link proof is asked for, never assumed, and it asks the
// dependents. The headline is the thing that changed; what a caller
// wants proved is that the members standing on it still bind to what it
// now publishes.
func TestTheLinkProofIsPerDependentAndOnlyWhenAsked(t *testing.T) {
	unasked := argvFiles(verify.Request{Ports: []string{"jq", "oniguruma"}})
	for _, f := range unasked {
		assert.NotContains(t, f.Name, "links.", "nobody asked for a manifest")
	}

	var links []string
	for _, f := range argvFiles(verify.Request{Ports: []string{"jq", "oniguruma", "libfoo"}, Manifest: true}) {
		if strings.HasPrefix(f.Name, "links.") {
			links = append(links, f.Dest()+" => "+strings.ReplaceAll(f.Body, "\n", "|"))
		}
	}
	assert.Equal(t, []string{
		"/tmp/dockhand-verify/links.1 => -q|contents|oniguruma|",
		"/tmp/dockhand-verify/links.2 => -q|contents|libfoo|",
	}, links, "the dependents are asked and the headline is not")
}

// A file's name is the one part of this protocol that reaches guest
// shell syntax: launch writes each one with `cat > <dest>`. Names are
// therefore built from a member's position and never from its name, so
// a port called anything at all lands in a path a shell reads as one
// word.
func TestGuestPathsNeverCarryAPortName(t *testing.T) {
	req := verify.Request{
		Ports:    []string{"jq", "; rm -rf /", "$(whoami)", "a b*c"},
		Test:     true,
		Manifest: true,
	}
	for _, f := range argvFiles(req) {
		assert.Equal(t, stateDir+"/"+f.Name, f.Dest())
		for _, ch := range " ;$()*&|<>\"'`\n\\" {
			assert.NotContains(t, f.Dest(), string(ch),
				"%q would be shell syntax in `cat > %s`", ch, f.Dest())
		}
	}
}

// stubGuest is a scratch state directory with a stub port(1) and a stub
// sudo on PATH: enough of a guest to run a runner script for real.
//
// Running the script is the only way to prove what it does. The bytes
// can be read, but the control flow — which files are consumed in which
// order, what a failure stops, which state files come to exist — is a
// property of /bin/sh, and asserting it against a shell that actually
// ran it is the difference between a review and a test.
type stubGuest struct {
	dir     string
	portCmd string
	env     []string
	t       *testing.T
}

func newStubGuest(t *testing.T, failOn string) *stubGuest {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	calls := filepath.Join(dir, "calls")

	// sudo -n drops its own flag and runs the rest, so the script's
	// privilege drop is exercised rather than skipped.
	require.NoError(t, os.WriteFile(filepath.Join(bin, "sudo"),
		[]byte("#!/bin/sh\n[ \"$1\" = -n ] && shift\nexec \"$@\"\n"), 0o755))

	// The stub port records its argv and answers with a line naming it,
	// so the log says which invocation produced which output. It fails
	// for exactly one token, which is how a co-member's failure is
	// staged.
	port := filepath.Join(bin, "port")
	require.NoError(t, os.WriteFile(port, []byte(fmt.Sprintf(
		"#!/bin/sh\n"+
			"printf 'port %%s\\n' \"$*\" >> %q\n"+
			"printf 'port %%s\\n' \"$*\"\n"+
			"case \" $* \" in *\" $DOCKHAND_STUB_FAIL \"*) printf 'Error: stub refused\\n'; exit 1;; esac\n"+
			"exit 0\n", calls)), 0o755))

	if failOn == "" {
		failOn = "__nothing__"
	}
	state := filepath.Join(dir, "state")
	require.NoError(t, os.MkdirAll(state, 0o755))
	return &stubGuest{
		dir:     state,
		portCmd: port,
		env:     append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "DOCKHAND_STUB_FAIL="+failOn),
		t:       t,
	}
}

// write puts an instruction file where launch would have put it.
func (g *stubGuest) write(files []argvFile) {
	g.t.Helper()
	for _, f := range files {
		require.NoError(g.t, os.WriteFile(filepath.Join(g.dir, f.Name), []byte(f.Body), 0o644))
	}
}

// run executes the script and waits for the guest to record a terminal
// state, the way Poll waits.
func (g *stubGuest) run(script string) {
	g.t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = g.env
	out, err := cmd.CombinedOutput()
	require.NoError(g.t, err, "the launch itself failed: %s", out)

	deadline := time.Now().Add(20 * time.Second)
	for {
		if s := strings.TrimSpace(g.read("state")); s == "passed" || s == "failed" {
			return
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("the runner never settled; state=%q log=%q", g.read("state"), g.read("log"))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// read is a file the guest wrote, or "" for one it did not.
func (g *stubGuest) read(name string) string {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(g.dir, name))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(g.t, err)
	return string(b)
}

// calls is every port(1) invocation the script made, in order.
func (g *stubGuest) calls() []string {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(g.dir), "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(g.t, err)
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// The single-subject runner consumes lint, then test, then install —
// an order that is its own and deliberately not the order launch writes
// them in. Run for real, because nothing had ever run it.
func TestTheFrozenRunnerConsumesLintThenTestThenInstall(t *testing.T) {
	g := newStubGuest(t, "")
	g.write(argvFiles(verify.Request{Ports: []string{"jq"}, Test: true}))

	g.run(runnerAt(g.dir, g.portCmd))

	assert.Equal(t, []string{
		"port lint jq",
		"port -d -N -k test jq",
		"port -d -N install jq",
	}, g.calls())
	assert.Equal(t, "passed\n", g.read("state"))
	assert.Empty(t, g.read("state.0"), "one subject writes one state file, and it is not indexed")
	assert.NotContains(t, g.read("log"), "dockhand subject:", "one subject leaves no marker")
}

// A failing invocation stops the run there, and the state file says so.
// The log ends at the failing command, which is why a reader can take
// its last error as the one that mattered.
func TestTheFrozenRunnerStopsAtTheFirstFailure(t *testing.T) {
	g := newStubGuest(t, "lint")
	g.write(argvFiles(verify.Request{Ports: []string{"jq"}, Test: true}))

	g.run(runnerAt(g.dir, g.portCmd))

	assert.Equal(t, []string{"port lint jq"}, g.calls(), "nothing ran after the failure")
	assert.Equal(t, "failed\n", g.read("state"))
	assert.Contains(t, g.read("log"), "Error: stub refused")
}

// A clean cohort: every member linted and installed in the order it was
// written, each announced by its marker, each leaving its own state
// file, and one aggregate state for the job Poll reads.
func TestTheCohortRunnerBuildsEveryMemberInOrder(t *testing.T) {
	g := newStubGuest(t, "")
	req := verify.Request{Ports: []string{"jq", "oniguruma"}}
	g.write(argvFiles(req))

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{
		"port lint jq",
		"port -d -N install jq",
		"port lint oniguruma",
		"port -d -N install oniguruma",
	}, g.calls())
	assert.Equal(t, "passed\n", g.read("state"))
	assert.Equal(t, "passed\n", g.read("state.0"))
	assert.Equal(t, "passed\n", g.read("state.1"))

	subjects := verify.SplitSubjects(g.read("log"))
	assert.Contains(t, subjects["jq"], "port -d -N install jq")
	assert.Contains(t, subjects["oniguruma"], "port -d -N install oniguruma")
	assert.NotContains(t, subjects["jq"], "oniguruma", "a member's section is its own output")
	assert.NotContains(t, subjects, "", "a cohort log opens with a marker and has no prologue")
}

// A member's failure does not stop the cohort. A member behind it that
// does not depend on it is built and passes on its own, the failure is
// the aggregate's answer, and each member's state file says what
// happened to that member and nothing else.
//
// Run against the real script, because the control flow is the claim:
// which members are attempted, which are skipped, and what each leaves
// behind is a property of /bin/sh reading these files, and only a shell
// that ran it can say.
func TestACohortGoesOnPastAMemberThatFailedWhenNothingDependsOnIt(t *testing.T) {
	g := newStubGuest(t, "jq")
	req := verify.Request{Ports: []string{"jq", "mise"}}
	g.write(argvFiles(req))

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{
		"port lint jq",
		"port lint mise",
		"port -d -N install mise",
	}, g.calls(), "the independent member behind the failure is built in full")
	assert.Equal(t, "failed\n", g.read("state"), "one failure fails the job")
	assert.Equal(t, "failed\n", g.read("state.0"))
	assert.Equal(t, "passed\n", g.read("state.1"))

	subjects := verify.SplitSubjects(g.read("log"))
	assert.Contains(t, subjects["jq"], "Error: stub refused")
	assert.Contains(t, subjects["mise"], "port -d -N install mise", "announced and built, after the failure")
	assert.Equal(t, []string{"jq", "mise"}, verify.SubjectOrder(g.read("log")))
}

// A member that depends on the one that failed is skipped: its state
// file says so and names the prerequisite's position, and it prints
// nothing into the log, because nothing was run for it. That silence
// is expected rather than a fault, and the state file is what makes it
// readable as such.
func TestACohortSkipsAMemberWhosePrerequisiteFailed(t *testing.T) {
	g := newStubGuest(t, "oniguruma")
	req := verify.Request{Ports: []string{"oniguruma", "jq"}, Requires: [][]string{nil, {"oniguruma"}}}
	g.write(argvFiles(req))

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{"port lint oniguruma"}, g.calls(), "nothing ran for the skipped member")
	assert.Equal(t, "failed\n", g.read("state"))
	assert.Equal(t, "failed\n", g.read("state.0"))
	assert.Equal(t, "skipped\n0\n", g.read("state.1"), "skipped, and for whom")
	assert.NotContains(t, verify.SplitSubjects(g.read("log")), "jq", "a skipped member announces nothing")
}

// A skip propagates down a chain. C depends on B, B on A, and A fails:
// B is skipped for A, and C is skipped for B — each names its own
// prerequisite, the one it actually declares, so the judge can say of
// each member exactly what stood in its way.
func TestASkipPropagatesDownAChainOfPrerequisites(t *testing.T) {
	g := newStubGuest(t, "liba")
	req := verify.Request{
		Ports:    []string{"liba", "libb", "libc"},
		Requires: [][]string{nil, {"liba"}, {"libb"}},
	}
	g.write(argvFiles(req))

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{"port lint liba"}, g.calls())
	assert.Equal(t, "failed\n", g.read("state"))
	assert.Equal(t, "failed\n", g.read("state.0"))
	assert.Equal(t, "skipped\n0\n", g.read("state.1"), "skipped for the member that failed")
	assert.Equal(t, "skipped\n1\n", g.read("state.2"), "skipped for the member that was skipped")
	assert.Equal(t, []string{"liba"}, verify.SubjectOrder(g.read("log")), "only the member that ran is announced")
}

// A failure in the middle of the cohort takes down exactly what depends
// on it. The headline passed before it, a sibling that needs only the
// headline is built after it and passes, and the member that needs the
// one that broke is skipped for it — four members, four different
// answers, from one loop that never stopped.
func TestAMiddleFailureTakesDownOnlyItsDependents(t *testing.T) {
	g := newStubGuest(t, "bar")
	req := verify.Request{
		Ports:    []string{"libfoo", "bar", "baz", "qux"},
		Requires: [][]string{nil, {"libfoo"}, {"libfoo"}, {"libfoo", "bar"}},
	}
	g.write(argvFiles(req))

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{
		"port lint libfoo",
		"port -d -N install libfoo",
		"port lint bar",
		"port lint baz",
		"port -d -N install baz",
	}, g.calls(), "libfoo and baz in full, bar until it broke, qux not at all")
	assert.Equal(t, "failed\n", g.read("state"))
	assert.Equal(t, "passed\n", g.read("state.0"))
	assert.Equal(t, "failed\n", g.read("state.1"))
	assert.Equal(t, "passed\n", g.read("state.2"))
	assert.Equal(t, "skipped\n1\n", g.read("state.3"), "skipped for bar, not for libfoo, which passed")
	assert.Equal(t, []string{"libfoo", "bar", "baz"}, verify.SubjectOrder(g.read("log")))

	// And the record reads back as the provider will read it: one
	// entry per member, in build order, the skip's position translated
	// into the port it names.
	out, err := exec.Command("/bin/sh", "-c", memberStatesScript(g.dir)).CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, []verify.MemberState{
		{Port: "libfoo", Outcome: verify.MemberPassed},
		{Port: "bar", Outcome: verify.MemberFailed},
		{Port: "baz", Outcome: verify.MemberPassed},
		{Port: "qux", Outcome: verify.MemberSkipped, Prerequisite: "bar"},
	}, memberStatesOf(string(out)))
}

// Only a prerequisite that has already failed or been skipped blocks a
// member. One that passed does not; one the runner has not reached yet
// — a graph whose order is not the build's — has no state file and does
// not either; and a line that is not a position is not a file name the
// runner will open.
func TestOnlyAFailedOrSkippedPrerequisiteBlocksAMember(t *testing.T) {
	g := newStubGuest(t, "")
	req := verify.Request{Ports: []string{"jq", "oniguruma"}, Requires: [][]string{{"oniguruma"}, {"jq"}}}
	g.write(argvFiles(req))
	// A line the runner must refuse rather than read as a path: it
	// names no position, and the file it would name is not the runner's.
	g.write([]argvFile{{Name: "requires.1", Body: "0\n../state\n\nx\n"}})

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{
		"port lint jq",
		"port -d -N install jq",
		"port lint oniguruma",
		"port -d -N install oniguruma",
	}, g.calls(), "a prerequisite that passed, or is still ahead, blocks nobody")
	assert.Equal(t, "passed\n", g.read("state"))
	assert.Equal(t, "passed\n", g.read("state.0"))
	assert.Equal(t, "passed\n", g.read("state.1"))
}

// The link proof runs against the dependents once they have installed,
// and never against the headline. It is asked through the same argv-file
// transport as everything else, so the port name it carries is data.
func TestTheCohortRunnerProvesTheDependentsLinks(t *testing.T) {
	g := newStubGuest(t, "")
	req := verify.Request{Ports: []string{"jq", "oniguruma"}, Manifest: true}
	g.write(argvFiles(req))

	g.run(cohortRunnerAt(g.dir, g.portCmd, len(req.Ports)))

	assert.Equal(t, []string{
		"port lint jq",
		"port -d -N install jq",
		"port lint oniguruma",
		"port -d -N install oniguruma",
		"port -q contents oniguruma",
	}, g.calls(), "the dependent is asked what it laid down, after it laid it down")
	assert.Equal(t, "passed\n", g.read("state"))
}

// A cohort's state files are written by rename, not by truncation.
// `echo x > f` is truncate-then-write, and a reader landing in that
// window sees an empty file — which is how this protocol spells "the
// runner never started". The temp names must not survive, or a later
// reader would find two answers.
func TestCohortStateFilesLeaveNoTornWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failOn string
		req    verify.Request
	}{
		{"every member passes", "", verify.Request{Ports: []string{"jq", "oniguruma"}}},
		// A skipped member's file is written by the same rename: the
		// window between truncation and write would otherwise read as a
		// runner that never started, for the one member that was never
		// started on purpose.
		{"a member is skipped", "oniguruma",
			verify.Request{Ports: []string{"oniguruma", "jq"}, Requires: [][]string{nil, {"oniguruma"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newStubGuest(t, tc.failOn)
			g.write(argvFiles(tc.req))

			g.run(cohortRunnerAt(g.dir, g.portCmd, len(tc.req.Ports)))

			entries, err := os.ReadDir(g.dir)
			require.NoError(t, err)
			for _, e := range entries {
				assert.NotContains(t, e.Name(), ".state", "%s is a rename that did not complete", e.Name())
			}
			for i := range tc.req.Ports {
				assert.NotEmpty(t, g.read(fmt.Sprintf("state.%d", i)), "every member has a state file once the job is over")
			}
		})
	}
}

// stubTartIO stands in for tart and records both halves of every call:
// the argv, and whatever was piped in on stdin. A transcript of argv
// alone would miss the bytes that matter most, because the instruction
// files reach the guest on stdin and never as arguments.
func stubTartIO(t *testing.T) (*tool.Finder, func() string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "transcript")
	bin := filepath.Join(dir, "tart")
	script := fmt.Sprintf(`#!/bin/sh
{
  printf 'ARGV %%s\n' "$*"
  if [ ! -t 0 ]; then
    in=$(cat)
    if [ -n "$in" ]; then printf 'STDIN<<\n%%s\nSTDIN>>\n' "$in"; fi
  fi
} >> %q
exit 0
`, log)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))

	finder := tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Tart) {
			return bin, nil
		}
		return "", fmt.Errorf("this test resolves only tart, not %s", name)
	})
	return finder, func() string {
		b, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return ""
		}
		require.NoError(t, err)
		return string(b)
	}
}

// A one-port request that ALSO asks for a manifest hands the guest the
// same three calls and the same frozen runner. What the manifest costs
// is paid before the launch — a roster, a baseline record, a capture,
// all written by the submit — and none of it reaches here.
//
// The distinction matters because the acceptance test's first
// verification is exactly this shape: one port, a manifest, and a build
// that must be the build every one-port verification has always been.
func TestAManifestDoesNotChangeWhatTheGuestIsAskedToBuild(t *testing.T) {
	tools, transcript := stubTartIO(t)
	p := Provider{Tools: tools}

	require.NoError(t, p.launch(t.Context(), "dockhand-worker-cafe",
		verify.Request{Ports: []string{"jq"}, Manifest: true}))

	assert.Equal(t, soloTranscript, transcript())
}
