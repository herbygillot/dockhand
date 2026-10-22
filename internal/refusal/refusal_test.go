package refusal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A refusal is reached through wrapping, by its kind, never its words; its
// kind decides its exit code and whether another try may meet it.
func TestARefusalIsReachedByItsKind(t *testing.T) {
	cause := errors.New("dial tcp: connection reset by peer")
	r := Wrap(Network, cause, "couldn't reach GitHub (%v)", cause).With("nothing was changed")
	wrapped := fmt.Errorf("fetching master: %w", r)
	require.Equal(t, Network, KindOf(wrapped))
	require.True(t, Transient(wrapped))
	require.ErrorIs(t, wrapped, cause)
	require.Equal(t, "couldn't reach GitHub (dial tcp: connection reset by peer)\nKept: nothing was changed.", r.Error())
	require.Equal(t, Failed, KindOf(errors.New("plain")))
	require.False(t, Transient(errors.New("plain")))
	require.Equal(t, 3, Attention.Exit())
	require.Equal(t, 2, CheckFailed.Exit())
	require.Equal(t, 1, Auth.Exit())
	require.Panics(t, func() { New("", "no kind") })
	require.True(t, Transient(errors.Join(errors.New("a"), New(UpstreamNotYet, "mirrors behind"))))
}

// Of failures joined, one another try may meet makes the whole worth one,
// whichever comes first: upstream's 404 beside a mirror's 502 is tried
// again (R3-6). A refusal's steps move on where any of them does.
func TestJoinedFailuresAreWorthAnotherTryWhereOneIs(t *testing.T) {
	t.Parallel()
	missing := Wrap(UpstreamMissing, errors.New("404"), "archives: not found")
	behind := New(Network, "mirror: 502")
	joined := fmt.Errorf("archives couldn't be fetched: %w", errors.Join(missing, behind))
	require.Equal(t, Network, KindOf(joined))
	require.True(t, Transient(joined))
	require.Equal(t, UpstreamMissing, KindOf(errors.Join(missing, errors.New("other"))))
	require.False(t, Transient(errors.Join(missing, errors.New("other"))))
	require.Equal(t, Failed, KindOf(errors.New("plain")))

	edit := New(Unsupported, "could not update jq automatically").
		Then(Next{External: "run port checksum jq"}, Next{Verb: VerbEdit, Args: []string{"jq"}})
	require.True(t, edit.Advances("update"))
	again := New(Failed, "x").Then(Next{Verb: VerbSubmit})
	require.False(t, again.Advances(string(VerbSubmit)))
}

// A particular of a sentinel is it, and its kind, and says the sentinel's
// words first; its own words are read without them (R3-38, where they
// were cut off the error).
func TestAParticularIsItsSentinelInItsOwnWords(t *testing.T) {
	t.Parallel()
	unsupported := New(Unsupported, "portfile: unsupported source edit")
	cause := errors.New("eval'd")
	err := fmt.Errorf("baseline: %w", unsupported.Because("cargo.update can't be read as yes or no (%w)", cause))
	require.ErrorIs(t, err, unsupported)
	require.ErrorIs(t, err, cause)
	require.Equal(t, Unsupported, KindOf(err))
	require.EqualError(t, err, "baseline: portfile: unsupported source edit: cargo.update can't be read as yes or no (eval'd)")
	require.Equal(t, "cargo.update can't be read as yes or no (eval'd)", WordsOf(err))
	require.Equal(t, "plain", WordsOf(errors.New("plain")))
	require.True(t, CheckFailed.Outcome())
	require.False(t, Network.Outcome())
}
