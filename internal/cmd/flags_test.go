package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
)

// release looks a real release up by name for the fixtures below; the
// table is the source of truth, so a typo here fails loudly.
func release(t *testing.T, name string) platform.Release {
	t.Helper()
	r, ok := platform.ByName(name)
	require.True(t, ok, "platform table does not name %q", name)
	return r
}

// The resolver's contract, pinned where no golden reaches: the order
// of the two refusals for a mixed list, duplicates kept, and the
// difference between the verbs that need a base and the one that does
// not. The provisioned fixture leads with a release the table cannot
// parse, so "the newest" is distinguishable from anything --on could
// name, and carries one real release for the membership rows.
func TestResolveReleaseSet(t *testing.T) {
	sonoma := release(t, "sonoma")
	sequoia := release(t, "sequoia")
	testos := []platform.Release{{Name: "Testos", Darwin: 99}, sonoma}
	noBaseFor := verify.ErrNoEnvironment.Error() +
		": no base image for Sequoia; `dockhand provision tart --macos sequoia` builds one"

	for _, tc := range []struct {
		name        string
		on          []string
		provisioned []platform.Release
		requireBase bool
		want        []platform.Release
		// is pins a sentinel the error carries; not pins one it must
		// not, so a row proves which refusal won.
		is, not error
		// usage says the error is (or must not be) a *UsageError.
		usage bool
		// text, when set, is the whole error string.
		text string
	}{
		{name: "nothing provisioned, nothing asked, base required",
			requireBase: true, is: verify.ErrNoEnvironment, text: verify.ErrNoEnvironment.Error() + ": no base images"},
		{name: "nothing provisioned, nothing asked, base not required",
			is: verify.ErrNoEnvironment, text: verify.ErrNoEnvironment.Error() + ": no base images"},
		{name: "nothing asked means the newest, base required",
			provisioned: testos, requireBase: true, want: testos[:1]},
		{name: "nothing asked means the newest, base not required",
			provisioned: testos, want: testos[:1]},
		{name: "all means every base, base required",
			on: []string{"all"}, provisioned: testos, requireBase: true, want: testos},
		{name: "all means every base, base not required",
			on: []string{"all"}, provisioned: testos, want: testos},
		{name: "all is folded",
			on: []string{"ALL"}, provisioned: testos, want: testos},
		{name: "all of nothing is nothing when no base is required",
			on: []string{"all"}, want: nil},
		{name: "all of nothing is refused when a base is required",
			on: []string{"all"}, requireBase: true, is: verify.ErrNoEnvironment},
		{name: "a release without a base is refused when required",
			on: []string{"sequoia"}, provisioned: testos, requireBase: true,
			is: verify.ErrNoEnvironment, text: noBaseFor},
		{name: "a release without a base is taken as given otherwise",
			on: []string{"sequoia"}, provisioned: testos, want: []platform.Release{sequoia}},
		{name: "an unknown release is a usage error, base required",
			on: []string{"cheetah"}, provisioned: testos, requireBase: true,
			is: platform.ErrUnknownRelease, usage: true},
		{name: "an unknown release is a usage error, base not required",
			on: []string{"cheetah"}, provisioned: testos,
			is: platform.ErrUnknownRelease, usage: true},
		{name: "unprovisioned before unknown: the missing base wins",
			on: []string{"sequoia", "cheetah"}, provisioned: testos, requireBase: true,
			is: verify.ErrNoEnvironment, not: platform.ErrUnknownRelease},
		{name: "unknown before unprovisioned: the unknown name wins",
			on: []string{"cheetah", "sequoia"}, provisioned: testos, requireBase: true,
			is: platform.ErrUnknownRelease, not: verify.ErrNoEnvironment, usage: true},
		{name: "duplicates are kept, in order",
			on: []string{"sonoma", "Sonoma"}, provisioned: testos, requireBase: true,
			want: []platform.Release{sonoma, sonoma}},
		{name: "the order given is the order returned",
			on: []string{"sequoia", "sonoma"}, provisioned: testos,
			want: []platform.Release{sequoia, sonoma}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveReleaseSet(tc.on, tc.provisioned, tc.requireBase)
			var usage *UsageError
			if tc.is == nil {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				return
			}
			require.ErrorIs(t, err, tc.is)
			assert.Nil(t, got)
			if tc.not != nil {
				require.NotErrorIs(t, err, tc.not)
			}
			if tc.usage {
				require.ErrorAs(t, err, &usage)
			} else {
				require.NotErrorAs(t, err, &usage)
			}
			if tc.text != "" {
				require.EqualError(t, err, tc.text)
			}
		})
	}
}

// parseRelease interprets nothing: "all" and "" are as unknown as
// "cheetah", and each is the invocation's fault.
func TestParseRelease(t *testing.T) {
	for _, in := range []string{"cheetah", "all", ""} {
		t.Run("unknown "+in, func(t *testing.T) {
			_, err := parseRelease(in)
			require.ErrorIs(t, err, platform.ErrUnknownRelease)
			var usage *UsageError
			require.ErrorAs(t, err, &usage)
		})
	}
	r, err := parseRelease("sequoia")
	require.NoError(t, err)
	assert.Equal(t, release(t, "sequoia"), r)
	r, err = parseRelease("14")
	require.NoError(t, err)
	assert.Equal(t, release(t, "sonoma"), r)
}

// modernReleases is the table's Darwin 21+ span in the table's own
// order — the provision sweep and the Xcode needs table print in it.
func TestModernReleases(t *testing.T) {
	var want []platform.Release
	for _, r := range platform.Releases {
		if r.Darwin >= 21 {
			want = append(want, r)
		}
	}
	got := modernReleases()
	require.Equal(t, want, got)
	require.NotEmpty(t, got)
	assert.Equal(t, "Monterey", got[0].Name)
	assert.Equal(t, "Tahoe", got[len(got)-1].Name)
}
