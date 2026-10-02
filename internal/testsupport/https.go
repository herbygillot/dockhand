package testsupport

import "context"

// HTTPSAnswers stands in for asking URLs over HTTPS: those it holds true
// answer, and no test asks the network.
type HTTPSAnswers map[string]bool

func (a HTTPSAnswers) Answers(_ context.Context, url string) bool { return a[url] }
