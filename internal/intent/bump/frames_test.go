package bump

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// fetched is a frame's answer: where from, and what called.
func fetched(site string, distfiles ...string) intent.FrameFetch {
	return intent.FrameFetch{Sites: []string{site}, Distfiles: distfiles}
}

// here is the branch this evaluation took, in every case below.
var here = fetched("https://example.invalid/5.0.1", "modern-5.0.1.tar.gz")

// theEdit is the edit a bump of 5.0.1 -> 5.0.2 would write.
func theEdit() ([]byte, []edit.Edit) {
	src := []byte("version 5.0.1\n")
	return src, []edit.Edit{{Kind: edit.Version, Start: 8, End: 13, Old: "5.0.1", New: "5.0.2", Reason: "version"}}
}

// moved says whether the edited source is in play, which is how these
// fakes stand in for a Portfile whose branch reads ${version}.
func moved(src []byte) string {
	if src != nil && strings.Contains(string(src), "5.0.2") {
		return "5.0.2"
	}
	return "5.0.1"
}

// twoBranch is a port whose older systems take a second branch. pinned
// says whether that branch holds its own release — LyX and cliclick do —
// or names itself after the port's version, as gh's binary branch does.
func twoBranch(pinned bool) framer {
	return func(_ context.Context, p info.Platform, src []byte) (intent.FrameFetch, error) {
		v := moved(src)
		switch {
		case p.Major >= 19:
			return fetched("https://example.invalid/"+v, "modern-"+v+".tar.gz"), nil
		case pinned:
			return fetched("https://example.invalid/4.0.1", "legacy-4.0.1.tar.gz"), nil
		default:
			return fetched("https://example.invalid/"+v, "legacy-"+v+".zip"), nil
		}
	}
}

// A BRANCH THAT FETCHES THE SAME THING AFTER THE EDIT IS NOT STALE, and
// that is measured rather than read off a filename. LyX pins 2.3.8 in two
// of its three branches and cliclick holds 4.0.1 in its legacy one;
// applying a bump of the current line moves neither.
func TestAPinnedBranchIsNotStale(t *testing.T) {
	src, edits := theEdit()
	frame, stale := staleElsewhere(t.Context(), twoBranch(true), here, src, edits)
	assert.False(t, stale)
	assert.Empty(t, frame)
}

// AND ONE WHOSE FETCH MOVES IS. gh's binary branch names itself after
// ${version}: the edit renames the file its untouched digests describe.
// The refusal names the frame that showed it, which is the thing a person
// can go and reproduce.
func TestABranchWhoseFetchMovesIsStale(t *testing.T) {
	src, edits := theEdit()
	frame, stale := staleElsewhere(t.Context(), twoBranch(false), here, src, edits)
	assert.True(t, stale)
	assert.NotEmpty(t, frame, "a refusal that cannot name where it looked is not actionable")
}

// THE FILENAME IS HALF THE ANSWER AT MOST. claude-code serves one binary
// called `claude` from .../darwin-arm64/ and .../darwin-x64/, with the
// version in the URL and not in the name. Comparing distfiles alone
// reports its two branches as identical, learns nothing about either, and
// clears a port whose digests really do go stale — which is why
// FrameFetch.Same asks about the sites too.
func TestAMovingSiteUnderAnUnchangingFilenameIsStale(t *testing.T) {
	src, edits := theEdit()
	rolling := func(_ context.Context, p info.Platform, s []byte) (intent.FrameFetch, error) {
		if p.Major >= 19 {
			return here, nil
		}
		return fetched("https://example.invalid/legacy/"+moved(s), "claude"), nil
	}
	frame, stale := staleElsewhere(t.Context(), rolling, here, src, edits)
	assert.True(t, stale, "the SITE moved though the filename did not")
	assert.NotEmpty(t, frame)
}

// NO EVIDENCE, NO REPRIEVE. A road with no evaluator pool cannot tell a
// pinned branch from a moving one, and neither can an enumeration in
// which every frame took the branch already accounted for. Both fall back
// to the refusal that shipped: this may only ever remove one it has
// earned the right to remove.
func TestWithoutEvidenceTheRefusalStands(t *testing.T) {
	src, edits := theEdit()

	_, stale := staleElsewhere(t.Context(), nil, here, src, edits)
	assert.True(t, stale, "no frames at all")

	same := func(_ context.Context, _ info.Platform, _ []byte) (intent.FrameFetch, error) {
		return here, nil
	}
	_, stale = staleElsewhere(t.Context(), same, here, src, edits)
	assert.True(t, stale, "every frame took the branch already accounted for")

	blank := func(_ context.Context, _ info.Platform, _ []byte) (intent.FrameFetch, error) {
		return intent.FrameFetch{}, nil
	}
	_, stale = staleElsewhere(t.Context(), blank, here, src, edits)
	assert.True(t, stale, "a frame that fetches nothing told us nothing")
}
