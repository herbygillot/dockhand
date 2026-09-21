package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A run command's flags are five kinds of thing: which contribution, what
// the change says, how it is built, how it reaches GitHub, and how the run
// behaves. Each flag is annotated with its kind where it is defined, and the
// usage template prints the kinds as sections instead of one alphabetical
// list. The global flags divide the same way, into paths and output.
const sectionKey = "dockhand.section"

const (
	sectionSelection = "Selection"
	sectionChange    = "Change"
	sectionBuild     = "Build"
	sectionGitHub    = "GitHub"
	sectionRun       = "Run"
	sectionPaths     = "Path"
	sectionOutput    = "Output"
)

// sectionOrder is how the sections print: the run's own kinds first, then
// the global ones, which are a command's own on the root command.
var sectionOrder = []string{sectionSelection, sectionChange, sectionBuild, sectionGitHub, sectionRun, sectionPaths, sectionOutput}

// section files the named flags under one section. Naming a flag that does
// not exist is a programming error, so it fails loudly.
func section(flags *pflag.FlagSet, name string, names ...string) {
	for _, flag := range names {
		if err := flags.SetAnnotation(flag, sectionKey, []string{name}); err != nil {
			panic(fmt.Sprintf("cli: sectioning --%s: %v", flag, err))
		}
	}
}

func flagSection(flag *pflag.Flag) string {
	if values := flag.Annotations[sectionKey]; len(values) == 1 {
		return values[0]
	}
	return ""
}

// flagSections renders a command's own flags and then the inherited ones,
// each set by section. A set with no sectioned flag prints as cobra would.
func flagSections(command *cobra.Command) string {
	var b strings.Builder
	if command.HasAvailableLocalFlags() {
		b.WriteString(renderSections(command.LocalFlags(), "", "Flags:", "Other flags:"))
	}
	if command.HasAvailableInheritedFlags() {
		b.WriteString(renderSections(command.InheritedFlags(), "Global ", "Global Flags:", "Global flags:"))
	}
	return b.String()
}

// renderSections prints the flags of a set under their section headings, in
// order, followed by any unsectioned flags under the rest heading; a set with
// no sectioned flag prints whole under the plain heading.
func renderSections(flags *pflag.FlagSet, prefix, plain, rest string) string {
	members := map[string]*pflag.FlagSet{}
	sectioned := false
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		name := flagSection(flag)
		if members[name] == nil {
			members[name] = pflag.NewFlagSet(name, pflag.ContinueOnError)
		}
		members[name].AddFlag(flag)
		sectioned = sectioned || name != ""
	})
	if !sectioned {
		return "\n\n" + plain + "\n" + strings.TrimRight(flags.FlagUsages(), " \n")
	}
	var b strings.Builder
	for _, name := range sectionOrder {
		if set := members[name]; set != nil {
			heading := name + " flags:"
			if prefix != "" {
				heading = prefix + strings.ToLower(name) + " flags:"
			}
			fmt.Fprintf(&b, "\n\n%s\n%s", heading, strings.TrimRight(set.FlagUsages(), " \n"))
		}
	}
	if set := members[""]; set != nil {
		fmt.Fprintf(&b, "\n\n%s\n%s", rest, strings.TrimRight(set.FlagUsages(), " \n"))
	}
	return b.String()
}

// usageTemplate is cobra's default with the two flag blocks replaced by the
// sectioned rendering; everything else prints as cobra prints it.
const usageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{flagSections .}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
