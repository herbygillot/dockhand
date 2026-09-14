package publish

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/herbygillot/dockhand/internal/record"
)

// publicationBody uses only the contribution and its selected, durable evidence.
// SourceContent has already established a single-commit contribution range.
func publicationBody(content record.PublicationContent, change record.Change, source record.Source, attempt record.Attempt) string {
	var b strings.Builder
	fmt.Fprintln(&b, "Submitted by [dockhand](https://github.com/herbygillot/dockhand)")
	fmt.Fprintf(&b, "\n#### Description\n\n%s\n", content.Title)
	var description []string
	for _, line := range strings.Split(content.Body, "\n") {
		if !strings.HasPrefix(line, "Generated-by: ") {
			description = append(description, line)
		}
	}
	if text := strings.TrimSpace(strings.Join(description, "\n")); text != "" {
		fmt.Fprintf(&b, "\n%s\n", text)
	}
	fmt.Fprintln(&b, "\n###### Tested on")
	evidence := attempt.Evidence
	if evidence == nil {
		fmt.Fprintln(&b, "\nEnvironment details were not recorded.")
	} else {
		environment := evidence.Environment
		if environment == nil {
			fmt.Fprintln(&b, "\nEnvironment details were not recorded.")
		} else {
			guest := environment.Guest
			if guest == nil {
				fmt.Fprintln(&b, "\nGuest macOS and developer tools versions were not recorded.")
			} else {
				fmt.Fprintf(&b, "\nmacOS %s; build %s; %s\n", known(guest.MacOSVersion), known(guest.MacOSBuild), known(guest.Architecture))
				tools := "Developer tools"
				switch guest.DeveloperTools {
				case record.DeveloperToolsXcode:
					tools = "Xcode"
				case record.DeveloperToolsCommandLine:
					tools = "Command Line Tools"
				}
				fmt.Fprintf(&b, "\n%s: %s\n", tools, known(guest.DeveloperToolsVersion))
				if guest.MacPortsVersion != "" {
					fmt.Fprintf(&b, "\nMacPorts: %s\n", oneLine(strings.TrimPrefix(guest.MacPortsVersion, "Version: ")))
				}
				if guest.NoActivePorts && guest.NoForeignPackageManagers {
					fmt.Fprintln(&b, "\nBefore verification: no active MacPorts ports or detected foreign package-manager prefixes.")
				}
			}
			fmt.Fprintf(&b, "\nProvider: %s; version: %s; image: %s\n", known(environment.Provider), known(environment.ProviderVersion), known(environment.Image))
			fmt.Fprintf(&b, "\nEnvironment identity: `%s`\n", oneLine(environment.EnvironmentDigest))
		}
		fmt.Fprintf(&b, "\nVerification attempt: `%s`; observed %s.\n", attempt.ID, evidence.ObservedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
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
		} else {
			label += " (no successful execution recorded)"
		}
		check(found != nil, label+".")
	}
	check(false, "Tested basic functionality of all binary files.")
	check(false, "Checked the port's most important [variants](https://trac.macports.org/wiki/Variants).")
	fmt.Fprintln(&b, "\nCommand paths above are relative to the verified ports tree where possible. Unchecked manual items require contributor review.")
	return b.String()
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
