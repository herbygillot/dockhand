//go:build !surveymeasure

package observe

import "context"

// Product builds contain no off policy or way to select one.
func followValue(_ context.Context, sources [][]byte, option string, base *baseCode) ([]string, bool) {
	return followedReadsBenign(sources, option, base)
}
