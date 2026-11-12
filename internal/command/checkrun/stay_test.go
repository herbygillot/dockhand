package checkrun

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// How long a command stays with a check is one value: detached and
// bounded together are refused by See itself, before it looks at the
// check, as the flags refuse them (docs/designs/check-lifecycle.md).
func TestDetachedAndBoundedAreRefusedTogether(t *testing.T) {
	t.Parallel()
	err := See(t.Context(), nil, checks.Queued{Run: model.Run{Number: 7, State: model.RunQueued}}, term.Streams{}, config.Places{}, Stay{Detach: true, For: Bound{For: time.Minute, Said: "1m"}}, nil)
	require.ErrorContains(t, err, "-d and --for can't be used together")
}
