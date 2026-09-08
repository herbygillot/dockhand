package git

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bool is the whole reason this verb exists rather than a bare
// read: git prints nothing for a key nobody set, and an empty value is
// a value a person may have chosen. A caller that could not tell them
// apart would rewrite the second while meaning to fill in the first.
func TestConfigTellsAnUnsetKeyFromAnEmptyValue(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	_, set, err := r.Config(ctx, "dockhand.unset")
	require.NoError(t, err)
	assert.False(t, set, "nothing sets this key")

	plant(t, r, "config", "dockhand.probe", "")
	value, set, err := r.Config(ctx, "dockhand.probe")
	require.NoError(t, err)
	assert.True(t, set, "a key set to the empty string is set")
	assert.Empty(t, value)
}

// SetConfig writes the repository's own config, which is where a
// setting about this checkout's refs belongs — never the person's
// global one.
func TestSetConfigWritesTheValueThisRepositoryReadsBack(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	require.NoError(t, r.SetConfig(ctx, "core.logAllRefUpdates", "always"))
	value, set, err := r.Config(ctx, "core.logAllRefUpdates")
	require.NoError(t, err)
	require.True(t, set)
	assert.Equal(t, "always", value)
}
