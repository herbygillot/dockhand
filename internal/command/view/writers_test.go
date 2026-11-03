package view

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// A plan names the subport a branch changes, its siblings on a line of
// their own, built as MacPorts' CI builds every subport of a changed
// Portfile, and an Excluded line a reason and set of releases, its ports
// named together, where terraform-1.16's said "Changed 19 ports" and 25
// Excluded lines (field testing's terraform replay, 2026-10-22).
func TestAPlanNamesTheChangedSubportAndItsSiblings(t *testing.T) {
	t.Parallel()
	var targets []model.PlanTarget
	siblings := map[model.TargetID]bool{}
	var names []string
	for _, name := range []string{"terraform-1.16", "terraform_select", "terraform-1.0", "terraform-1.1", "terraform-1.2", "terraform-1.3"} {
		targets = append(targets, model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Directory: "sysutils/terraform", Kind: model.Substantive, Role: model.Changed})
		if name != "terraform-1.16" {
			siblings[model.TargetID(name)] = true
			names = append(names, name)
		}
	}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	minmax := "minmax builds each port on its oldest and newest releases, macOS 12 and 27"
	var excluded []model.Exclusion
	for _, name := range names {
		excluded = append(excluded, model.Exclusion{Target: model.Target{Name: name}, Reason: minmax, Span: true})
	}
	plan := model.Plan{Targets: targets, Environments: []model.Environment{sequoia, tahoe}, Tests: model.TestsDeclared,
		Builds: []model.EnvironmentPlan{
			{Environment: sequoia, Exclusions: excluded, Order: []model.TargetID{"terraform-1.16"}},
			{Environment: tahoe, Exclusions: excluded, Order: []model.TargetID{"terraform-1.16"}},
		}}
	var out bytes.Buffer
	WritePlan(&out, plan, nil, siblings, nil, listShown)
	require.Contains(t, out.String(), "Changed     terraform-1.16\n")
	require.Contains(t, out.String(), "Siblings    terraform_select, terraform-1.0, terraform-1.1, terraform-1.2, terraform-1.3 (MacPorts CI builds every subport of a changed Portfile; use --only to narrow the check)\n")
	require.Contains(t, out.String(), fmt.Sprintf("Excluded    terraform_select, terraform-1.0, terraform-1.1, terraform-1.2, terraform-1.3: %s\n", minmax))

	out.Reset()
	WritePlan(&out, plan, nil, nil, nil, listShown)
	require.Contains(t, out.String(), "Changed     terraform-1.16, terraform_select, terraform-1.0, terraform-1.1, terraform-1.2, terraform-1.3\n", "with no record, as before")
	require.NotContains(t, out.String(), "Siblings")
}

// A link finding is a notice, its mark !, and one that couldn't be read
// says nothing of them, unmarked, as one whose links are each declared.
func TestWriteLinks(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteLinks(&out, []engine.LinkFinding{
		{Target: "harbor", Port: "oniguruma", Library: "/opt/local/lib/libonig.5.dylib", Binary: "/opt/local/bin/harbor", Libraries: 2},
		{Target: "harbor-cli", Unread: true},
		{Target: "crystal", Clean: true, Ports: []string{"boehmgc", "pcre2"}},
		{Target: "pcre2", Clean: true, Ports: []string{"zlib"}},
		{Target: "jq", Clean: true},
	})
	require.Equal(t, "Links\n  ! harbor links libonig.5.dylib from oniguruma (and 1 more of its libraries), which it doesn't declare in depends_lib; depends_lib-append port:oniguruma declares it\n"+
		"    couldn't read what harbor-cli's build links, so whether it declares them isn't said\n"+
		"    crystal links boehmgc and pcre2's libraries, each declared in depends_lib\n"+
		"    pcre2 links zlib's libraries, declared in depends_lib\n"+
		"    jq links no other port's libraries\n", out.String())
}

// A grid with a cell an earlier check gave says once, under it, that the
// cell stands, since such a cell fills only where nothing its build read
// has changed; one without such cells says nothing (the exiv2 session's
// notes, 2026-10-24).
func TestTheGridSaysAnEarlierChecksCellStands(t *testing.T) {
	t.Parallel()
	twelve := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	plan := model.Plan{Environments: []model.Environment{twelve, tahoe}, Builds: []model.EnvironmentPlan{{Environment: twelve}, {Environment: tahoe}}}
	cell := func(environment model.Environment, execution model.ExecutionID) engine.Cell {
		return engine.Cell{Kind: engine.CellRecorded, Environment: environment, TargetResult: model.TargetResult{Target: "exiv2", Outcome: model.OutcomePassed, Execution: execution}}
	}
	evidence := engine.Evidence{Run: model.Run{ID: "r2", Number: 491}, Earlier: []model.Run{{ID: "r1", Number: 490}}, Plan: plan,
		Executions: map[model.ExecutionID]model.GuestExecution{"x1": {ID: "x1", Run: "r1"}, "x2": {ID: "x2", Run: "r2"}},
		Targets:    []engine.TargetEvidence{{Target: model.PlanTarget{ID: "exiv2", Target: model.Target{Name: "exiv2"}}, Passed: true, Outcomes: []engine.Cell{cell(twelve, "x1"), cell(tahoe, "x2")}}}}
	var out bytes.Buffer
	WriteResults(&out, "  ", evidence, false)
	require.Contains(t, out.String(), "(check-490)")
	require.Contains(t, out.String(), "\n  (check-490): result reused from an earlier check; its build inputs have not changed\n")

	evidence.Targets[0].Outcomes[0] = cell(twelve, "x2")
	out.Reset()
	WriteResults(&out, "  ", evidence, false)
	require.NotContains(t, out.String(), "standing since")
}

// Targets whose links couldn't be read for one reason are one line with
// it, a line each where the reasons differ: status said five such lines
// with no reason (the exiv2 session's notes, 2026-10-24).
func TestUnreadLinksAreOneLineAReason(t *testing.T) {
	t.Parallel()
	guest := "the guest couldn't read them, as its log says"
	var findings []engine.LinkFinding
	for _, name := range []string{"exiv2", "gpsbabel", "libheif", "darktable", "digikam"} {
		findings = append(findings, engine.LinkFinding{Target: model.TargetID(name), Unread: true, Reason: guest})
	}
	findings = append(findings, engine.LinkFinding{Target: "bazel", Unread: true, Reason: "its result is check-610's, which didn't read them"})
	var out bytes.Buffer
	WriteLinks(&out, findings)
	require.Equal(t, "Links\n    couldn't read what 5 targets link (exiv2, gpsbabel, libheif, darktable, and 1 more): "+guest+"\n"+
		"    couldn't read what bazel's build links: its result is check-610's, which didn't read them\n", out.String())
}

// A running check's VMs are named under the queue with the Tart home that
// reaches them, which a person's tart list doesn't see (the bazel
// session's notes, 2026-10-24).
func TestTheQueueNamesARunningChecksVMsWithTheirTartHome(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteQueue(&out, []checks.QueuedCheck{
		{Run: model.Run{ID: "r1", Number: 616, State: model.RunRunning}, VMs: []string{"dockhand-check-run-9-tahoe-1"}, TartHome: "~/.dockhand/tart"},
		{Run: model.Run{ID: "r2", Number: 617, State: model.RunQueued}},
	}, true)
	require.Contains(t, out.String(), "  check-616 runs in dockhand-check-run-9-tahoe-1: TART_HOME=~/.dockhand/tart tart exec dockhand-check-run-9-tahoe-1 …\n")
	require.NotContains(t, out.String(), "check-617 runs in")
	require.NotContains(t, out.String(), "nothing is running the queue", "serve leads")
}

// A check that took its slot says, under the queue, when it was queued and
// how long it waited, and logs says it with its finish, where check-822's
// wait for a Tart slot was read from its finish less its took (rc1's
// G13); one queued still, or recorded before admission kept a start, says
// nothing of it.
func TestAChecksWaitForItsSlotIsSaid(t *testing.T) {
	t.Parallel()
	queued := time.Date(2026, 10, 31, 14, 2, 11, 0, time.Local)
	started, finished := queued.Add(3*time.Minute+31*time.Second), queued.Add(18*time.Minute)
	running := model.Run{ID: "r1", Number: 822, State: model.RunRunning, CreatedAt: queued, StartedAt: &started}
	var out bytes.Buffer
	WriteQueue(&out, []checks.QueuedCheck{{Run: running}, {Run: model.Run{ID: "r2", Number: 823, State: model.RunQueued, CreatedAt: queued}}}, true)
	require.Contains(t, out.String(), "\n  check-822 queued 2026-10-31 14:02:11, waited 3m 31s for its slot\n")
	require.NotContains(t, out.String(), "check-823 queued")

	passed := running
	passed.State, passed.FinishedAt = model.RunPassed, &finished
	out.Reset()
	require.NoError(t, WriteLogs(&out, config.Places{}, engine.RunLogs{Run: passed}, false))
	require.Contains(t, out.String(), "check-822 · passed\n  Queued 2026-10-31 14:02:11, waited 3m 31s for its slot, finished 2026-10-31 14:20:11.\n")
	viewed := RunView(passed)
	require.Equal(t, int64(211), *viewed.WaitedSeconds)
	require.Equal(t, started, *viewed.StartedAt)

	old := passed
	old.StartedAt = nil
	out.Reset()
	require.NoError(t, WriteLogs(&out, config.Places{}, engine.RunLogs{Run: old}, false))
	require.NotContains(t, out.String(), "waited")
	require.Nil(t, RunView(old).WaitedSeconds)
}

// A queued check nothing leads says so, and how to run it, where it said
// its state alone (field testing's outdated-ports session's check-660).
func TestAQueuedCheckNothingRunsSaysSo(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	WriteQueue(&out, []checks.QueuedCheck{{Run: model.Run{ID: "r1", Number: 660, State: model.RunQueued}}}, false)
	require.Contains(t, out.String(), "nothing is running the queue; dockhand wait check-660 runs it here, dockhand serve --drain runs the queue, cancel ends it")
}

// A long Changed line names listShown ports and counts the rest; with -v
// it names every one, where only --json gave them (the docs dig's I37).
func TestVerboseNamesAWholeList(t *testing.T) {
	t.Parallel()
	var targets []model.PlanTarget
	var names []string
	for i := range 20 {
		name := fmt.Sprintf("py3%02d-thing", i)
		names = append(names, name)
		targets = append(targets, model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Kind: model.Substantive, Role: model.Changed})
	}
	plan := model.Plan{Targets: targets, Environments: []model.Environment{{Provider: "command"}}, Tests: model.TestsDeclared}
	var out bytes.Buffer
	streams := term.Begin(term.Streams{Out: &out}, nil)
	WritePlan(streams.Out, plan, nil, nil, nil, ListShown(streams))
	require.Contains(t, out.String(), "Changed     20 ports: py300-thing, py301-thing, py302-thing, py303-thing, py304-thing, py305-thing, py306-thing, py307-thing and 12 more\n")

	out.Reset()
	streams.SetVerbosity(1)
	WritePlan(&out, plan, nil, nil, nil, ListShown(streams))
	require.Contains(t, out.String(), "Changed     "+strings.Join(names, ", ")+"\n", "all twenty at -v")
}

// A two-release check's report says how long each target took in each
// release, from the result recorded before it or the run's start, and
// how long the check ran in each; a reused result says no time (the docs
// dig's I38).
func TestTheReportSaysHowLongEachPartTook(t *testing.T) {
	t.Parallel()
	sonoma := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	plan := model.Plan{Environments: []model.Environment{sonoma, tahoe}, Builds: []model.EnvironmentPlan{{Environment: sonoma}, {Environment: tahoe}}}
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	ended := func(d time.Duration) *time.Time { at := start.Add(d); return &at }
	cell := func(environment model.Environment, execution model.ExecutionID, target model.TargetID, at time.Duration) engine.Cell {
		return engine.Cell{Kind: engine.CellRecorded, Environment: environment, TargetResult: model.TargetResult{Target: target, Outcome: model.OutcomePassed, Execution: execution, RecordedAt: start.Add(at)}}
	}
	evidence := engine.Evidence{Run: model.Run{ID: "r1", Number: 12}, Plan: plan,
		Executions: map[model.ExecutionID]model.GuestExecution{"x1": {ID: "x1", Run: "r1", CreatedAt: start, FinishedAt: ended(13 * time.Minute)}, "x2": {ID: "x2", Run: "r1", CreatedAt: start, FinishedAt: ended(9*time.Minute + 41*time.Second)}},
		Targets: []engine.TargetEvidence{
			{Target: model.PlanTarget{ID: "oniguruma", Target: model.Target{Name: "oniguruma"}}, Passed: true, Outcomes: []engine.Cell{cell(sonoma, "x1", "oniguruma", 4*time.Minute+12*time.Second), cell(tahoe, "x2", "oniguruma", 3*time.Minute)}},
			{Target: model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}}, Passed: true, Outcomes: []engine.Cell{cell(sonoma, "x1", "jq", 12*time.Minute), cell(tahoe, "x2", "jq", 9*time.Minute)}},
		}}
	var out bytes.Buffer
	WriteResults(&out, "  ", evidence, false)
	require.Regexp(t, `oniguruma +\S.*\(4m 12s\) +\S.*\(3m 0s\)`, out.String())
	require.Regexp(t, `jq +\S.*\(7m 48s\) +\S.*\(6m 0s\)`, out.String(), "from the result before it")
	require.Contains(t, out.String(), "\n  Took 13m 0s on "+checks.DescribeEnvironment(sonoma)+"; 9m 41s on "+checks.DescribeEnvironment(tahoe)+".\n")

	evidence.Targets[1].Outcomes[1].ReusedFrom = "x0"
	out.Reset()
	WriteResults(&out, "  ", evidence, false)
	require.NotContains(t, out.String(), "(6m 0s)", "a reused result took no time here")
}

// What lint warned of is a line each, a new port's marked ✗, an update's
// a notice, and a new port lint didn't nitpick says so (the docs dig's
// I13).
func TestWriteLint(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	WriteLint(&out, []engine.LintFinding{
		{Target: "harbor", Port: "harbor", Environment: tahoe, Lines: []string{"Line 3 has trailing whitespace"}, Holds: true},
		{Target: "jq", Port: "jq", Environment: tahoe, Lines: []string{"no license set"}},
		{Target: "harbor-cli", Port: "harbor-cli", Environment: model.Environment{Provider: "github"}, Lines: []string{}, NotNitpicked: true},
	})
	on := checks.DescribeEnvironment(tahoe)
	require.Equal(t, "Lint\n  ✗ harbor: Line 3 has trailing whitespace on "+on+"\n  ! jq: no license set on "+on+"\n"+
		"    harbor-cli: a new port, linted without --nitpick on "+checks.DescribeEnvironment(model.Environment{Provider: "github"})+", so its nitpicks weren't read\n", out.String())
	out.Reset()
	WriteLint(&out, nil)
	require.Empty(t, out.String())
}

// A prefix a check is building in says who holds it, as a check waiting
// on it says (Codex's review of 8e314b96, finding 1's note).
func TestAPrefixBeingBuiltInSaysWhoHoldsIt(t *testing.T) {
	t.Parallel()
	require.Equal(t, "check-12 is building in /opt/local, held by check-12 (pid 4711). Your port commands targeting this prefix must wait; upgrades made there in the meantime will be undone when the prefix is restored.",
		ChangeWords(buildenv.Change{Check: "check-12", Place: "/opt/local", Building: true, Holder: "check-12 (pid 4711)"}))
}

// A plan's JSON says of a local prefix other than /opt/local what its text
// says, that its dependencies build from source; /opt/local's says nothing.
func TestALocalPlansJSONCarriesItsPrefixNote(t *testing.T) {
	t.Parallel()
	viewed := PlanView(model.Plan{Environments: []model.Environment{
		{Provider: buildenv.Local, Prefix: "/opt/macports-test"},
		{Provider: buildenv.Local, Prefix: "/opt/local"},
	}})
	require.Equal(t, "/opt/macports-test: MacPorts' binary archives are built for /opt/local alone, so its dependencies build from source", viewed.Environments[0].Note)
	require.Empty(t, viewed.Environments[1].Note)
}

// A plan says a moved port where it came from, and a removed directory as
// no port to build; a GitHub check says the workflow builds nothing for a
// move (field testing's move of dockhand to macports, 2026-10-29).
func TestAPlanSaysAMoveAndARemoval(t *testing.T) {
	t.Parallel()
	plan := model.Plan{Environments: []model.Environment{{Provider: buildenv.GitHub}},
		Targets: []model.PlanTarget{{ID: "dockhand", Target: model.Target{Name: "dockhand"}, Directory: "macports/dockhand", MovedFrom: "sysutils/dockhand", Kind: model.RevisionOnly, Role: model.Changed}},
		Removed: []string{"devel/gone"}}
	var out bytes.Buffer
	WritePlan(&out, plan, nil, nil, nil, 8)
	require.Contains(t, out.String(), "Changed     dockhand (moved from sysutils/dockhand)\n")
	require.Contains(t, out.String(), "Removed     devel/gone, with no port to build\n")
	require.Contains(t, out.String(), "github's workflow builds nothing for a moved port")

	// A rename says the name it had, and the workflow builds its new
	// Portfile (dockhand rename).
	plan.Targets = []model.PlanTarget{{ID: "xgo", Target: model.Target{Name: "xgo"}, Directory: "lang/xgo", MovedFrom: "lang/goplus", Kind: model.Substantive, Role: model.Changed}}
	out.Reset()
	WritePlan(&out, plan, nil, nil, nil, 8)
	require.Contains(t, out.String(), "Changed     xgo (renamed from goplus)\n")
	require.NotContains(t, out.String(), "github's workflow builds nothing")
}

// check --plan -v prints each target's evaluation in each environment,
// a line for what it has, each value as MacPorts writes it, and --json
// carries the same (field testing's curl +sectrust notes, note 8b).
func TestThePlansEvaluationIsPrinted(t *testing.T) {
	t.Parallel()
	tahoe := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	plan := model.Plan{Evaluated: []model.Evaluated{{Target: "curl +gnutls +sectrust", Environment: tahoe, Variants: []string{"+brotli", "+gnutls", "+sectrust"},
		DependsBuild: []string{"port:pkgconfig"}, DependsLib: []string{"port:gnutls", "path:lib/libssl.dylib:openssl"}, ConfigureArgs: "--with-gnutls {--with-ca-path=/opt/local/etc}"}}}
	var out bytes.Buffer
	WriteEvaluated(&out, plan)
	require.Equal(t, "\ncurl +gnutls +sectrust on "+checks.DescribeEnvironment(tahoe)+"\n"+
		"  variants       +brotli +gnutls +sectrust\n"+
		"  depends_build  port:pkgconfig\n"+
		"  depends_lib    port:gnutls path:lib/libssl.dylib:openssl\n"+
		"  configure.args --with-gnutls {--with-ca-path=/opt/local/etc}\n", out.String())
	viewed := PlanView(plan)
	require.Len(t, viewed.Evaluated, 1)
	require.Equal(t, []string{"port:gnutls", "path:lib/libssl.dylib:openssl"}, viewed.Evaluated[0].DependsLib)
	require.Equal(t, []string{}, viewed.Evaluated[0].DependsRun, "empty, never null")
	require.Empty(t, PlanView(model.Plan{}).Evaluated, "a recorded plan has none")
}

// tidy's preview names every trailer a commit keeps, a Co-authored-by:
// beside Closes:, and dockhand's own Generated-By as what it adds (field
// testing's notes from the curl fix, note 12).
func TestTidysPreviewNamesEveryTrailer(t *testing.T) {
	t.Parallel()
	hand := engine.TidyGroup{Directory: "net/curl", Paths: []string{"net/curl/Portfile"}, Message: "curl: fix the build with Secure Transport\n\nThe sectrust variant built without its framework.\n\nCloses: https://trac.macports.org/ticket/74620\nCo-authored-by: Ada <ada@example.com>"}
	var out bytes.Buffer
	WriteTidyPlan(&out, engine.TidyPlan{Groups: []engine.TidyGroup{hand}}, false)
	require.Contains(t, out.String(), "       keeps: Closes: https://trac.macports.org/ticket/74620\n       keeps: Co-authored-by: Ada <ada@example.com>\n")
	require.NotContains(t, out.String(), "adds:", "a hand edit's commit adds nothing")

	dockhands := engine.TidyGroup{Directory: "textproc/jq", Paths: []string{"textproc/jq/Portfile"}, Message: "jq: update to 1.8.1\n\nGenerated-By: Dockhand devel (https://github.com/herbygillot/dockhand)"}
	out.Reset()
	WriteTidyPlan(&out, engine.TidyPlan{Groups: []engine.TidyGroup{dockhands}}, false)
	require.Contains(t, out.String(), "       adds: Generated-By: Dockhand devel (https://github.com/herbygillot/dockhand)\n")
	require.NotContains(t, out.String(), "keeps: Generated-By")
}

// A Next: naming a variant build quotes it for the shell, as status's
// failed extra says "dockhand submit --accept 'curl +sectrust +ssl'"
// (field testing's note 10 from the curl fix).
func TestANextNamingAVariantBuildQuotesIt(t *testing.T) {
	t.Parallel()
	require.Equal(t, "dockhand submit --branch curl-fix --accept 'curl +sectrust +ssl'", RenderNext(branchstate.Next{Verb: branchstate.VerbSubmit, Branch: "curl-fix", Args: []string{"--accept", "curl +sectrust +ssl"}}, ""))
	require.Equal(t, `dockhand decide --branch jq-fix license-changed jq --because "…"`, RenderNext(branchstate.Next{Verb: branchstate.VerbDecide, Branch: "jq-fix", Args: []string{"license-changed", "jq", "--because", `"…"`}}, ""), "an argument quoted already stays as it is")
}
