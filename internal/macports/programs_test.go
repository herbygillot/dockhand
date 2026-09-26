package macports

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func TestHostProgramsAreWellFormed(t *testing.T) {
	at, reason := ValidHostPrograms(HostPrograms)
	require.Equal(t, -1, at, "HostPrograms[%d]: %s", at, reason)
	at, _ = ValidHostPrograms([]HostProgram{{Name: `git`, Form: FormSubcommand}})
	require.Equal(t, 0, at, "a subcommand form names its subcommands")
	at, _ = ValidHostPrograms([]HostProgram{{Name: `(`, Form: FormAny}})
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
