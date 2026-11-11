package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/progress"
)

// The door: the person's own work passes unsaid; another's allowed passes,
// said once; another's refused stops before any tree is staged, Open and
// Acquire each asking the door first (#162).
func TestTheDoorAdmitsOnlyWhatThePersonAllowed(t *testing.T) {
	t.Parallel()
	var said []string
	ctx := progress.WithReporter(context.Background(), func(update progress.Update) { said = append(said, update.Message) })
	require.NoError(t, admit(ctx))
	require.Empty(t, said)

	allowed := WithOwner(ctx, Owner{Whose: "@alice's", Of: "#35174"})
	require.NoError(t, admit(allowed))
	require.NoError(t, admit(allowed))
	require.Equal(t, []string{"Evaluating @alice's Portfiles on this Mac, for #35174"}, said, "once")

	refused := errors.New("not allowed")
	stopped := WithOwner(ctx, Owner{Whose: "@alice's", Of: "#35174", Refused: refused})
	require.ErrorIs(t, admit(stopped), refused)
}
