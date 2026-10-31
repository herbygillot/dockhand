package eval

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// observeEffects observes a port of these lines with the evaluator given.
func observeEffects(t *testing.T, e *Evaluator, lines string) macports.Observation {
	t.Helper()
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/effects/files/extra.tcl", "set extra_read 1\n")
	putFile(t, tree.Root(), "devel/effects/Portfile", "PortSystem 1.0\nname effects\nversion 1\n"+lines+"\n")
	bound, err := tree.Select(model.Target{Name: "effects", Portfile: "devel/effects/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true})
	require.NoError(t, err)
	return got
}

const recordedLines = `source ${filespath}/extra.tcl
set results [list [file exists /usr/bin/true] [exec /usr/bin/uname -s]]
post-destroot {
    system "echo destroot"
}`

func TestARecordingEvaluationKeepsWhatItReadRanAndDeclared(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	e.Recording = true
	o := observeEffects(t, e, recordedLines).Ports["effects"]

	read := func(command, subcommand, suffix string) macports.PathRead {
		t.Helper()
		for _, r := range o.Reads {
			if r.Command == command && r.Subcommand == subcommand && strings.HasSuffix(r.Path, suffix) {
				return r
			}
		}
		require.Failf(t, "not read", "%s %s %s in %+v", command, subcommand, suffix, o.Reads)
		return macports.PathRead{}
	}
	sourced := read("source", "", "/devel/effects/files/extra.tcl")
	require.Equal(t, "tree", sourced.Kind)
	require.Equal(t, "host", sourced.Source)
	require.True(t, filepath.IsAbs(sourced.Path))
	host := read("file", "exists", "/usr/bin/true")
	require.Equal(t, "host", host.Kind)
	require.Equal(t, 1, host.Count)
	require.Contains(t, o.Execs, macports.CommandRun{Line: "/usr/bin/uname -s", Source: "host", Count: 1})
	i := slices.IndexFunc(o.Hooks, func(h macports.Hook) bool {
		return strings.Contains(h.Procedure, "post") && strings.Contains(h.Procedure, "destroot")
	})
	require.GreaterOrEqual(t, i, 0, "%+v", o.Hooks)
	require.Contains(t, o.Hooks[i].Body, `system "echo destroot"`)
	require.Contains(t, o.Hooks[i].Origin, "devel/effects/Portfile:")

	e = liveEvaluator(t)
	o = observeEffects(t, e, recordedLines).Ports["effects"]
	require.Empty(t, o.Reads, "an evaluator that doesn't record keeps nothing")
	require.Empty(t, o.Execs)
	require.Empty(t, o.Hooks)
	require.NotEmpty(t, o.Ledger, "and still counts")
}

func TestAnInstalledRegistryAnswersAsBaseDoes(t *testing.T) {
	t.Parallel()
	lines := `set results {}
lappend results [catch {registry_active dockhand-held} active] $active
lappend results [_portnameactive dockhand-held] [registry_exists_for_name dockhand-held]
lappend results [registry_exists dockhand-held 1.2 3 +doc] [registry_exists dockhand-held 1.2]
lappend results [catch {registry_open dockhand-held 1.2 3 +doc 1} entry]
if {[lindex $results end] == 0} {
    foreach property {name version revision variants epoch active} { lappend results [registry_prop_retr $entry $property] }
}
lappend results [catch {registry_active dockhand-absent} active] $active
long_description {*}$results`
	description := func(e *Evaluator) ([]string, macports.PortObservation) {
		got := observeEffects(t, e, lines)
		values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["long_description"])
		require.Empty(t, errs)
		return values, got.Ports["effects"]
	}

	e := liveEvaluator(t)
	e.Installation = InstalledPorts
	var asked []string
	var mu sync.Mutex
	e.Installed = func(port string) map[string]InstalledPort {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, port)
		return map[string]InstalledPort{"dockhand-held": {Version: "1.2", Revision: 3, Epoch: 1, Variants: "+doc"}}
	}
	got, o := description(e)
	require.Equal(t, []string{
		"0", "{dockhand-held 1.2 3 +doc 1 1}",
		"1", "1",
		"1", "0",
		"0",
		"dockhand-held", "1.2", "3", "+doc", "1", "1",
		"1", "Registry error: dockhand-absent not registered as installed & active.",
	}, got)
	require.Contains(t, asked, "effects")
	require.Contains(t, o.Ledger, macports.LedgerEntry{Command: "registry_active", Source: "installed", Subject: "dockhand-held", Count: 1})
	require.Contains(t, o.Ledger, macports.LedgerEntry{Command: "registry_active", Source: "fresh", Subject: "dockhand-absent", Count: 1})

	got, _ = description(liveEvaluator(t))
	require.Equal(t, []string{
		"1", "Registry error: dockhand-held not registered as installed & active.",
		"0", "0",
		"0", "0",
		"1",
		"1", "Registry error: dockhand-absent not registered as installed & active.",
	}, got, "the fresh installation holds nothing")
}

func TestARecordingSessionSaysWhatMportinitRead(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	e.Recording = true
	var records []Bootstrap
	var mu sync.Mutex
	e.Bootstrap = func(b Bootstrap) {
		mu.Lock()
		defer mu.Unlock()
		records = append(records, b)
	}
	observeEffects(t, e, "")
	observeEffects(t, e, "")
	require.Len(t, records, 1, "once a session's record is had, it isn't asked again")
	record := records[0]
	require.True(t, slices.ContainsFunc(record.Reads, func(r BootstrapRead) bool {
		return r.Command == "open" && strings.HasSuffix(r.Path, "/macports.conf")
	}), "%+v", record.Reads)
	require.True(t, slices.ContainsFunc(record.Reads, func(r BootstrapRead) bool { return r.Command == "env" && r.Path == "HOME" && r.Value != "" }), "%+v", record.Reads)
	i := slices.IndexFunc(record.Settings, func(s BootstrapSetting) bool { return s.Name == "prefix" })
	require.GreaterOrEqual(t, i, 0)
	require.NotEmpty(t, record.Settings[i].Value)
	require.Contains(t, record.Aliases, WorkerAlias{Name: "registry_active", Target: "registry::active"})
	require.True(t, slices.ContainsFunc(record.Aliases, func(a WorkerAlias) bool { return a.Name == "_portnameactive" }))
}

func TestTheHostInstallationAnswersFromTheHost(t *testing.T) {
	t.Parallel()
	host := findHostInstallation(t)
	if host.port == "" {
		t.Skip("the test prefix has no port installed")
	}
	e := liveEvaluator(t)
	e.Installation = HostInstallation
	got := observeEffects(t, e, "long_description [_portnameactive "+host.port+"] [registry_exists_for_name "+host.port+"]")
	require.Equal(t, "1", strings.Fields(got.Snapshot.Ports["effects"].Options["long_description"])[1], "%s is in this Mac's registry", host.port)
	require.Contains(t, got.Ports["effects"].Ledger, macports.LedgerEntry{Command: "registry_exists_for_name", Source: "host", Subject: host.port, Count: 1})
}
