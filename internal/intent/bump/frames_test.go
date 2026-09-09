package bump

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

// twoBranch answers as a port with two branches does: modern systems
// take the first, older ones the second. `pinned` says whether the older
// branch holds its own release — LyX and cliclick do — or names itself
// after the port's version, as gh's binary branch does.
func twoBranch(pinned bool) framer {
	return func(_ context.Context, p info.Platform, src []byte) (info.Values, error) {
		v := "5.0.1"
		if src != nil && strings.Contains(string(src), "5.0.2") {
			v = "5.0.2"
		}
		if p.Major >= 19 {
			return vals("modern-" + v + ".tar.gz"), nil
		}
		if pinned {
			return vals("legacy-4.0.1.tar.gz"), nil
		}
		return vals("legacy-" + v + ".zip"), nil
	}
}

func vals(distfiles ...string) info.Values {
	return info.Values{Semantic: info.Semantic{Distfiles: distfiles}}
}

// the edit a bump of 5.0.1 -> 5.0.2 would write
func theEdit() ([]byte, []edit.Edit) {
	src := []byte("version 5.0.1\n")
	return src, []edit.Edit{{Kind: edit.Version, Start: 8, End: 13, Old: "5.0.1", New: "5.0.2", Reason: "version"}}
}

// A BRANCH THAT FETCHES THE SAME FILE AFTER THE EDIT IS NOT STALE, and
// that is measured rather than read off a filename. LyX pins 2.3.8 in
// two of its three branches and cliclick holds 4.0.1 in its legacy one;
// applying a bump of the current line moves neither.
func TestAPinnedBranchIsNotStale(t *testing.T) {
	src, edits := theEdit()
	frame, stale := staleElsewhere(t.Context(), twoBranch(true), vals("modern-5.0.1.tar.gz"), src, edits)
	assert.False(t, stale)
	assert.Empty(t, frame)
}

// AND ONE WHOSE FETCH MOVES IS. gh's binary branch names itself after
// ${version}: the edit renames the file its untouched digests describe.
// The refusal names the frame that showed it.
func TestABranchWhoseFetchMovesIsStale(t *testing.T) {
	src, edits := theEdit()
	frame, stale := staleElsewhere(t.Context(), twoBranch(false), vals("modern-5.0.1.tar.gz"), src, edits)
	assert.True(t, stale)
	assert.NotEmpty(t, frame, "a refusal that cannot name where it looked is not actionable")
}

// THE NAME WAS NEVER THE QUESTION. A first version of this asked whether
// the other branch's distfile CONTAINED the version being moved, which
// would clear this port wrongly: its legacy branch fetches a file whose
// name never changes and whose content follows the version, so the
// digests go stale under a name that stayed put.
func TestAMovingFetchUnderAnUnchangingNameIsStale(t *testing.T) {
	src, edits := theEdit()
	rolling := func(_ context.Context, p info.Platform, s []byte) (info.Values, error) {
		if p.Major >= 19 {
			return vals("modern-5.0.1.tar.gz"), nil
		}
		if s != nil && strings.Contains(string(s), "5.0.2") {
			return vals("legacy-latest.tar.gz", "extra-5.0.2.patch"), nil
		}
		return vals("legacy-latest.tar.gz"), nil
	}
	_, stale := staleElsewhere(t.Context(), rolling, vals("modern-5.0.1.tar.gz"), src, edits)
	assert.True(t, stale, "the fetch moved; that the name did not is beside the point")
}

// NO FRAMES, NO REPRIEVE — and neither does an enumeration that learned
// nothing. This may only ever remove a refusal it has earned.
func TestWithoutEvidenceTheRefusalStands(t *testing.T) {
	src, edits := theEdit()
	_, stale := staleElsewhere(t.Context(), nil, vals("modern-5.0.1.tar.gz"), src, edits)
	assert.True(t, stale)

	same := func(_ context.Context, _ info.Platform, _ []byte) (info.Values, error) {
		return vals("modern-5.0.1.tar.gz"), nil
	}
	_, stale = staleElsewhere(t.Context(), same, vals("modern-5.0.1.tar.gz"), src, edits)
	assert.True(t, stale, "every frame took the branch already accounted for")
}
