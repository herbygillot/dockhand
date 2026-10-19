package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/config"
)

// Each rule config takes a profile's releases by is one of dockhand's own
// profiles, read from config's one vocabulary (the architecture review of
// 5897a82, R3-16).
func TestEveryRuleConfigTakesIsADockhandProfile(t *testing.T) {
	t.Parallel()
	for name, rule := range map[string]config.ReleaseRule{string(config.RuleHost): config.RuleHost, config.ProfileLocal: config.RuleHost, string(config.RuleMinMax): config.RuleMinMax, string(config.RuleMacPortsCI): config.RuleMacPortsCI, string(config.RuleAll): config.RuleAll} {
		profile, ok := builtinProfile(name)
		require.True(t, ok, name)
		require.Equal(t, buildenv.Tart, profile.Provider, name)
		require.Equal(t, rule, profile.Rule, name)
	}
}
