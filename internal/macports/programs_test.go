package macports

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func TestHostProgramsAreWellFormed(t *testing.T) {
	at, reason := validHostPrograms(HostPrograms)
	require.Equal(t, -1, at, "HostPrograms[%d]: %s", at, reason)
	at, _ = validHostPrograms([]HostProgram{{Name: `git`, Form: FormSubcommand}})
	require.Equal(t, 0, at, "a subcommand form names its subcommands")
	at, _ = validHostPrograms([]HostProgram{{Name: `(`, Form: FormAny}})
	require.Equal(t, 0, at, "a pattern compiles")
}

func TestHostProgramsReadBackAsTcl(t *testing.T) {
	entries, errs := syntax.ListValues(TclHostPrograms(HostPrograms))
	require.Empty(t, errs)
	require.Len(t, entries, len(HostPrograms))
	for i, entry := range entries {
		fields, errs := syntax.DictValues(entry)
		require.Empty(t, errs, HostPrograms[i].Name)
		require.Equal(t, HostPrograms[i].Name, fields["name"])
		require.Equal(t, string(HostPrograms[i].Form), fields["form"])
		options, errs := syntax.ListValues(fields["options"])
		require.Empty(t, errs)
		require.Len(t, options, len(HostPrograms[i].Options), HostPrograms[i].Name)
		for j, option := range options {
			pair, errs := syntax.ListValues(option)
			require.Empty(t, errs)
			require.Equal(t, []string{HostPrograms[i].Options[j].Pattern, strconv.Itoa(HostPrograms[i].Options[j].Values)}, pair)
		}
		subcommands, _ := syntax.ListValues(fields["subcommands"])
		require.Equal(t, len(HostPrograms[i].Subcommands), len(subcommands))
		refused, _ := syntax.ListValues(fields["refused"])
		require.Equal(t, len(HostPrograms[i].Refused), len(refused))
	}
}

func TestWorkerAliasesReadBackAsTcl(t *testing.T) {
	answers, errs := syntax.DictValues(TclWorkerAliases(WorkerAliases))
	require.Empty(t, errs)
	require.Len(t, answers, len(WorkerAliases), "each alias is named once")
	for _, alias := range WorkerAliases {
		require.Equal(t, string(alias.Answer), answers[alias.Name])
	}
	require.Len(t, WorkerAliasReads(), 15, "Base 2.12.7 gives a worker ten aliases that write and fifteen that read")
	require.NotContains(t, WorkerAliasReads(), "registry_install")
}

func words(line string) []Argument {
	var args []Argument
	for _, word := range strings.Fields(line) {
		args = append(args, Argument{Text: word, Literal: !strings.HasPrefix(word, "$")})
	}
	return args
}

// The rules the evaluator's dispatcher applies in Tcl (dispatcher.tcl),
// applied to source text: a substituted word, $ here, is admitted only as
// an option's value. A literal command line's rows are the one admission
// table, eval's TestExecAdmissionIsOneTable, which runs each too (#39).
func TestCommandLineRefusalAdmitsComputedWordsOnlyAsValues(t *testing.T) {
	for line, want := range map[string]string{
		"/usr/libexec/java_home -f -v $version": "",
		"git -C $dir log -1 --pretty=%ct":       "",
		"/usr/libexec/java_home $flag":          "runs java_home with a computed argument",
		"$program -v":                           "runs a computed program",
		"echo hi > $file":                       "writes to a computed file",
		"ruby1.8 -e $code":                      "runs ruby1.8 with -e",
	} {
		require.Equal(t, want, CommandLineRefusal(words(line)), line)
	}
}

// validHostPrograms reports the first entry of programs whose patterns do
// not compile, or whose form is unknown, as its index and a reason; -1 if
// every entry is sound.
func validHostPrograms(programs []HostProgram) (int, string) {
	for i, program := range programs {
		patterns := []string{program.Name}
		for _, option := range program.Options {
			patterns = append(patterns, option.Pattern)
		}
		patterns = append(patterns, program.Refused...)
		for _, pattern := range patterns {
			if _, err := regexp.Compile("^(?:" + pattern + ")$"); err != nil {
				return i, err.Error()
			}
		}
		switch program.Form {
		case FormAny, FormOnly, FormWrapper:
			if len(program.Subcommands) > 0 {
				return i, "subcommands outside the subcommand form"
			}
		case FormSubcommand:
			if len(program.Subcommands) == 0 {
				return i, "the subcommand form without subcommands"
			}
		default:
			return i, "unknown form " + strconv.Quote(string(program.Form))
		}
	}
	return -1, ""
}
