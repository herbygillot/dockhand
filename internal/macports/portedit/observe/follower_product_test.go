//go:build !surveymeasure

package observe

import (
	"github.com/stretchr/testify/require"
	"os/exec"
	"testing"
)

func TestTheProductBuildContainsNoFollowerOffPolicy(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", "{{join .GoFiles `\n`}}", ".")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "follower_product.go")
	require.NotContains(t, string(out), "follower_survey.go")
	require.NotContains(t, string(out), "tolerance_legacy_survey.go")
}
