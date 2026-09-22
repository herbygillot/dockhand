package publish

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"path"
	"strings"
	"unicode"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow/view"
)

// publicationBody uses only the contribution and its selected, durable evidence.
// SourceContent has already established a single-commit contribution range.
func publicationBody(content record.PublicationContent, change record.Change, source record.Source, attempt record.Attempt, shared []record.SharedFile) string {
	var b strings.Builder
	fmt.Fprintln(&b, "Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**")
	fmt.Fprintf(&b, "\n#### Description\n\n%s\n", content.Title)
	var description []string
	for _, line := range strings.Split(content.Body, "\n") {
		if !portedit.IsAttribution(line) {
			description = append(description, line)
		}
	}
	if text := strings.TrimSpace(strings.Join(description, "\n")); text != "" {
		fmt.Fprintf(&b, "\n%s\n", text)
	}
	if len(shared) > 0 {
		fmt.Fprintln(&b, "\n###### Shared files")
		fmt.Fprintln(&b)
		port := change.InitiatingTarget
		if port == "" && len(change.Targets) > 0 {
			port = change.Targets[0].Name
		}
		for _, line := range view.SharedWords(shared, port) {
			fmt.Fprintf(&b, "- %s\n", oneLine(line))
		}
	}
	fmt.Fprintln(&b, "\n###### Tested on")
	evidence := attempt.Evidence
	unverified := attempt.ID == ""
	if unverified {
		fmt.Fprintln(&b, "\nNot built locally. The author asked dockhand to publish this change without verification (`--unverified`), so no lint, test, or install verdict exists for it. The MacPorts pull request workflow is the only check it has had.")
	} else if evidence == nil {
		fmt.Fprintln(&b, "\nEnvironment details were not recorded.")
	} else {
		facts := view.Facts(evidence)
		writeComponentTable(&b, facts.Components)
		if flow := facts.Workflow; flow != nil {
			fmt.Fprintf(&b, "\nProvider: %s\n\n", facts.Provider)
			fmt.Fprintf(&b, "- [workflow run](%s), attempt %d\n", oneLine(flow.URL), flow.RunAttempt)
			for _, job := range flow.Jobs {
				fmt.Fprintf(&b, "- %s: %s\n", oneLine(job.Name), oneLine(job.Conclusion))
			}
			fmt.Fprintln(&b, "\nThe MacPorts workflow passed under its own policy. It may tolerate port test failures; individual port phases and exact runner tool versions are not independently established.")
		} else if environment := facts.Environment; environment == nil {
			fmt.Fprintln(&b, "\nEnvironment details were not recorded.")
		} else {
			if !environment.GuestRecorded {
				fmt.Fprintln(&b, "\nGuest macOS and developer tools versions were not recorded.")
			}
			image := known(environment.Image)
			if environment.Pristine {
				image += " (pristine)"
			}
			fmt.Fprintf(&b, "\nProvider: %s\n\n", known(environment.Provider))
			fmt.Fprintf(&b, "- version: %s\n", known(environment.Version))
			fmt.Fprintf(&b, "- image: %s\n", image)
			fmt.Fprintf(&b, "- environment identity: `%s`\n", oneLine(environment.Identity))
		}
		fmt.Fprintf(&b, "\nVerification attempt: `%s`; observed %s.\n", attempt.ID, facts.ObservedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Fprint(&b, "\n###### Verification\n\n")
	check := func(yes bool, text string) {
		mark := " "
		if yes {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", mark, text)
	}
	check(false, "Followed the [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages).")
	check(change.GeneratedCommit != "" && change.GeneratedCommit == source.Commit, "Squashed and [minimized commits](https://guide.macports.org/#project.github).")
	check(false, "Checked for other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change.")
	check(false, "Referenced applicable [Trac tickets](https://trac.macports.org/wiki/Tickets) with full URLs in the commit message.")
	for _, phase := range []string{"lint", "test", "install"} {
		var found *record.StepResult
		if evidence != nil {
			for i := range evidence.Steps {
				step := &evidence.Steps[i]
				if step.Package == attempt.Spec.Target.Name && step.Phase == phase && step.Verdict == record.VerdictPassed {
					found = step
				}
			}
		}
		labels := map[string]string{"lint": "Checked the Portfile with lint", "test": "Ran the port's tests", "install": "Completed a full install"}
		label := labels[phase]
		if found != nil && len(found.Command) > 0 {
			label += " with `" + normalizedCommand(found.Command, attempt.Spec.Target) + "` (run as " + known(found.User) + ")"
		} else if found != nil {
			label += " (exact command was not recorded)"
		} else if phase == "test" && evidence != nil && evidence.TestOmission != "" {
			label += " — " + oneLine(evidence.TestOmission)
		} else if phase == "test" && evidence != nil && evidence.TestFailure != "" {
			label += " — the port's tests failed; advisory here as in the MacPorts workflow: " + oneLine(evidence.TestFailure)
		} else if unverified {
			label += " (skipped at the author's request)"
		} else {
			label += " (no successful execution recorded)"
		}
		check(found != nil, label+".")
	}
	check(false, "Tested basic functionality of all binary files.")
	check(false, "Checked the port's most important [variants](https://trac.macports.org/wiki/Variants).")
	fmt.Fprintln(&b, "\nUnchecked manual items require contributor review.")
	return b.String()
}

// writeComponentTable lists what the build ran on: the guest's macOS,
// developer tools and MacPorts, and the dockhand that drove it. It is a table
// because these are facts with values, read down a column rather than through
// a paragraph. Only what was recorded appears, and a run with nothing to show,
// as a workflow observation has, writes no table at all.
func writeComponentTable(b *strings.Builder, components []view.Component) {
	if len(components) == 0 {
		return
	}
	fmt.Fprint(b, "\n| **Component** | **Version** |\n| :--- | :--- |\n")
	for _, entry := range components {
		fmt.Fprintf(b, "| %s | %s |\n", entry.Name, entry.Version)
	}
}

// testedOnHeading and verificationHeading bound the section of a body that
// dockhand writes from evidence. What is above them is the description, and
// what is below is the review checklist, which a person completes.
const testedOnHeading = "###### Tested on"
const verificationHeading = "###### Verification"

// keepBody decides what an existing pull request's body becomes.
//
// By default it becomes itself: whatever is on the forge is kept exactly,
// because a maintainer may have rewritten the description and a reviewer may
// have ticked items in the checklist, and neither can be regenerated.
//
// Refreshing replaces one section, the environment dockhand recorded, and
// leaves every byte outside it alone. That is the part that goes stale when a
// template changes or newer evidence supersedes it, and it is the only part no
// person is expected to have written. A body without that section is one
// somebody wrote themselves, so it is kept whole.
// keepBody decides what an existing pull request body becomes. The
// description and the checklist are never dockhand's to change. The Tested
// on section is: a body without one gains it at the end, since a person's
// pull request adopted by dockhand should show how it was built; a body with
// one keeps it unless refresh rewrites it from this verification; and a
// contribution adopted with keep says the body is entirely its author's.
func keepBody(existing, fresh string, refresh, keep bool) string {
	if keep {
		return existing
	}
	replacement, freshOK := testedOnSection(fresh)
	if !freshOK {
		return existing
	}
	from, to, ok := testedOnBounds(existing)
	if !ok {
		return strings.TrimRight(existing, "\n") + "\n\n" + strings.TrimRight(replacement, "\n") + "\n"
	}
	if !refresh {
		return existing
	}
	return existing[:from] + replacement + existing[to:]
}

// testedOnBounds locates the section in a body, from its heading to whatever
// follows it, which is the verification heading in a body dockhand wrote.
func testedOnBounds(body string) (int, int, bool) {
	from := strings.Index(body, testedOnHeading)
	if from < 0 {
		return 0, 0, false
	}
	rest := body[from+len(testedOnHeading):]
	next := strings.Index(rest, verificationHeading)
	if next < 0 {
		return from, len(body), true
	}
	return from, from + len(testedOnHeading) + next, true
}

func testedOnSection(body string) (string, bool) {
	from, to, ok := testedOnBounds(body)
	if !ok {
		return "", false
	}
	return body[from:to], true
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
func known(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not recorded"
	}
	return oneLine(s)
}
func normalizedCommand(argv []string, target record.Target) string {
	parts := make([]string, len(argv))
	directory := path.Dir(target.Portfile)
	for i, arg := range argv {
		if i > 0 && argv[i-1] == "-D" && strings.HasSuffix(arg, "/"+directory) {
			arg = directory
		}
		if arg == "" || strings.IndexFunc(arg, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_./:=+-", r)
		}) >= 0 {
			arg = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		}
		// Markdown code spans must not be terminated by captured command data.
		parts[i] = strings.ReplaceAll(arg, "`", "&#96;")
	}
	return strings.Join(parts, " ")
}
