package testsupport

import "context"

// HTTPSAnswers stands in for asking URLs over HTTPS: those it holds true
// answer, and no test asks the network. It's an engine's probe
// (engine.HTTPSProbe) and an edit's answers (portedit.HTTPSAnswers).
type HTTPSAnswers map[string]bool

// Ask has the answer at once, so starts nothing.
func (a HTTPSAnswers) Ask(context.Context, string) {}

func (a HTTPSAnswers) Answers(_ context.Context, url string) bool { return a[url] }
