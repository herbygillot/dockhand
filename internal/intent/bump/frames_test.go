package bump

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/macports/info"
)

// framed answers as a port with two branches would: the one this host
// took, and one other.
func framed(other []string) func(context.Context, info.Platform) (info.Values, error) {
	return func(_ context.Context, p info.Platform) (info.Values, error) {
		if p.Major >= 19 {
			return info.Values{Semantic: info.Semantic{Distfiles: []string{"here-5.0.1.tar.gz"}}}, nil
		}
		return info.Values{Semantic: info.Semantic{Distfiles: other}}, nil
	}
}

// A BRANCH THAT PINS ITS OWN RELEASE IS NOT STALE. cliclick holds
// cliclick-4.0.1 for the systems the 5.x line dropped, beside its own
// 32-bit and c89 patches. A bump of 5.0.1 leaves it exactly as it should,
// and refusing the port for that would be refusing it for being right.
func TestAPinnedBranchDoesNotMakeABumpStale(t *testing.T) {
	here := info.Values{Semantic: info.Semantic{Distfiles: []string{"here-5.0.1.tar.gz"}}}
	assert.False(t, tracksVersion(t.Context(), framed([]string{"legacy-4.0.1.tar.gz"}), here, "5.0.1"),
		"the other branch fetches 4.0.1; moving 5.0.1 cannot stale it")
}

// AND A BRANCH NAMED FOR THE VERSION IS. gh's binary branch is
// gh_${version}_macOS_amd64.zip: a bump renames the file and leaves the
// digests of the release before under it, so the port fails its checksum
// on every system that takes that branch.
func TestABranchNamedForTheVersionIsStale(t *testing.T) {
	here := info.Values{Semantic: info.Semantic{Distfiles: []string{"here-5.0.1.tar.gz"}}}
	assert.True(t, tracksVersion(t.Context(), framed([]string{"gh_5.0.1_macOS_amd64.zip"}), here, "5.0.1"))
}

// NO FRAMES, NO REPRIEVE. A road with no evaluator pool to spend cannot
// tell the two apart, and the honest answer where a defect cannot be
// ruled out is the refusal that shipped. The refinement only ever
// removes a refusal it has earned the right to remove.
func TestWithoutFramesTheRefusalStands(t *testing.T) {
	here := info.Values{Semantic: info.Semantic{Distfiles: []string{"here-5.0.1.tar.gz"}}}
	assert.True(t, tracksVersion(t.Context(), nil, here, "5.0.1"))
	assert.True(t, tracksVersion(t.Context(), framed([]string{"legacy-4.0.1.tar.gz"}), here, ""),
		"no version to look for is not evidence of safety")

	// Every frame produced what this one did: the enumeration learned
	// nothing, so it concedes to the refusal rather than clearing it.
	same := func(_ context.Context, _ info.Platform) (info.Values, error) {
		return here, nil
	}
	assert.True(t, tracksVersion(t.Context(), same, here, "5.0.1"))
}
