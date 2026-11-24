package submit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// unreadIdentity is a provider whose environment's identity can't be read.
type unreadIdentity struct{ buildenv.Provider }

func (unreadIdentity) Identity(context.Context, model.Environment) (string, error) {
	return "", errors.New("the image's record couldn't be read")
}

// A person's own submit, resting on an environment whose identity can't
// be read now, says so and goes on: identity-unread holds only a
// submission nobody attends (docs/designs/check-lifecycle.md, "Identity").
func TestAnAttendedSubmitSaysAnUnreadIdentityAndGoesOn(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy", "--yes")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	original := cmdtest.Stands(t).Forge
	cmdtest.Stands(t).Forge = func(e *engine.Engine) engine.Forge {
		for name, provider := range e.Providers {
			e.Providers[name] = unreadIdentity{provider}
		}
		return original(e)
	}

	out, errs, err := cmdtest.Dockhand(t, "submit", "--yes")
	require.NoError(t, err, errs)
	require.Contains(t, cmdtest.Flat(out), "could not read what the environment is now, so whether check-1's results there were built in this same environment isn't known: the image's record couldn't be read")
	require.Len(t, g.PRs, 1, "it went on")
}

// holds.allow can't name identity-unread, nor can decide: what couldn't be
// checked holds a submission nobody attends whatever the configuration
// says, as source-unknown does.
func TestHoldsAllowCantNameAnUnreadIdentity(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	config := filepath.Join(w.Home, ".dockhand", "config.toml")
	kept, err := os.ReadFile(config)
	require.NoError(t, err)
	for _, holds := range []string{"[holds]\nallow = [\"identity-unread\"]\n", "[holds.ports.jq]\nallow = [\"identity-unread\"]\n"} {
		require.NoError(t, os.WriteFile(config, append(append([]byte{}, kept...), holds...), 0o644))
		_, _, err = cmdtest.Dockhand(t, "status")
		require.ErrorContains(t, err, `allow: "identity-unread" is not a hold dockhand raises or lets go`, holds)
	}
	require.NoError(t, os.WriteFile(config, kept, 0o644))
	_, _, err = cmdtest.Dockhand(t, "decide", "identity-unread", "-p", "jq", "--because", "the image is the one it was")
	require.ErrorContains(t, err, `"identity-unread" is not a hold dockhand raises`)
}
