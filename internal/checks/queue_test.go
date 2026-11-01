package checks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// A check in a prefix on this Mac runs its Portfiles as root there, so
// it's refused for a pull request someone else wrote, which the person
// adopted, and for serve, unless [providers.local] serve says it may.
func TestALocalCheckIsRefusedWhereItsPortfilesArentTheirs(t *testing.T) {
	t.Parallel()
	local := model.Plan{Environments: []model.Environment{{Provider: buildenv.Local, Prefix: "/opt/local"}}}
	adopted := model.Branch{Name: "dockhand/jq-4f2a", PullRequest: &model.PullRequest{Number: 35968, Adopted: true}}
	c := &Service{}
	require.ErrorContains(t, c.localAllowed(QueueRequest{Branch: adopted, Plan: local}), "is a pull request you adopted, whose Portfiles someone else wrote. A local check would run them as root on this Mac; check it with --on tart or --on github instead")
	require.NoError(t, c.localAllowed(QueueRequest{Branch: adopted, Plan: model.Plan{Environments: []model.Environment{{Provider: buildenv.Tart}}}}))
	require.ErrorContains(t, c.localAllowed(QueueRequest{Plan: local, Origin: model.OriginServe}), "unless [providers.local] serve = true")
	c.LocalServe = true
	require.NoError(t, c.localAllowed(QueueRequest{Plan: local, Origin: model.OriginServe}))
	require.NoError(t, c.localAllowed(QueueRequest{Plan: local, Origin: model.OriginPerson}))
}
