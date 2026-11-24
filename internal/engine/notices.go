package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Notice is one thing a branch's updates raised that a person should
// read: what an update found and left, or what its upstream comparison
// says, by port; Holds is one that holds the branch, apart from one that
// only informs.
type Notice struct {
	Port  string
	Words string
	Holds bool
}

// Notices are what the branch's updates raised, read from their records,
// so a restart lists them as the run that made them did (#194, N1; #180's
// first part): each written edit's findings it left, and its upstream
// comparison's changes, a new -sys crate's among them. A patch rejection
// holds where it still stands (StandingRejections); a comparison's change
// holds as it says. An edit from before has no result, and says nothing.
func (e *Engine) Notices(ctx context.Context, branch model.Branch) ([]Notice, error) {
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return nil, err
	}
	standing := map[string]bool{}
	for _, edit := range edits {
		if _, seen := standing[edit.Port]; !seen {
			standing[edit.Port] = len(branchstate.StandingRejections(edit.Port, edits)) > 0
		}
	}
	var notices []Notice
	seen := map[Notice]bool{}
	add := func(notice Notice) {
		if !seen[notice] {
			seen[notice] = true
			notices = append(notices, notice)
		}
	}
	for _, edit := range edits {
		if edit.Result != nil {
			for _, finding := range edit.Result.Findings {
				if finding.Fixed {
					continue
				}
				add(Notice{Port: edit.Port, Words: FindingWords(finding), Holds: finding.Kind == model.FindingPatchRejected && standing[edit.Port]})
			}
		}
		if edit.Upstream != nil {
			for _, change := range edit.Upstream.Changes {
				if change.Message != "" {
					add(Notice{Port: edit.Port, Words: change.Message, Holds: change.Hold})
				}
			}
		}
	}
	return notices, nil
}
