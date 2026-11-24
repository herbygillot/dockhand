package ship

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// unreadAfterBuild is a provider whose environment's identity reads, as
// none, until its build is done, and can't be read after it.
type unreadAfterBuild struct {
	buildenv.Provider
	built *atomic.Bool
}

func (p unreadAfterBuild) Identity(context.Context, model.Environment) (string, error) {
	if p.built.Load() {
		return "", errors.New("the image's record couldn't be read")
	}
	return "", nil
}

func (p unreadAfterBuild) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	err := p.Provider.Execute(ctx, job, build)
	p.built.Store(true)
	return err
}

// ship --yes submits as nobody attends it, so an environment whose
// identity can't be read once its check passed holds the submission, by
// identity-unread, naming the environment and the cause
// (docs/designs/check-lifecycle.md at bbb4c0b7, "Identity").
func TestShipWithYesIsHeldByAnUnreadIdentity(t *testing.T) {
	t.Parallel()
	g := twoPortWorld(t)
	built := &atomic.Bool{}
	original := cmdtest.Stands(t).Forge
	cmdtest.Stands(t).Forge = func(e *engine.Engine) engine.Forge {
		for name, provider := range e.Providers {
			e.Providers[name] = unreadAfterBuild{provider, built}
		}
		return original(e)
	}

	out, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--yes")
	require.Error(t, err, out)
	require.True(t, built.Load())
	said := cmdtest.Flat(out + errs + err.Error())
	require.Contains(t, said, "passed its check and needs review, so nothing was submitted")
	require.Contains(t, said, "could not read what the environment is now, so whether check-1's results there were built in this same environment isn't known: the image's record couldn't be read")
	require.Empty(t, g.PRs)
}
