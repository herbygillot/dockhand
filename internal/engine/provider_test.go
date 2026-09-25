package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// --on names providers that are set up, each once, with releases only
// where the provider can build on them; with none, the command provider
// when there is one.
func TestEnvironmentsAreTheProvidersOnNames(t *testing.T) {
	e := &Engine{Providers: map[string]Provider{"command": &scriptedProvider{}, "github": &scriptedProvider{}}}
	environments, err := e.Environments(nil)
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "command"}}, environments)
	environments, err = e.Environments([]string{"github", "command", "github"})
	require.NoError(t, err)
	require.Equal(t, []model.Environment{{Provider: "github"}, {Provider: "command"}}, environments)

	for on, refusal := range map[string]string{
		"tart:sonoma": "the tart provider is not in v3 yet",
		"nosuch":      `no provider "nosuch" is set up`,
		"github:15":   "the github provider builds on the runners MacPorts' workflow names",
		"command:15":  "the command provider builds wherever its script does",
	} {
		_, err := e.Environments([]string{on})
		require.ErrorContains(t, err, refusal, on)
	}
	_, err = (&Engine{}).Environments(nil)
	require.ErrorContains(t, err, "a check needs somewhere to build")
}
