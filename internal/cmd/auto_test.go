package cmd

// The auto-mode ruling, held here: the invoker is declared and never
// detected, the declaration is one run's answer rather than each verb's,
// and promote is a person's verb.

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// resolvedInvoker runs the tree far enough to have resolved the run's
// invoker and hands back what it resolved.
//
// It executes a probe verb rather than a real one because the answer is
// settled in PersistentPreRunE, before any Action runs — which is the
// property being checked. The probe touches no repository, so what the
// test sees is the resolution and nothing downstream of it.
func resolvedInvoker(t *testing.T, args ...string) record.Driver {
	t.Helper()
	root, rc := newRoot("test")
	root.AddCommand(&cobra.Command{
		Use:  "probe",
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error { return nil },
	})
	root.SetArgs(append(args, "probe"))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())
	return rc.Invoker
}

// The ruling: auto mode is declared and never detected. There are three
// declarations and one default, and every one of them is something the
// invocation says out loud.
func TestAutoModeIsDeclaredAndNeverDetected(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string // "" leaves DOCKHAND_AUTO unset
		args []string
		want record.Driver
	}{
		{name: "nothing declared is a person", want: record.Human},
		{name: "--auto declares the machine", args: []string{"--auto"}, want: record.Machine},
		{name: "--auto=true is the same declaration", args: []string{"--auto=true"}, want: record.Machine},
		{name: "the environment declares it too", env: "1", want: record.Machine},
		{name: "and can spell it as a word", env: "true", want: record.Machine},
		{name: "an environment that says no is no", env: "0", want: record.Human},
		{name: "an empty environment declares nothing", env: "", want: record.Human},
		// The command line is the nearer declaration, so it withdraws a
		// standing one: a maintainer on a machine whose launchd plist
		// exports DOCKHAND_AUTO can still run a verb as themselves.
		{name: "--auto=false withdraws the environment's", env: "1",
			args: []string{"--auto=false"}, want: record.Human},
		{name: "--auto overrides an environment that said no", env: "0",
			args: []string{"--auto"}, want: record.Machine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(autoEnv, tc.env)
			} else {
				t.Setenv(autoEnv, "")
			}
			assert.Equal(t, tc.want, resolvedInvoker(t, tc.args...))
		})
	}
}

// The `auto` verb IS the declaration: it is the unattended entrypoint,
// and nothing else it could be told changes that. The invoker is
// resolved before the pass runs, which is why a run that cannot open a
// repository still answers the question.
func TestTheAutoVerbDeclaresItself(t *testing.T) {
	t.Setenv(autoEnv, "")
	root, rc := newRoot("test")
	root.SetArgs([]string{"auto", "-t", t.TempDir()})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	// The tempdir is not a checkout, so the pass fails; the declaration
	// was made before it tried.
	require.Error(t, root.Execute())
	assert.Equal(t, record.Machine, rc.Invoker)
}

// A value that is neither true nor false is the operator's typo, and
// guessing which they meant is how a machine ends up with a person's
// authority. It is a usage error, said in the environment's own words.
func TestAnUnreadableAutoDeclarationIsAUsageError(t *testing.T) {
	t.Setenv(autoEnv, "sometimes")
	err := run(t, "doctor")
	require.Error(t, err)
	assert.Equal(t, exitcode.Usage, ExitCode(err))
	assert.Contains(t, err.Error(), autoEnv)
}

// Nothing on the road that resolves the invoker, or that carries the
// answer onward, asks the kernel whether a terminal is attached.
// IsTerminal is exported, it is one import away, and it is the intuitive
// answer to "is a person running this" — so the prohibition is checked
// rather than stated.
//
// The scan covers internal/engine as well as the two packages that
// resolve the invoker, because that is where the declaration BECOMES the
// record's provenance: Policy.Invoker arrives there and Policy.askedBy
// turns it into what every mint writes down. A detection inserted into
// askedBy would decide asked_by for every change in the namespace, and
// while this walk stopped at internal/cmd nothing in the tree would have
// said so.
//
// The one allowed hit is named and counted rather than excused by file:
// engine/run.go's diffFromPlan asks whether stdout is a terminal to
// decide whether to page a diff, which is a rendering question and no
// part of deciding who asked for anything. A SECOND read in that same
// file fails here, which is the property an excused file would not have.
//
// Two exclusions, both narrowing the scan to what ships. Comment lines
// are skipped because the prohibition has to be able to name what it
// forbids — auto.go's doc comment does, and so does this one — while no
// line that compiles may. Test files are skipped for the same reason
// one line down: this file's own pattern is the prohibition spelled
// out, and no _test.go in these packages is in the binary.
func TestNoInvokerPathDetectsATerminal(t *testing.T) {
	// One alternation, so a failure names the token it found.
	detection := regexp.MustCompile(`IsTerminal|isatty|ModeCharDevice|os\.Std(in|out|err)\.Stat\(`)
	allowed := map[string]int{filepath.Join("..", "..", "internal/engine", "run.go"): 1}
	for _, pkg := range []string{"internal/cmd", "internal/runstate", "internal/engine"} {
		dir := filepath.Join("..", "..", pkg)
		require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			found := 0
			for i, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") || !detection.MatchString(line) {
					continue
				}
				found++
				assert.LessOrEqual(t, found, allowed[path],
					"%s:%d: auto mode is declared, never detected", path, i+1)
			}
			assert.Equal(t, allowed[path], found,
				"%s: this file's terminal reads are pinned by count", path)
			return nil
		}))
	}
}

// The whole chain, through the command tree the user types into: a
// declaration on the command line becomes the provenance on the record
// the mint writes. Every link between them — the persistent flag, the
// run's resolved invoker, the realization policy, the note — is
// exercised by running the verb.
//
// --no-verify because provenance is not a verdict, and what is recorded
// here must not depend on whether the machine running it can boot a VM.
func TestAnAutoMintRecordsWhoAskedForIt(t *testing.T) {
	testenv.PortTclsh(t)
	t.Setenv(agentEnv, "claude-code")
	portdir := goldenPortRepo(t)
	tr := captureExecute(t, "bump", "--auto", "--to", "2.0", "--no-verify", portdir)
	require.Equal(t, 0, tr.exit, tr.render())

	repo, err := git.Open(t.Context(), testFinder(), portdir)
	require.NoError(t, err)
	tip, err := repo.RevParse(t.Context(), "dockhand/bumpee-2.0")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(t.Context(), tip)
	require.NoError(t, err)
	assert.Equal(t, record.Machine, n.AskedBy)
	assert.Equal(t, "claude-code", n.Agent, "the marker names which agent was driving")
}

// And the same verb typed by a person records a person, with the marker
// still carried: the agent marker says who was at the keyboard's other
// end, never that the keyboard was unattended.
func TestAPersonsMintRecordsAPerson(t *testing.T) {
	testenv.PortTclsh(t)
	t.Setenv(autoEnv, "")
	t.Setenv(agentEnv, "claude-code")
	portdir := goldenPortRepo(t)
	tr := captureExecute(t, "bump", "--to", "2.0", "--no-verify", portdir)
	require.Equal(t, 0, tr.exit, tr.render())

	repo, err := git.Open(t.Context(), testFinder(), portdir)
	require.NoError(t, err)
	tip, err := repo.RevParse(t.Context(), "dockhand/bumpee-2.0")
	require.NoError(t, err)
	n, err := ledger.Open(repo).Read(t.Context(), tip)
	require.NoError(t, err)
	assert.Equal(t, record.Human, n.AskedBy)
	assert.Equal(t, "claude-code", n.Agent)
}

// promote is a person's verb. There is one machine publish path — the
// reconciler's slot — and a promote that published as the machine would
// be a second one, reachable by adding a flag.
func TestPromoteRefusesInAutoMode(t *testing.T) {
	rs := &runstate.Context{Invoker: record.Machine, Out: io.Discard, Err: io.Discard}
	err := promoteAction{target: "dockhand/jq-1.8"}.Execute(t.Context(), rs)

	var refusal *PromoteIsHumanError
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, exitcode.MachineGate, ExitCode(err))
	assert.Equal(t, "promote-is-human", exitcode.TwinOf(err).Reason)
	assert.Contains(t, err.Error(), "dockhand auto")
}

// The refusal comes before the repository: the run above has no Repo
// seam wired at all, so reaching one would have panicked rather than
// refused. Said as its own test because the ordering is the point — a
// refusal about who is asking must not depend on where they are
// standing.
func TestPromoteRefusesBeforeItOpensAnything(t *testing.T) {
	rs := &runstate.Context{Invoker: record.Machine, Out: io.Discard, Err: io.Discard}
	require.NotPanics(t, func() {
		_ = promoteAction{target: "dockhand/jq-1.8"}.Execute(t.Context(), rs)
	})
}

// THE SANCTIONED MACHINE PUBLISH ROAD HAS AN ENTRANCE, and this is it.
//
// `auto` is the one verb that hands the reconciler a publish slot;
// `status` and `clean` pass none and must not. Without this wiring the
// slot, its per-pass cap, its pacing and its 62-while-pending exit were
// reachable only from the engine's own tests, and flipping
// machinePublishEnabled would have changed no binary behaviour at all —
// which is the second change the constant was supposed not to need.
//
// What the pass says on this build is the refusal, once, on stderr:
// asked for the whole pass rather than per branch, because on a build
// with the road closed there is nothing branch-specific to say.
func TestAutoHandsTheReconcilerAPublishSlotAndIsRefused(t *testing.T) {
	repo, _ := lifecycleRepo(t)
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(),
		Invoker: record.Machine, MachinePublish: machinePublishEnabled,
		Out: &out, Err: &errb}

	// A closed publish road is not a broken pass: the reconciliation
	// succeeds and reports every branch.
	require.NoError(t, autoAction{}.Execute(t.Context(), rs))
	assert.Contains(t, out.String(), "dockhand/jq-1.8", "the pass still reports")
	assert.Contains(t, errb.String(), "the machine publish road is disabled at build time",
		"and says once that the road it was asked to walk is closed")
	assert.Equal(t, 1, strings.Count(errb.String(), "disabled at build time"),
		"once for the pass, not once per branch")
}

// The refusal is stated, not exited on. A cron entry that returned
// non-zero every ten minutes because a road it was never asked to walk
// is closed would read as a broken machine in every log that watched it;
// what DOES deserve the exit is unfinished work, which is 62.
func TestAutoExitsZeroOverAClosedPublishRoad(t *testing.T) {
	repo, _ := lifecycleRepo(t)
	var out, errb bytes.Buffer
	rs := &runstate.Context{TreeRoot: repo.Root, Tools: testFinder(),
		Invoker: record.Machine, MachinePublish: machinePublishEnabled,
		Out: &out, Err: &errb}

	err := autoAction{}.Execute(t.Context(), rs)
	require.NoError(t, err)
	assert.Equal(t, exitcode.OK, ExitCode(err))
}

// And the slot is handed in by `auto` alone. status and clean run the
// same pass and neither may open a pull request as a side effect of
// being run, so a Publish literal appearing in either of their opts is
// the ruling broken.
func TestOnlyAutoHandsInAPublishSlot(t *testing.T) {
	for _, f := range []struct{ file, verb string }{
		{"status.go", "status"},
		{"clean.go", "clean"},
	} {
		src, err := os.ReadFile(f.file)
		require.NoError(t, err)
		assert.NotContains(t, string(src), "Publish:",
			"%s runs the same pass and must not publish as a side effect of being run", f.verb)
	}
	src, err := os.ReadFile("auto.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "Publish: slot",
		"the unattended entrypoint is where the slot is handed in")
	assert.Contains(t, string(src), "return slot.Outcome()",
		"and where the pass's own exit comes from")
}

// A person's promote is untouched by any of this: the invoker is the
// only thing the precondition reads, and the human road runs on to the
// repository it always did — here, to fail on a directory that is not
// one, which is proof it got past the verb and into the work.
func TestPromoteStillRunsForAPerson(t *testing.T) {
	rs := &runstate.Context{Invoker: record.Human, TreeRoot: t.TempDir(),
		Tools: testFinder(), Out: io.Discard, Err: io.Discard}
	err := promoteAction{target: "dockhand/jq-1.8"}.Execute(t.Context(), rs)
	require.Error(t, err, "the tempdir is not a checkout")

	var refusal *PromoteIsHumanError
	assert.NotErrorAs(t, err, &refusal, "and the refusal is not what stopped it")
	assert.NotEqual(t, exitcode.MachineGate, ExitCode(err))
}
