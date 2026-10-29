package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// prefixes stands for the local provider: --on local:<prefix> names a
// prefix, and its changes are what it says.
type prefixes struct {
	scriptedProvider
	changes []buildenv.Change
}

func (*prefixes) Environments(_ context.Context, prefix string) ([]model.Environment, error) {
	if prefix == "" {
		prefix = "/opt/local"
	}
	return []model.Environment{{Provider: buildenv.Local, Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, Prefix: prefix}}, nil
}

func (p *prefixes) Changes(context.Context) ([]buildenv.Change, error) { return p.changes, nil }

// --on local builds in the configured prefix, and local:<prefix> in
// another; several mean all must pass, each its own environment.
func TestOnLocalNamesAPrefix(t *testing.T) {
	t.Parallel()
	e := &Engine{Providers: map[string]buildenv.Provider{buildenv.Local: &prefixes{}}}
	environments, err := e.Environments(t.Context(), []string{"local", "local:/opt/macports-test", "local:/opt/local"})
	require.NoError(t, err)
	require.Equal(t, []string{"/opt/local", "/opt/macports-test"}, []string{environments[0].Prefix, environments[1].Prefix})
	values, ok := OnValues(environments)
	require.True(t, ok)
	require.Equal(t, []string{"local:/opt/local", "local:/opt/macports-test"}, values)
	require.Equal(t, "local:/opt/macports-test macOS 26 (Tahoe) arm64", DescribeEnvironment(environments[1]))
	require.Equal(t, "/opt/macports-test: MacPorts' binary archives are built for /opt/local alone, so its dependencies build from source", PrefixNote(environments[1]))
	require.Empty(t, PrefixNote(environments[0]))
}

// A check in a prefix on this Mac runs its Portfiles as root there, so
// it's refused for a pull request someone else wrote, which the person
// adopted, and for serve, unless [providers.local] serve says it may.
func TestALocalCheckIsRefusedWhereItsPortfilesArentTheirs(t *testing.T) {
	t.Parallel()
	local := model.Plan{Environments: []model.Environment{{Provider: buildenv.Local, Prefix: "/opt/local"}}}
	adopted := model.Branch{Name: "dockhand/jq-4f2a", PullRequest: &model.PullRequest{Number: 35968, Adopted: true}}
	e := &Engine{}
	require.ErrorContains(t, e.localAllowed(QueueRequest{Branch: adopted, Plan: local}), "is a pull request you adopted, whose Portfiles someone else wrote. A local check would run them as root on this Mac; check it with --on tart or --on github instead")
	require.NoError(t, e.localAllowed(QueueRequest{Branch: adopted, Plan: model.Plan{Environments: []model.Environment{{Provider: buildenv.Tart}}}}))
	require.ErrorContains(t, e.localAllowed(QueueRequest{Plan: local, Origin: model.OriginServe}), "unless [providers.local] serve = true")
	e.LocalServe = true
	require.NoError(t, e.localAllowed(QueueRequest{Plan: local, Origin: model.OriginServe}))
	require.NoError(t, e.localAllowed(QueueRequest{Plan: local, Origin: model.OriginPerson}))
}

// What checks changed of the person's own is gathered from each provider
// that changes something, for status to say.
func TestChangesAreGatheredFromTheProviders(t *testing.T) {
	t.Parallel()
	left := buildenv.Change{Check: "check-9", Place: "/opt/local", Remedy: "dockhand undo check-9"}
	e := &Engine{Providers: map[string]buildenv.Provider{buildenv.Local: &prefixes{changes: []buildenv.Change{left}}, "command": &scriptedProvider{}}}
	require.Equal(t, []buildenv.Change{left}, e.Changes(t.Context()))
}
