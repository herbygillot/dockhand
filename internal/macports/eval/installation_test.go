package eval

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// hostInstallation is what the test prefix has installed: a port the
// registry holds, a program only the prefix has, and one the prefix
// shadows along PATH with an argument that only reports, and whose answer
// tells the two apart.
type hostInstallation struct {
	prefix, port, only, shadowed, flag string
}

func findHostInstallation(t *testing.T) hostInstallation {
	t.Helper()
	_, tcl := tclSession(t)
	host := hostInstallation{prefix: tcl("set ::macports::prefix")}
	host.port = tcl("set entries [registry::entry imaged]; expr {[llength $entries] ? [[lindex $entries 0] name] : {}}")
	programs, err := filepath.Glob(filepath.Join(host.prefix, "bin", "*"))
	require.NoError(t, err)
	base := []string{"port", "portindex", "portmirror", "port-tclsh", "daemondo"}
	for _, program := range programs {
		name := filepath.Base(program)
		if slices.Contains(base, name) {
			continue
		}
		elsewhere := false
		for _, dir := range []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				elsewhere = true
			}
		}
		if !elsewhere && host.only == "" {
			host.only = program
		}
	}
	for _, candidate := range [][2]string{{"perl", "-V:installprefix"}, {"python3", "--version"}} {
		if _, err := os.Stat(filepath.Join(host.prefix, "bin", candidate[0])); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join("/usr/bin", candidate[0])); err == nil {
			host.shadowed, host.flag = candidate[0], candidate[1]
			break
		}
	}
	return host
}

// evaluatedDescription evaluates lines that leave a Tcl list in results,
// and returns the list: the port's description carries it out.
func evaluatedDescription(t *testing.T, lines string) []string {
	t.Helper()
	got, err := evaluateLines(t, "set results {}\n"+lines+"\ndescription {*}$results", false)
	require.NoError(t, err)
	values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["description"])
	require.Empty(t, errs)
	return values
}

func TestTheInstallationIsFresh(t *testing.T) {
	t.Parallel()
	host := findHostInstallation(t)
	got := evaluatedDescription(t, strings.Join([]string{
		"lappend results [file exists ${prefix}/bin/port]",
		"lappend results [file isdirectory ${prefix}/lib/pkgconfig]",
		"lappend results [file exists ${prefix}/share/macports/install/prefix.mtree]",
		"lappend results [lsort [glob -nocomplain -tails -directory ${prefix}/bin *]]",
		"lappend results [catch {registry_active dockhand-no-such-port} message] $message",
	}, "\n"))
	require.Equal(t, "1", got[0], "Base's own program is there")
	require.Equal(t, "1", got[1], "the prefix's skeleton is there")
	require.Equal(t, "1", got[2], "Base's own files are there")
	listed, errs := syntax.ListValues(got[3])
	require.Empty(t, errs)
	for _, name := range listed {
		require.Contains(t, []string{"port", "portindex", "portmirror", "port-tclsh", "daemondo"}, name, "only Base's programs are in a fresh bin")
	}
	require.Equal(t, []string{"1", "Registry error: dockhand-no-such-port not registered as installed & active."}, got[4:6])

	if host.port != "" {
		got := evaluatedDescription(t, strings.Join([]string{
			"lappend results [_portnameactive " + host.port + "]",
			"lappend results [catch {registry_active " + host.port + "} message] $message",
			"lappend results [registry_exists_for_name " + host.port + "]",
		}, "\n"))
		require.Equal(t, []string{"0", "1", "Registry error: " + host.port + " not registered as installed & active.", "0"}, got,
			"%s is installed on this Mac and not in a fresh installation", host.port)
	}
	if host.only != "" {
		name := filepath.Base(host.only)
		got := evaluatedDescription(t, strings.Join([]string{
			"lappend results [file exists " + host.only + "] [file executable " + host.only + "]",
			"lappend results [catch {file size " + host.only + "} message] $message",
			"lappend results [catch {open " + host.only + "} message] $message",
			"lappend results [catch {exec " + host.only + "} message] $message",
			"lappend results [catch {exec " + name + "} message] $message",
			"lappend results [catch {findBinary " + name + "} message]",
			"lappend results [registry_file_registered " + host.only + "]",
		}, "\n"))
		require.Equal(t, []string{
			"0", "0",
			"1", `could not read "` + host.only + `": no such file or directory`,
			"1", `couldn't open "` + host.only + `": no such file or directory`,
			"1", `couldn't execute "` + host.only + `": no such file or directory`,
			"1", `couldn't execute "` + name + `": no such file or directory`,
			"1",
			"0",
		}, got, "%s is installed on this Mac and not in a fresh installation", host.only)
	}
	if host.shadowed != "" {
		want, err := exec.Command(filepath.Join("/usr/bin", host.shadowed), host.flag).CombinedOutput()
		require.NoError(t, err)
		got := evaluatedDescription(t, "lappend results [exec "+host.shadowed+" "+host.flag+" 2>@1]")
		require.Equal(t, strings.TrimSuffix(string(want), "\n"), got[0], "%s runs from /usr/bin, not the prefix", host.shadowed)
		require.NotContains(t, got[0], host.prefix)
	}
}

func TestTheLedgerNamesWhatTheInstallationWasAsked(t *testing.T) {
	t.Parallel()
	got, err := evaluateLines(t, "catch {registry_active dockhand-asked}\nset found [_portnameactive dockhand-too]", true)
	require.NoError(t, err)
	var asked []string
	for _, entry := range got.Ports["effects"].Ledger {
		if entry.Source == "fresh" && entry.Subject != "" {
			asked = append(asked, entry.Command+" "+entry.Subject)
		}
	}
	require.Contains(t, asked, "registry_active dockhand-asked")
	require.Contains(t, asked, "_portnameactive dockhand-too")
}

func TestFileStatSetsItsCallersArray(t *testing.T) {
	t.Parallel()
	got := evaluatedDescription(t, "file stat /etc/hosts info\nlappend results [info exists info(size)]")
	require.Equal(t, []string{"1"}, got)
}
