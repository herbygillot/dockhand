package prdescription

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// Tested on states what the environment reported, as MacPorts' template
// has it, and the runs behind its results. Without a report it names the
// release and the tools the environment stated, as the engine words them:
// never a Darwin version read as macOS's.
func TestTestedOnSaysWhatTheEnvironmentWas(t *testing.T) {
	tart := []Run{{ID: "tart_7y62p4sigena6xlr", Ref: "dockhand-check-run-x-tahoe-1", Check: "check-11"}}
	tahoe := func(tools model.DeveloperTools) Report {
		return Report{Architecture: "arm64", Release: "macOS 26 (Tahoe) arm64", Tools: tools, Provider: "tart: built in a clean VM"}
	}
	with := func(r Report, observed model.Observed, runs []Run) Report {
		r.Observed, r.Runs = observed, runs
		return r
	}
	workflow := Report{Provider: "github: MacPorts' CI workflow in the author's fork"}
	for _, test := range []struct {
		name   string
		report Report
		want   string
	}{
		{"reported, with Xcode", with(tahoe(model.DeveloperToolsXcode), model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", Tools: "26.6.0.0.1781586589"}, tart),
			"macOS 26.6.2 25G71 arm64\nXcode 26.6 17F42 · tart: built in a clean VM (Run ID: tart_7y62p4sigena6xlr - checked in check-11)\n\n"},
		{"reported, with its MacPorts", with(tahoe(model.DeveloperToolsXcode), model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Xcode: "26.6", XcodeBuild: "17F42", MacPorts: "2.12.6"}, tart),
			"macOS 26.6.2 25G71 arm64\nXcode 26.6 17F42 · MacPorts 2.12.6 · tart: built in a clean VM (Run ID: tart_7y62p4sigena6xlr - checked in check-11)\n\n"},
		{"reported, with the tools", with(tahoe(model.DeveloperToolsCommandLine), model.Observed{MacOS: "26.6.2", Build: "25G71", Architecture: "arm64", Tools: "26.6.0.0.1781586589"},
			append([]Run{{ID: "tart_b3kq9wz0m1xv4ce7", Check: "check-10"}}, tart...)),
			"macOS 26.6.2 25G71 arm64\nCommand Line Tools 26.6.0.0.1781586589 · tart: built in a clean VM (Run IDs: tart_b3kq9wz0m1xv4ce7 - checked in check-10; tart_7y62p4sigena6xlr - checked in check-11)\n\n"},
		{"not reported", tahoe(model.DeveloperToolsXcode), "macOS 26 (Tahoe) arm64\nXcode, its version not recorded · tart: built in a clean VM\n\n"},
		{"an unknown release", Report{Architecture: "arm64", Release: "Darwin 30 arm64", Provider: "tart: built in a clean VM"}, "Darwin 30 arm64\nDeveloper tools not recorded · tart: built in a clean VM\n\n"},
		{"partly reported, a run of an unknown check", with(tahoe(model.DeveloperToolsXcode), model.Observed{MacOS: "26.6.2", Xcode: "26.6"}, []Run{{ID: "tart_q2w8e4r6t1y3u5i7"}}),
			"macOS 26.6.2 arm64\nXcode 26.6 · tart: built in a clean VM (Run ID: tart_q2w8e4r6t1y3u5i7)\n\n"},
		{"nothing known", Report{Provider: "command: built by the author's own command"}, "Developer tools not recorded · command: built by the author's own command\n\n"},
		{"a workflow run", with(workflow, model.Observed{}, []Run{{ID: "github_q2w8e4r6t1y3u5i7", Ref: "https://github.com/ada/macports-ports/actions/runs/123", Check: "check-11"}}),
			"Developer tools not recorded · github: MacPorts' CI workflow in the author's fork (Run ID: https://github.com/ada/macports-ports/actions/runs/123 - checked in check-11)\n\n"},
		{"a workflow run, its runners' releases reported", with(workflow,
			model.Observed{Builders: []model.BuilderObserved{{Builder: "macos-14", MacOS: "14"}, {Builder: "macos-15", MacOS: "15"}, {Builder: "macos-15-intel", MacOS: "15"}, {Builder: "macos-latest"}}},
			[]Run{{ID: "github_q2w8e4r6t1y3u5i7", Ref: "https://github.com/ada/macports-ports/actions/runs/123", Check: "check-11"}}),
			"macOS 14, 15\nDeveloper tools not recorded · github: MacPorts' CI workflow in the author's fork (Run ID: https://github.com/ada/macports-ports/actions/runs/123 - checked in check-11)\n\n"},
		{"a workflow run whose runners named no release", with(workflow, model.Observed{Builders: []model.BuilderObserved{{Builder: "macos-latest"}}}, nil),
			"Developer tools not recorded · github: MacPorts' CI workflow in the author's fork\n\n"},
		{"a run reused in another check", with(tahoe(model.DeveloperToolsXcode), model.Observed{}, []Run{{ID: "tart_a", Check: "check-3", ReusedIn: "check-4"}}),
			"macOS 26 (Tahoe) arm64\nXcode, its version not recorded · tart: built in a clean VM (Run ID: tart_a - checked in check-3, reused in check-4)\n\n"},
	} {
		require.Equal(t, test.want, test.report.lines(), test.name)
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
	merged, sections := Merge(old, old, fresh, false)
	require.Equal(t, fresh, merged)
	require.Equal(t, Refreshed, sections.TestedOn)

	edited := strings.Replace(old, "macOS 26", "macOS 26, and by hand", 1)
	merged, _ = Merge(edited, old, fresh, false)
	require.Equal(t, edited, merged, "its old last line stays, so no first line is added")

	removed := strings.TrimPrefix(fresh, submittedBy+"\n\n")
	merged, _ = Merge(removed, fresh, fresh, false)
	require.Equal(t, removed, merged, "a first line a person took out stays out")

	introduced := "Why now: a CVE.\n\n" + old
	merged, _ = Merge(introduced, old, fresh, false)
	require.True(t, strings.HasPrefix(merged, "Why now: a CVE.\n\n#### Description"), "one a person began otherwise begins as they did")

	// One dockhand began with the plain line, before it was bold, is given
	// the bold one, while it's as dockhand wrote it.
	plain := plainSubmittedBy + "\n\n#### Description\n\nupdate\n\n###### Tested on\n\nmacOS 26\n\n" + signature("v3.1.0") + "\n"
	merged, _ = Merge(plain, plain, fresh, false)
	require.Equal(t, fresh, merged)
	merged, _ = Merge("Why now.\n\n"+plain, plain, fresh, false)
	require.True(t, strings.HasPrefix(merged, "Why now.\n\n"+plainSubmittedBy+"\n"), "a line a person moved stays theirs")
}

// The template's Type(s) are its own, which no caller can change: one
// asked for that isn't one of them is refused by submit.
func TestTheTypesAreTheTemplates(t *testing.T) {
	require.Equal(t, []string{"bugfix", "enhancement", "security fix"}, Types())
	got := Types()
	got[0] = "feature"
	require.Equal(t, "bugfix", Types()[0], "a copy, never the template's own")
	require.True(t, IsType("security fix"))
	require.False(t, IsType("feature"))
	require.Equal(t, "¹⁰", superscript(10))
}

// A merged description rewrites only what dockhand wrote: everything from
// Tested on down while it's as dockhand last wrote it, GitHub's line
// endings aside.
func TestTheMergedDescriptionKeepsOnlyWhatDockhandWrote(t *testing.T) {
	fresh := "#### Description\n\nnew\n\n###### Tested on\n\nnew evidence\n"
	last := "#### Description\n\nold\n\n###### Tested on\n\nold evidence\n"
	merged, sections := Merge("#### Description\n\nmine\n\n###### Tested on\r\n\r\nold evidence\r\n", last, fresh, false)
	require.Equal(t, Refreshed, sections.TestedOn)
	require.Equal(t, "#### Description\n\nmine\n\n###### Tested on\n\nnew evidence\n", merged, "the description stays the person's")
	_, sections = Merge("#### Description\n\nmine\n\n###### Tested on\n\nI built it myself\n", last, fresh, false)
	require.Equal(t, Kept, sections.TestedOn)
	_, sections = Merge("#### Description\n\nmine\n", last, fresh, false)
	require.Equal(t, Absent, sections.TestedOn)
	_, sections = Merge(last, last, last, false)
	require.Equal(t, Current, sections.TestedOn, "dockhand's, and already as it would write it")
}

// The Type(s) are dockhand's while they are what it wrote: refreshed then,
// kept once a person edits them, and replaced by types the person names.
// A description that leaves them out, with Tested on alone, stays without,
// unless types are named, which go before Tested on. The merge says what
// it did to each part, rather than a second reading of its result.
func TestTheMergedDescriptionsTypesAreDockhandsWhileUnchanged(t *testing.T) {
	body := func(description string, ticked []string, evidence string) string {
		var types strings.Builder
		for _, kind := range Types() {
			types.WriteString("- [" + tick(slices.Contains(ticked, kind)) + "] " + kind + "\n")
		}
		return "#### Description\n\n" + description + "\n\n###### Type(s)\n\n" + types.String() + "\n###### Tested on\n\n" + evidence + "\n"
	}
	last := body("jq: update", nil, "old evidence")
	fresh := body("jq: update", []string{"enhancement"}, "new evidence")

	merged, sections := Merge(last, last, fresh, false)
	require.Equal(t, fresh, merged, "all of it as dockhand now writes it: the enhancement an update is, and the new evidence")
	require.Equal(t, Sections{Description: Current, Types: Refreshed, TestedOn: Refreshed}, sections)
	merged, _ = Merge(strings.ReplaceAll(last, "\n", "\r\n"), last, fresh, false)
	require.Contains(t, merged, "- [x] enhancement", "GitHub's line endings aren't a person's edit")
	ticked := body("jq: update", []string{"enhancement"}, "old evidence")
	merged, sections = Merge(ticked, ticked, body("jq: update", []string{"security fix"}, "new evidence"), false)
	require.Equal(t, body("jq: update", []string{"enhancement", "security fix"}, "new evidence"), merged,
		"what dockhand ticked stays ticked, though a person's commit since means it wouldn't tick it now; what's newly true is ticked too")
	require.Equal(t, Refreshed, sections.Types)
	_, sections = Merge(ticked, ticked, body("jq: update", nil, "old evidence"), false)
	require.Equal(t, Sections{Description: Current, Types: Current, TestedOn: Current}, sections, "nothing ticked goes, and nothing new is")

	edited := body("mine", []string{"bugfix"}, "old evidence")
	merged, sections = Merge(edited, last, fresh, false)
	require.Equal(t, body("mine", []string{"bugfix"}, "new evidence"), merged, "Type(s) a person ticked stay theirs")
	require.Equal(t, Sections{Description: Kept, Types: Kept, TestedOn: Refreshed}, sections)
	merged, sections = Merge(edited, last, body("jq: update", []string{"security fix"}, "new evidence"), true)
	require.Equal(t, body("mine", []string{"security fix"}, "new evidence"), merged, "types the person names replace them")
	require.Equal(t, Refreshed, sections.Types)

	elided := "#### Description\n\nmine\n\n###### Tested on\n\nold evidence\n"
	merged, sections = Merge(elided, last, fresh, false)
	require.Equal(t, "#### Description\n\nmine\n\n###### Tested on\n\nnew evidence\n", merged, "a description without Type(s) stays without")
	require.Equal(t, Sections{Description: Kept, Types: Absent, TestedOn: Refreshed}, sections, "Tested on alone is still dockhand's")
	merged, sections = Merge(elided, last, fresh, true)
	require.Equal(t, "#### Description\n\nmine\n\n###### Type(s)\n\n- [ ] bugfix\n- [x] enhancement\n- [ ] security fix\n\n###### Tested on\n\nnew evidence\n", merged,
		"types the person names go before Tested on")
	require.Equal(t, Sections{Description: Kept, Types: Refreshed, TestedOn: Refreshed}, sections)
}

// The Description is dockhand's while it is what dockhand wrote, as the
// Type(s) are: a commit's body written since the pull request opened
// reaches it then (the hugo exercise's re-submitting sshuttle, finding 3).
// One a person edited, or left out, stays so.
func TestTheMergedDescriptionsDescriptionIsDockhandsWhileUnchanged(t *testing.T) {
	types := typesSection([]string{"enhancement"})
	body := func(description string) string {
		return "#### Description\n\n" + description + types + "###### Tested on\n\nevidence\n"
	}
	last := body("")
	fresh := body("Build with Python 3.14, the python PortGroup's default.\n\n")
	merged, sections := Merge(last, last, fresh, false)
	require.Equal(t, fresh, merged)
	require.Equal(t, Refreshed, sections.Description)
	merged, sections = Merge(strings.ReplaceAll(last, "\n", "\r\n"), last, fresh, false)
	require.Contains(t, merged, "Build with Python 3.14", "GitHub's line endings aren't a person's edit")
	require.Equal(t, Refreshed, sections.Description)

	mine := body("What I tested by hand.\n\n")
	merged, sections = Merge(mine, last, fresh, false)
	require.Equal(t, mine, merged, "a person's Description stays theirs")
	require.Equal(t, Kept, sections.Description)
	elided := types + "###### Tested on\n\nevidence\n"
	merged, sections = Merge(elided, last, fresh, false)
	require.Equal(t, elided, merged, "a description without one stays without")
	require.Equal(t, Absent, sections.Description)
}

// The releases MacPorts' CI builds on that no check here did are said
// under the table, so a reviewer knows what the author's check didn't
// cover (field testing, #35157).
func TestUncoveredCIReleasesAreSaid(t *testing.T) {
	facts := Facts{TestedOn: TestedOn{Columns: []string{"macOS 26"}, Rows: []Row{{Port: "tart", Cells: []Cell{{Words: "✓"}}}}, UncoveredCI: "14 and 15"}}
	require.Contains(t, Owned(facts), "\nMacPorts CI also builds on macOS 14 and 15, which no check here built on.\n")
	facts.TestedOn.UncoveredCI = ""
	require.NotContains(t, Owned(facts), "MacPorts CI also builds")
}

// A Description says nothing of the change where the one commit's body is
// empty or trailers alone, and nothing else is said there.
func TestADescriptionOfASubjectOnlyCommitSaysNothing(t *testing.T) {
	t.Parallel()
	one := func(message string) Facts { return Facts{Commits: []Commit{{ID: "abc", Message: message}}} }
	require.True(t, SaysNothing(one("py-coremltools: update to 9.0")))
	require.True(t, SaysNothing(one("py-coremltools: update to 9.0\n\nAssisted-by: Claude Code")))
	require.False(t, SaysNothing(one("py-coremltools: update to 9.0\n\nAdds NOTICE's Apache-2 parts to the license.")))
	withNote := one("py-coremltools: update to 9.0")
	withNote.Note = "Tested with a model conversion."
	require.False(t, SaysNothing(withNote))
	require.False(t, SaysNothing(Facts{Commits: []Commit{{Message: "a: x"}, {Message: "b: y"}}}), "a table of commits says what each did")
}

// What dockhand records as written keeps its own text where a person's
// edit was kept, so the next merge still tells them apart: recorded
// merged, an edited Description survived one re-submit (the architecture
// re-synthesis, L1).
func TestWrittenKeepsDockhandsTextWhereAPersonsWasKept(t *testing.T) {
	written := "#### Description\n\nupdate\n\n###### Tested on\n\nmacOS 26\n"
	edited := strings.Replace(written, "update", "What I tested by hand.", 1)
	fresh := strings.Replace(written, "macOS 26", "macOS 26 and 15", 1)
	merged, sections := Merge(edited, written, fresh, false)
	require.Equal(t, Kept, sections.Description)
	require.Contains(t, merged, "What I tested by hand.")
	recorded := Written(merged, written, sections)
	require.Equal(t, fresh, recorded, "dockhand's Description, and the Tested on it refreshed")
	again, sections := Merge(merged, recorded, fresh, false)
	require.Equal(t, Kept, sections.Description, "still theirs on the next submit")
	require.Equal(t, merged, again)
}
