//go:build surveymeasure

package eval

import (
	"context"
	_ "embed"
)

type standinAnswers struct{}

// WithStandinAnswers selects the survey experiment for one fresh evaluator
// session. This API and its Tcl implementation do not exist in product builds.
func WithStandinAnswers(ctx context.Context) context.Context {
	return context.WithValue(ctx, standinAnswers{}, true)
}

//go:embed perturbation_survey.tcl
var perturbationScript string

func surveyScript(ctx context.Context) string {
	if ctx.Value(standinAnswers{}) == true {
		return "\n" + perturbationScript
	}
	return ""
}
