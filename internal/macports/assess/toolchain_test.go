package assess

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// pinnedGoPort is a module-mode Go port with the minimum given, pinning
// the Go series given as trivy did: go.bin naming the toolchain's command,
// and a build dependency on its port.
func pinnedGoPort(minimum, series string) macports.PortInfo {
	port := goPort(minimum)
	port.Options["go.bin"] = "/opt/local/bin/go-" + series
	port.Dependencies = []macports.Dependency{{Port: "go-" + series, Phase: "build", Spec: "port:go-" + series}}
	return port
}

// goMods are one archive's pair whose go.mod requires the Go given, before
// and after.
func goMods(t *testing.T, before, after string) []Pair {
	t.Helper()
	return []Pair{{Before: read(t, "pkg-1", map[string]string{"go.mod": "module m\n\ngo " + before + "\n"}, project.Spec{}),
		After: read(t, "pkg-2", map[string]string{"go.mod": "module m\n\ngo " + after + "\n"}, project.Spec{})}}
}

// toolchainFinding is the toolchain rule's finding about a requirement.
func toolchainFinding(subject, message string, class model.ConcernClass, hold bool) model.UpstreamChange {
	return model.UpstreamChange{Kind: "toolchain", Path: "go.mod", Rule: GoToolchainRule, Subject: subject, Class: class, Hold: hold, Message: message}
}

// A Go the Portfile pins older than go.mod requires holds, whatever the
// minimum says: trivy pinned go-1.26, 0.75.0's go.mod required 1.27.0, and
// the update raised go.toolchain_min to 1.27.0 and said it gated on it,
// where a build with the pin fails (the trivy run, #35083). A pin by its
// dependency alone, as vault's, is one too; where the minimum doesn't gate
// on the requirement either, both are said; and where the base couldn't be
// read, what couldn't be checked holds.
func TestAGoPinOlderThanGoModRequiresHolds(t *testing.T) {
	byDependency := goPort("1.27.0")
	byDependency.Dependencies = []macports.Dependency{{Port: "go-1.26", Phase: "build", Spec: "port:go-1.26"}}
	for _, test := range []struct {
		name  string
		input Input
		want  model.UpstreamChange
	}{
		{"the minimum raised, as the update said it",
			Input{Port: pinnedGoPort("1.27.0", "1.26"), Base: pinnedGoPort("1.26.3", "1.26"), Pairs: goMods(t, "1.26.3", "1.27.0"),
				Toolchain: &Toolchain{Required: "1.27.0", Declared: "1.26.3", Outcome: ToolchainRaised}},
			toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, so go.toolchain_min is raised from 1.26.3, but the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete", model.Introduced, true)},
		{"the minimum gating on it, as the check's assessment found it",
			Input{Port: pinnedGoPort("1.27.0", "1.26"), Base: pinnedGoPort("1.26.3", "1.26"), Pairs: goMods(t, "1.26.3", "1.27.0")},
			toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, but the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete", model.Introduced, true)},
		{"pinned by the dependency alone",
			Input{Port: byDependency, Base: byDependency, Pairs: goMods(t, "1.26.3", "1.27.0")},
			toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, but the Portfile pins go-1.26 (depends_build); the pin may be obsolete", model.Introduced, true)},
		{"the minimum below it too",
			Input{Port: pinnedGoPort("1.25", "1.26"), Base: pinnedGoPort("1.25", "1.26"), Pairs: goMods(t, "1.26.3", "1.27.0")},
			toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, above go.toolchain_min 1.25, which doesn't gate on it; and the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete", model.Introduced, true)},
		{"the base unread",
			Input{Port: pinnedGoPort("1.27.0", "1.26"), Base: pinnedGoPort("1.26.3", "1.26"), Toolchain: &Toolchain{Required: "1.27.0"}},
			toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, but the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete", model.UnknownBaseline, true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, []model.UpstreamChange{test.want}, Assess(test.input).Changes)
		})
	}
}

// A pin that meets what go.mod requires adds nothing to what's said of the
// minimum: go-1.27 for 1.27.0, and go-1.26 for 1.26.8, since a pin is
// judged by its series, as a minimum is.
func TestAGoPinMeetingTheRequirementSaysNothingMore(t *testing.T) {
	changes := Assess(Input{Port: pinnedGoPort("1.27.0", "1.27"), Base: pinnedGoPort("1.26.3", "1.26"), Pairs: goMods(t, "1.26.3", "1.27.0")}).Changes
	require.Equal(t, []model.UpstreamChange{toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, which go.toolchain_min 1.27.0 already gates on", model.Introduced, false)}, changes)
	changes = Assess(Input{Port: pinnedGoPort("1.26", "1.26"), Base: pinnedGoPort("1.26", "1.26"), Pairs: goMods(t, "1.26.3", "1.26.8")}).Changes
	require.Equal(t, []model.UpstreamChange{toolchainFinding("1.26.8", "upstream: go.mod requires Go 1.26.8, which go.toolchain_min 1.26 already gates on", model.Introduced, false)}, changes)
}

// A pin the base had, which already didn't meet the base's go.mod, is
// said and holds nothing, since an update isn't an audit of everything the
// port already was; with a minimum the base's didn't gate on either, both
// are. A pin changed since the base is the candidate's own, and holds.
func TestAGoPinAlreadyBehindAtTheBaseIsSaidAndHoldsNothing(t *testing.T) {
	for _, test := range []struct {
		name  string
		input Input
		want  model.UpstreamChange
	}{
		{"the same pin",
			Input{Port: pinnedGoPort("1.27.0", "1.26"), Base: pinnedGoPort("1.27.0", "1.26"), Pairs: goMods(t, "1.27.0", "1.27.1")},
			toolchainFinding("1.27.1", "upstream: go.mod requires Go 1.27.1, but the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete; the base's pin didn't meet its go.mod either", model.Present, false)},
		{"the same pin, and a minimum below at the base too",
			Input{Port: pinnedGoPort("1.25", "1.26"), Base: pinnedGoPort("1.25", "1.26"), Pairs: goMods(t, "1.27.0", "1.27.0")},
			toolchainFinding("1.27.0", "upstream: go.mod requires Go 1.27.0, above go.toolchain_min 1.25, which doesn't gate on it; the base's didn't gate on it either; and the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete; the base's pin didn't meet its go.mod either", model.Present, false)},
		{"a pin changed since",
			Input{Port: pinnedGoPort("1.27.0", "1.26"), Base: pinnedGoPort("1.27.0", "1.25"), Pairs: goMods(t, "1.27.0", "1.27.1")},
			toolchainFinding("1.27.1", "upstream: go.mod requires Go 1.27.1, but the Portfile pins go-1.26 (go.bin, depends_build); the pin may be obsolete", model.Introduced, true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, []model.UpstreamChange{test.want}, Assess(test.input).Changes)
		})
	}
}
