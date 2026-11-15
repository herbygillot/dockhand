package observe

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"

	"github.com/stretchr/testify/require"
)

func TestExplanationContinuesAfterFailureWithoutChangingTheVerdict(t *testing.T) {
	b := newBaseFixture(t)
	d := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	bad := macports.Declaration{Command: "dockhand.host-access", Values: []string{"unexplained", "closed", "portfile", "exec false"}}
	p := macports.PortObservation{ModeledHostAccess: true, Problems: []string{"unexplained", d.Values[0]}, Declarations: []macports.Declaration{bad, d}, BaseLibrary: b.lib}
	data := []byte("PortSystem 1.0\nname x\nversion 1\n")
	root := t.TempDir()
	want, failed := Tolerate(t.Context(), p, data, root)
	got := Explain(t.Context(), p, data, root)
	require.True(t, failed)
	require.Equal(t, want, got.Observation)
	require.Equal(t, failed, got.Inconclusive)
	require.Len(t, got.Accesses, 2)
	require.Equal(t, RuleNone, got.Accesses[0].Rule)
	require.Equal(t, RuleBuildDefault, got.Accesses[1].Rule)
	require.True(t, got.Accesses[1].Followers[0].Benign)
	require.True(t, p.ModeledHostAccess, "the input remains unchanged")
}

func TestExplanationIdentifiesAnAccessNotCountedByTheInput(t *testing.T) {
	p := macports.PortObservation{Declarations: []macports.Declaration{{Command: "dockhand.host-access", Values: []string{"not flagged"}}}}
	got := Explain(t.Context(), p, nil, t.TempDir())
	require.False(t, got.Inconclusive)
	require.Len(t, got.Accesses, 1)
	require.True(t, got.Accesses[0].Ignored)
	require.Equal(t, RuleNone, got.Accesses[0].Rule)
}

func TestToleranceDebugSentencesStayUnchanged(t *testing.T) {
	for _, c := range []struct {
		rule    Rule
		refused bool
		want    string
	}{
		{RuleBuildDefault, false, "Host access explained as a build-only option's default: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RuleBaseLookup, false, "Host access explained as Base's own lookup for an option no fetch or checksum value reads: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RuleSourceDate, false, "Host access explained as Base's source_date_epoch lookup of git: the snapshot has no .git, so the answer doesn't depend on whether git is found: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RuleCallback, false, "Host access explained as a PortGroup callback's build-only read: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RulePortGroup, false, "Host access explained as a PortGroup's build-only read: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RuleNone, false, "Host access not explained: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RuleNone, true, "Host access is a compiler probe but the Portfile reads toolchain options outside build positions: host answer; frames: /tree/Portfile:7 build.args [probe]"},
		{RuleToolchain, false, ""},
	} {
		t.Run(string(c.rule), func(t *testing.T) {
			var updates []progress.Update
			ctx := progress.WithReporter(t.Context(), func(u progress.Update) { updates = append(updates, u) })
			d := macports.Declaration{Values: []string{"host answer"}, Frames: []macports.SourceFrame{{File: "/tree/Portfile", Line: 7, Command: "build.args [probe]\nsecond line"}}}
			reportAccess(ctx, c.rule, c.refused, d)
			if c.want == "" {
				require.Empty(t, updates, "accepted compiler probes were silent")
			} else {
				require.Equal(t, []progress.Update{{Level: progress.Debug, Message: c.want}}, updates)
			}
		})
	}
}

func TestToleranceReportsACompilerRefusal(t *testing.T) {
	b := newBaseFixture(t)
	d := macports.Declaration{Command: "dockhand.host-access", Values: []string{"compiler answer"}, Frames: []macports.SourceFrame{{Command: "portconfigure::probe"}, {File: b.file("port1.0/portconfigure.tcl"), Line: 1, Command: "return [findBinary clang /usr/bin/clang]"}}}
	p := macports.PortObservation{ModeledHostAccess: true, Declarations: []macports.Declaration{d}, BaseLibrary: b.lib}
	var updates []progress.Update
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { updates = append(updates, u) })
	_, bad := Tolerate(ctx, p, []byte("distfiles ${configure.cc}.tgz"), t.TempDir())
	require.True(t, bad)
	require.Equal(t, []progress.Update{{Level: progress.Debug, Message: "Host access is a compiler probe but the Portfile reads toolchain options outside build positions: compiler answer; frames: :0 portconfigure::probe <- " + b.file("port1.0/portconfigure.tcl") + ":1 return [findBinary clang /usr/bin/clang]"}}, updates)
}
