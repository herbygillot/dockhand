//go:build !surveymeasure

package eval

import "context"

func surveyScript(context.Context) string { return "" }
