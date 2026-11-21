package ghactions

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/github"
)

type accountIdentity string

func (a accountIdentity) Current(context.Context) (github.Identity, error) {
	return github.Identity{Account: string(a)}, nil
}

type blockedIdentity struct{ release <-chan struct{} }

func (b blockedIdentity) Current(context.Context) (github.Identity, error) {
	<-b.release
	return github.Identity{Account: "ada"}, nil
}

func releaseAfter(d time.Duration) (<-chan struct{}, func()) {
	ch := make(chan struct{})
	timer := time.AfterFunc(d, func() { close(ch) })
	return ch, func() {
		if timer.Stop() {
			close(ch)
		}
	}
}
