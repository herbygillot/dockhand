package cargo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// Blocks is the cargo family's Regenerator: cargo.crates and
// cargo.crates_github regenerated together from the Cargo.lock inside
// the new distfile — the lock rules on which sources are registry and
// which are git, so the two blocks always describe one crate set. The
// one veto left is a patch over the lockfile.
type Blocks struct{}

var _ vendored.Regenerator = Blocks{}

func (Blocks) Kind() vendored.Kind { return vendored.CargoCrates }

func (Blocks) Present(vals info.Values) bool {
	return vals.Vendored.CargoCrates != "" || vals.Vendored.CargoCratesGithub != ""
}

// Veto refuses the one way cargo regeneration would be dishonest,
// judged before any network: a patch over the lockfile — the built
// crate set is then not the one upstream shipped, so regenerating
// from the distfile's copy would state something untrue. Both blocks
// regenerate from the new lock together, so the git form is not a
// veto.
func (Blocks) Veto(vals info.Values) (string, bool) {
	if pf, ok := patchesLockfile(vals); ok {
		return fmt.Sprintf("%s rewrites %s, so the built crate set is not the one upstream shipped", pf, LockName), true
	}
	return "", false
}

func (Blocks) Supplied(_ context.Context, rc vendored.Regen) ([]string, error) {
	supplied, err := SuppliedIn(rc.Vals.Vendored.CargoCrates)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}
	git, err := GithubSuppliedIn(rc.Vals.Vendored.CargoCratesGithub)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}
	return append(supplied, git...), nil
}

// Regenerate rebuilds the block from the lockfile inside the distfile
// just fetched, so the crate set and the checksum recorded for that
// distfile describe the same bytes — re-laid under the existing
// block's proven geometry when Assess can prove one.
func (Blocks) Regenerate(ctx context.Context, rc vendored.Regen) ([]edit.Edit, error) {
	span, err := vendored.Locate(rc.Src, rc.CST, portstyle.ScopeOf(rc.Src, rc.Vals.Name), vendored.CargoCrates)
	if err != nil {
		return nil, err
	}
	lock, from, err := Lockfile(ctx, rc.Tools, rc.Fetched, rc.ShadowVals.Worksrcdir)
	if err != nil {
		return nil, err
	}
	slog.Debug("read lockfile", "from", filepath.Base(from), "bytes", len(lock))
	// The lock rules on git sources before anything regenerates: a git
	// dependency the block format cannot express declines here, by
	// name — never a complete-looking branch that cannot build.
	git, err := gitCrates(lock)
	if err != nil {
		return nil, err
	}
	geom, proven := Assess(span.Text(rc.Src))
	slog.Debug("assessed block layout", "layout", string(geom.Layout), "proven", proven)
	block, err := Generate(ctx, rc.Tools, rc.TempDir, lock, geom.Layout)
	if err != nil {
		return nil, err
	}
	if proven {
		block = Reformat(block, geom)
	}
	slog.Debug("regenerated block", "kind", vendored.CargoCrates.String(), "bytes", len(block))
	edits := []edit.Edit{vendored.Edit(rc.Src, span, block, vendored.CargoCrates)}
	ghEdits, err := githubEdits(ctx, rc, span, git)
	if err != nil {
		return nil, err
	}
	return append(edits, ghEdits...), nil
}

// patchesLockfile reports whether any of the port's patchfiles rewrites
// the file the generator reads. The patches' own diff headers are read
// rather than their names guessed at: a patch named for something else
// can still touch the lockfile. A patchfile that cannot be read is
// treated as touching it — the point is to prove the lockfile is
// untouched, and an unreadable patch proves nothing.
func patchesLockfile(vals info.Values) (string, bool) {
	for _, pf := range vals.Patchfiles {
		body, err := os.ReadFile(filepath.Join(vals.Filespath, pf))
		if err != nil {
			return pf, true
		}
		for line := range strings.Lines(string(body)) {
			if !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") {
				continue
			}
			if strings.Contains(line, LockName) {
				return pf, true
			}
		}
	}
	return "", false
}
