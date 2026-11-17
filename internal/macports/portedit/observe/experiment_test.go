package observe

import (
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExperimentEvidenceAppliesReadByRead(t *testing.T) {
	b := newBaseFixture(t)
	d := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	other := d
	other.Values = append([]string(nil), d.Values...)
	other.Values[0] = "another question"
	other.Values[3] = "findBinary another"
	port := macports.PortObservation{ModeledHostAccess: true, BaseLibrary: b.lib, Declarations: []macports.Declaration{d, other}, Problems: []string{d.Values[0], other.Values[0]}, MachineReadExperiment: &macports.MachineReadExperiment{Outcome: "same", Varied: []int{0}}}
	_, left := Tolerate(t.Context(), port, []byte("build.args ${build.cmd}"), t.TempDir())
	require.True(t, left, "a varied read cannot explain an unvaried read")
	port.Declarations = port.Declarations[:1]
	port.Problems = port.Problems[:1]
	_, left = Tolerate(t.Context(), port, []byte("distfiles arbitrary.tgz"), t.TempDir())
	require.False(t, left)
	port.MachineReadExperiment = nil
	_, left = Tolerate(t.Context(), port, nil, t.TempDir())
	require.True(t, left, "recognition without evidence is not acceptance")
}

func TestNativeBuildDefaultNeedsNoCounterfactual(t *testing.T) {
	b := newBaseFixture(t)
	frames := append(b.substitution("build.jobs", "[portbuild::build_getjobs]"), frame("portbuild::build_getjobs"), macports.SourceFrame{File: b.file("port1.0/portbuild.tcl"), Line: 152, Command: "set jobs [sysctl hw.activecpu]"})
	d := macports.Declaration{Command: "dockhand.host-access", Values: []string{"this Mac's kernel read with sysctl"}, Frames: frames}
	_, recognized := recognizeBuildDefault(d, newBaseCode(b.lib))
	require.True(t, recognized)
	p := macports.PortObservation{HostAccess: true, BaseLibrary: b.lib, Declarations: []macports.Declaration{d}, Problems: []string{d.Values[0]}}
	_, left := Tolerate(t.Context(), p, []byte("build.args -j${build.jobs}"), t.TempDir())
	require.False(t, left, "native Base defaults use this Mac's answer without a second evaluation")
}

func TestChecksumBlockedReadCannotUseAnotherReadsEqualSummary(t *testing.T) {
	summary := &macports.MachineReadExperiment{Outcome: "same", Varied: []int{0, 1}, Unexplained: []int{0}}
	require.False(t, experimentExplains(summary, 0))
	require.True(t, experimentExplains(summary, 1))
}
