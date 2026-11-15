//go:build !surveymeasure

package eval

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductCannotSelectStandinAnswers(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .GoFiles `\n`}}", ".").CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "perturbation_product.go")
	require.NotContains(t, string(out), "perturbation_survey.go")
	require.Empty(t, surveyScript(t.Context()))
}
