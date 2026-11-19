package ghactions

import (
	"context"

	"github.com/herbygillot/dockhand/internal/github"
)

type accountIdentity string

func (a accountIdentity) Current(context.Context) (github.Identity, error) {
	return github.Identity{Account: string(a)}, nil
}
