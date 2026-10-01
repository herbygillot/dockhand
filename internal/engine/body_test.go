package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Tested on states what the environment reported, as MacPorts' template
// has it, and the runs behind its results. Without a report it names the
// release and the tools the environment stated, and a release dockhand
// doesn't know keeps its Darwin version: never a Darwin version read as
// macOS's.
func TestTestedOnSaysWhatTheEnvironmentWas(t *testing.T) {
	tahoe := model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	tart := []model.GuestExecution{{ID: "tart_7y62p4sigena6xlr", Run: "run_eleven", ProviderRef: "dockhand-check-run-x-tahoe-1"}}
	checks := map[model.RunID]string{"run_ten": "check-10", "run_eleven": "check-11"}
	for _, test := range []struct {
		name        string
		environment model.Environment
		observed    model.Observed
		runs        []model.GuestExecution
		want        string
	}{
		{"reported, with Xcode", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", Tools: "26.6.0.0.1781586589"}, tart,
			"macOS 26.6.2 25G71 arm64\nXcode 26.6 17F42 · tart: built in a clean VM (Run ID: tart_7y62p4sigena6xlr - checked in check-11)\n\n"},
		{"reported, with its MacPorts", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", MacPorts: "2.12.6"}, tart,
			"macOS 26.6.2 25G71 arm64\nXcode 26.6 17F42 · MacPorts 2.12.6 · tart: built in a clean VM (Run ID: tart_7y62p4sigena6xlr - checked in check-11)\n\n"},
		{"reported, with the tools", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsCommandLine},
			model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Tools: "26.6.0.0.1781586589"},
			append([]model.GuestExecution{{ID: "tart_b3kq9wz0m1xv4ce7", Run: "run_ten"}}, tart...),
			"macOS 26.6.2 25G71 arm64\nCommand Line Tools 26.6.0.0.1781586589 · tart: built in a clean VM (Run IDs: tart_b3kq9wz0m1xv4ce7 - checked in check-10; tart_7y62p4sigena6xlr - checked in check-11)\n\n"},
		{"not reported", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode}, model.Observed{}, nil,
			"macOS 26 (Tahoe) arm64\nXcode, its version not recorded · tart: built in a clean VM\n\n"},
		{"an unknown release", model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "30", Architecture: "arm64"}}, model.Observed{}, nil,
			"Darwin 30 arm64\nDeveloper tools not recorded · tart: built in a clean VM\n\n"},
		{"partly reported, a run of an unknown check", model.Environment{Provider: "tart", Platform: tahoe, DeveloperTools: model.DeveloperToolsXcode},
			model.Observed{MacOS: "26.6.2", Xcode: "26.6"}, []model.GuestExecution{{ID: "tart_q2w8e4r6t1y3u5i7", Run: "run_gone"}},
			"macOS 26.6.2 arm64\nXcode 26.6 · tart: built in a clean VM (Run ID: tart_q2w8e4r6t1y3u5i7)\n\n"},
		{"nothing known", model.Environment{Provider: "command"}, model.Observed{}, nil,
			"Developer tools not recorded · command: built by the author's own command\n\n"},
		{"a workflow run", model.Environment{Provider: "github"}, model.Observed{},
			[]model.GuestExecution{{ID: "github_q2w8e4r6t1y3u5i7", Run: "run_eleven", ProviderRef: "https://github.com/ada/macports-ports/actions/runs/123"}},
			"Developer tools not recorded · github: MacPorts' CI workflow in the author's fork (Run ID: https://github.com/ada/macports-ports/actions/runs/123 - checked in check-11)\n\n"},
		{"a workflow run, its runners' releases reported", model.Environment{Provider: "github"},
			model.Observed{Builders: []model.BuilderObserved{{Builder: "macos-14", MacOS: "14"}, {Builder: "macos-15", MacOS: "15"}, {Builder: "macos-15-intel", MacOS: "15"}, {Builder: "macos-latest"}}},
			[]model.GuestExecution{{ID: "github_q2w8e4r6t1y3u5i7", Run: "run_eleven", ProviderRef: "https://github.com/ada/macports-ports/actions/runs/123"}},
			"macOS 14, 15\nDeveloper tools not recorded · github: MacPorts' CI workflow in the author's fork (Run ID: https://github.com/ada/macports-ports/actions/runs/123 - checked in check-11)\n\n"},
		{"a workflow run whose runners named no release", model.Environment{Provider: "github"},
			model.Observed{Builders: []model.BuilderObserved{{Builder: "macos-latest"}}}, nil,
			"Developer tools not recorded · github: MacPorts' CI workflow in the author's fork\n\n"},
	} {
		require.Equal(t, test.want, testedOn(test.environment, test.observed, test.runs, checks, nil), test.name)
	}
}

// The description's first line names dockhand, and its last line
// dockhand's version, or dockhand alone when the build doesn't know it.
func TestTheSignatureNeedsNoVersion(t *testing.T) {
	require.Equal(t, "Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**", submittedBy)
	require.Equal(t, "- [dockhand](https://github.com/herbygillot/dockhand) ver. v3.1.0", signature("v3.1.0"))
	require.Equal(t, "- [dockhand](https://github.com/herbygillot/dockhand)", signature(" "))
}

// A description dockhand wrote before its first line named dockhand gains
// that line when submitting again rewrites its last line, which named
// dockhand then. One whose Tested on a person edited keeps its old last
// line, and gains nothing; a first line a person took out stays out.
func TestAnOlderDescriptionGainsItsFirstLine(t *testing.T) {
	old := "#### Description\n\nupdate\n\n###### Tested on\n\nmacOS 26\n\nSubmitted by **[dockhand](https://github.com/herbygillot/dockhand)** (ver. v3.0.0)\n"
	fresh := submittedBy + "\n\n#### Description\n\nupdate\n\n###### Tested on\n\nmacOS 26\n\n" + signature("v3.1.0") + "\n"
	merged, sections := mergeBody(old, old, fresh, false)
	require.Equal(t, fresh, merged)
	require.Equal(t, SectionRefreshed, sections.TestedOn)

	edited := strings.Replace(old, "macOS 26", "macOS 26, and by hand", 1)
	merged, _ = mergeBody(edited, old, fresh, false)
	require.Equal(t, edited, merged, "its old last line stays, so no first line is added")

	removed := strings.TrimPrefix(fresh, submittedBy+"\n\n")
	merged, _ = mergeBody(removed, fresh, fresh, false)
	require.Equal(t, removed, merged, "a first line a person took out stays out")

	introduced := "Why now: a CVE.\n\n" + old
	merged, _ = mergeBody(introduced, old, fresh, false)
	require.True(t, strings.HasPrefix(merged, "Why now: a CVE.\n\n#### Description"), "one a person began otherwise begins as they did")

	// One dockhand began with the plain line, before it was bold, is given
	// the bold one, while it's as dockhand wrote it.
	plain := plainSubmittedBy + "\n\n#### Description\n\nupdate\n\n###### Tested on\n\nmacOS 26\n\n" + signature("v3.1.0") + "\n"
	merged, _ = mergeBody(plain, plain, fresh, false)
	require.Equal(t, fresh, merged)
	merged, _ = mergeBody("Why now.\n\n"+plain, plain, fresh, false)
	require.True(t, strings.HasPrefix(merged, "Why now.\n\n"+plainSubmittedBy+"\n"), "a line a person moved stays theirs")
}

// A column heading is the environment's release alone, with its
// architecture where two share a release, and its provider where the plan
// has several.
func TestEnvironmentHeadingsAreShort(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	intel := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}}
	unknown := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "30", Architecture: "arm64"}}
	github := model.Environment{Provider: "github"}
	require.Equal(t, "macOS 26", EnvironmentHeading(tahoe, []model.Environment{tahoe, sequoia}))
	require.Equal(t, "macOS 15", EnvironmentHeading(sequoia, []model.Environment{tahoe, sequoia}))
	require.Equal(t, "macOS 26 arm64", EnvironmentHeading(tahoe, []model.Environment{tahoe, intel}))
	require.Equal(t, "tart macOS 26", EnvironmentHeading(tahoe, []model.Environment{tahoe, github}))
	require.Equal(t, "github", EnvironmentHeading(github, []model.Environment{tahoe, github}))
	require.Equal(t, "Darwin 30", EnvironmentHeading(unknown, []model.Environment{unknown}))
}

// A log directory names the release as everything else does, macOS 26 as
// macos26, not by its Darwin version, which isn't macOS's.
func TestLogDirectoriesNameTheMacOSRelease(t *testing.T) {
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	require.Equal(t, "tart-macos26-arm64", environmentSlug(tahoe))
	require.Equal(t, "tart-macos15-x86_64", environmentSlug(model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "x86_64"}}))
	require.Equal(t, "tart-darwin30-arm64", environmentSlug(model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "30", Architecture: "arm64"}}))
	require.Equal(t, "tart-darwin30-arm64", environmentSlug(model.Environment{Provider: "tart", Platform: model.Platform{Version: "30", Architecture: "arm64"}}), "no OS is Darwin's")
	require.Equal(t, "command", environmentSlug(model.Environment{Provider: "command"}))
}

// Timed-out tests ran and didn't pass: the checklist doesn't claim the
// existing tests were tried, and the table says they timed out. (The
// architecture review of 2026-09-27, finding 1.)
func TestTimedOutTestsAreNotPassing(t *testing.T) {
	evidence := Evidence{Plan: model.Plan{Environments: []model.Environment{{Provider: "command"}}}, Targets: []TargetEvidence{
		{Target: model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}}, Passed: true, Outcomes: cells([]model.TargetResult{{Outcome: model.OutcomePassed, Tests: model.TestsPassed}})},
		{Target: model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor"}}, Passed: true, Outcomes: cells([]model.TargetResult{{Outcome: model.OutcomePassed, Tests: model.TestsTimedOut}})},
	}}
	body := ownedSections(bodyFacts{Evidence: &evidence})
	require.Contains(t, body, "- [ ] tried existing tests")
	require.Contains(t, body, "| libharbor | ✓ build passed; tests timed out (advisory) |")
}

// An environment's report names the runs that made it: where results came
// from runs that found the environment otherwise, each report is its own,
// with its own runs. From the architecture review of 2026-09-27, which
// found the latest report standing for every run.
func TestEachReportNamesTheRunsThatMadeIt(t *testing.T) {
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	older := model.Observed{MacOS: "26.6.1", Xcode: "26.6", MacPorts: "2.12.5"}
	newer := model.Observed{MacOS: "26.6.2", Xcode: "26.6", MacPorts: "2.12.6"}
	runs := map[model.ExecutionID]model.GuestExecution{
		"tart_a": {ID: "tart_a", Run: "run_ten", Observed: older, CreatedAt: at},
		"tart_b": {ID: "tart_b", Run: "run_eleven", Observed: newer, CreatedAt: at.Add(time.Hour)},
		"tart_c": {ID: "tart_c", Run: "run_eleven", CreatedAt: at.Add(2 * time.Hour)},
	}
	evidence := func(executions ...model.ExecutionID) Evidence {
		e := Evidence{Plan: model.Plan{Environments: []model.Environment{{Provider: "tart"}}}, Executions: map[model.ExecutionID]model.GuestExecution{}}
		for i, id := range executions {
			e.Executions[id] = runs[id]
			e.Targets = append(e.Targets, TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(fmt.Sprint("port", i))},
				Outcomes: cells([]model.TargetResult{{Execution: id, Outcome: model.OutcomePassed}})})
		}
		return e
	}
	observations := evidence("tart_a", "tart_b").Observations(0)
	require.Len(t, observations, 2, "they found the environment otherwise")
	require.Equal(t, older, observations[0].Observed)
	require.Equal(t, model.ExecutionID("tart_a"), observations[0].Runs[0].ID)
	require.Equal(t, newer, observations[1].Observed)

	observations = evidence("tart_b", "tart_c").Observations(0)
	require.Len(t, observations, 1, "a run that said nothing joins the one report")
	require.Equal(t, newer, observations[0].Observed)
	require.Len(t, observations[0].Runs, 2)

	observations = evidence("tart_a", "tart_b", "tart_c").Observations(0)
	require.Len(t, observations, 3, "and stands alone beside two")
	require.Equal(t, model.Observed{}, observations[2].Observed)
	require.Empty(t, evidence().Observations(0))
}

// Reports of an environment's several builders, a workflow's runners, are
// one report where each builder said the same, and two where one said
// otherwise.
func TestBuildersReportsAreOneWhereEachSaidTheSame(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	runners := func(releases ...string) model.Observed {
		var observed model.Observed
		for _, release := range releases {
			observed.Builders = append(observed.Builders, model.BuilderObserved{Builder: "macos-" + release, MacOS: release})
		}
		return observed
	}
	evidence := Evidence{Plan: model.Plan{Environments: []model.Environment{{Provider: "github"}}}, Executions: map[model.ExecutionID]model.GuestExecution{
		"github_a": {ID: "github_a", Run: "run_ten", Observed: runners("14", "15"), CreatedAt: at},
		"github_b": {ID: "github_b", Run: "run_eleven", Observed: runners("14", "15"), CreatedAt: at.Add(time.Hour)},
		"github_c": {ID: "github_c", Run: "run_eleven", Observed: runners("14", "15", "26"), CreatedAt: at.Add(2 * time.Hour)},
	}}
	for i, id := range []model.ExecutionID{"github_a", "github_b", "github_c"} {
		evidence.Targets = append(evidence.Targets, TargetEvidence{Target: model.PlanTarget{ID: model.TargetID(fmt.Sprint("port", i))},
			Outcomes: cells([]model.TargetResult{{Execution: id, Outcome: model.OutcomePassed}})})
	}
	observations := evidence.Observations(0)
	require.Len(t, observations, 2)
	require.Equal(t, runners("14", "15"), observations[0].Observed)
	require.Len(t, observations[0].Runs, 2, "the same runners' report is one")
	require.Equal(t, runners("14", "15", "26"), observations[1].Observed)
	require.False(t, runners("14").IsZero())
	require.True(t, model.Observed{}.IsZero())
}

// An environment where every port is excluded wasn't tested: Tested on
// doesn't name it, and the table still says excluded (the beekeeper-studio
// run's finding 4, where platforms {darwin >= 23} left macOS 12 out, and
// the pull request said it was built there).
func TestAnExcludedEnvironmentIsNotCalledTested(t *testing.T) {
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	target := model.PlanTarget{ID: "beekeeper-studio", Target: model.Target{Name: "beekeeper-studio"}}
	evidence := Evidence{
		Run: model.Run{ID: "run_21", Number: 21},
		Plan: model.Plan{Environments: []model.Environment{monterey, tahoe}, Builds: []model.EnvironmentPlan{
			{Environment: monterey, Exclusions: []model.Exclusion{{Target: target.Target, Reason: "known_fail"}}},
			{Environment: tahoe, Order: []model.TargetID{target.ID}},
		}},
		Targets:    []TargetEvidence{{Target: target, Passed: true, Outcomes: []Cell{noResult(CellExcluded, monterey, target.ID), recorded(tahoe, model.TargetResult{Execution: "tart_b", Outcome: model.OutcomePassed})}}},
		Executions: map[model.ExecutionID]model.GuestExecution{"tart_b": {ID: "tart_b", Run: "run_21", Observed: model.Observed{MacOS: "26.6", Xcode: "26.6"}}},
	}
	require.False(t, evidence.Tested(0))
	require.True(t, evidence.ExcludesAll(0))
	require.True(t, evidence.Tested(1))
	require.False(t, evidence.ExcludesAll(1))

	testedOn, table, found := strings.Cut(ownedSections(bodyFacts{Evidence: &evidence}), "| Port |")
	require.True(t, found)
	require.Equal(t, "###### Tested on\n\nmacOS 26.6 arm64\nXcode 26.6 · tart: built in a clean VM (Run ID: tart_b - checked in check-21)\n\n", testedOn)
	require.Contains(t, table, "| beekeeper-studio | — excluded | ✓ |")
}

// A --variants each check that passed ticks the template's variants item
// by itself, and says which it built; each build is its own row. One
// where a variant build failed leaves the item to the person.
func TestAVariantsCheckAnswersTheVariantsItem(t *testing.T) {
	command := model.Environment{Provider: "command"}
	passed := func(target model.Target) TargetEvidence {
		return TargetEvidence{Target: model.PlanTarget{ID: target.ID(), Target: target}, Passed: true,
			Outcomes: []Cell{recorded(command, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsNone})}}
	}
	jq := model.Target{Name: "jq"}
	tests := model.Target{Name: "jq", Variants: map[string]bool{"tests": true}}
	docs := model.Target{Name: "jq", Variants: map[string]bool{"docs": true}}
	evidence := Evidence{Plan: model.Plan{Environments: []model.Environment{command}, EachVariant: true}, Targets: []TargetEvidence{passed(jq), passed(tests), passed(docs)}}
	port, builds, ok := evidence.VariantsBuilt()
	require.True(t, ok)
	require.Equal(t, "jq", port)
	require.Equal(t, []string{"+tests", "+docs"}, builds)
	body := ownedSections(bodyFacts{Evidence: &evidence})
	require.Contains(t, body, "- [x] checked that the Portfile's most important [variants](https://trac.macports.org/wiki/Variants) haven't been broken? (dockhand built jq with each of +tests, +docs over its defaults)\n")
	require.Contains(t, body, "| jq +tests | ✓ |\n")

	evidence.Targets[2].Passed = false
	_, _, ok = evidence.VariantsBuilt()
	require.False(t, ok, "a variant build that didn't pass answers nothing")
	require.Contains(t, ownedSections(bodyFacts{Evidence: &evidence}), "- [ ] checked that the Portfile's most important [variants]")
	require.Contains(t, ownedSections(bodyFacts{Evidence: &evidence, TestedVariants: true}), "- [x] checked that the Portfile's most important [variants](https://trac.macports.org/wiki/Variants) haven't been broken?\n",
		"the person's statement, as before")
	evidence.Plan.EachVariant = false
	_, _, ok = evidence.VariantsBuilt()
	require.False(t, ok, "only --variants each answers it")
}

// check's --variants is each, or variants as MacPorts' command line takes
// them.
func TestTheVariantsFlag(t *testing.T) {
	variants, each, err := VariantsFlag("+tests -docs")
	require.NoError(t, err)
	require.False(t, each)
	require.Equal(t, map[string]bool{"tests": true, "docs": false}, variants)
	variants, each, err = VariantsFlag(" each ")
	require.NoError(t, err)
	require.True(t, each)
	require.Nil(t, variants)
	variants, each, err = VariantsFlag("")
	require.NoError(t, err)
	require.False(t, each)
	require.Nil(t, variants)
	_, _, err = VariantsFlag("tests")
	require.ErrorContains(t, err, "--variants: ")
}

// A new port is said under Description as its Portfile says it, for a
// reviewer who has never heard of it; its Type(s) stay unticked, since
// MacPorts' automation labels a new Portfile a submission (the txt run's
// finding 4).
func TestANewPortIsSaidInItsDescription(t *testing.T) {
	body := pullRequestBody(bodyFacts{NewPorts: []NewPort{{Name: "txt", Version: "0.8.1", Description: "A fast, intuitive terminal text editor",
		Homepage: "https://txt.hellman.io/", License: "MIT or Apache-2"}}})
	require.Contains(t, body, "#### Description\n\nNew port **txt** 0.8.1: A fast, intuitive terminal text editor\n\n- homepage: https://txt.hellman.io/\n- license: MIT or Apache-2\n\n")
	require.Contains(t, body, "- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n")
}

// A result's reason for not wholly passing is said in a note under the
// table, its cell marked, once for each reason with every port and
// environment that gave it. The rust run, #35084, read "tests failed
// (advisory)" on both releases, and nothing said that its bootstrap had
// panicked before any test ran. A failed build's reason is said the same
// way; a timeout's is its deadline, which its cell says, and a result
// with no reason has no mark.
func TestAResultsReasonIsSaidUnderTheTable(t *testing.T) {
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	failedTests := model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsFailed, Detail: "tests: Failed to test rust: command execution failed"}
	evidence := Evidence{Plan: model.Plan{Environments: []model.Environment{sequoia, tahoe}}, Targets: []TargetEvidence{
		{Target: model.PlanTarget{ID: "rust", Target: model.Target{Name: "rust"}}, Passed: true, Outcomes: []Cell{recorded(sequoia, failedTests), recorded(tahoe, failedTests)}},
		{Target: model.PlanTarget{ID: "cargo", Target: model.Target{Name: "cargo"}}, Outcomes: []Cell{
			recorded(sequoia, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsTimedOut, Detail: "tests: timed out"}),
			recorded(tahoe, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Detail: "Failed to build cargo: `cc`\nexited 1"}),
		}},
		{Target: model.PlanTarget{ID: "rust-src", Target: model.Target{Name: "rust-src"}}, Passed: true, Outcomes: cells([]model.TargetResult{
			{Outcome: model.OutcomePassed, Tests: model.TestsFailed}, {Outcome: model.OutcomePassed, Tests: model.TestsNone},
		})},
	}}
	_, table, found := strings.Cut(ownedSections(bodyFacts{Evidence: &evidence}), "| Port |")
	require.True(t, found)
	table, _, found = strings.Cut(table, "###### Verification")
	require.True(t, found)
	require.Equal(t, ` macOS 15 | macOS 26 |
| --- | --- | --- |
| rust | ✓ build passed; tests failed (advisory)¹ | ✓ build passed; tests failed (advisory)¹ |
| cargo | ✓ build passed; tests timed out (advisory) | ✗ failed at install² |
| rust-src | ✓ build passed; tests failed (advisory) | ✓ |

¹ rust on macOS 15, macOS 26: `+"`tests: Failed to test rust: command execution failed`"+`
² cargo on macOS 26: `+"`` Failed to build cargo: `cc` exited 1 ``"+`

`, table)

	evidence.Targets = evidence.Targets[2:]
	require.NotContains(t, ownedSections(bodyFacts{Evidence: &evidence}), "¹", "no reason, no note")
	require.Equal(t, "¹⁰", superscript(10))
}
