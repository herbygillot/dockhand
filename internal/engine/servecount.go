package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// settleServe is what serve's submitting pass does first, before any
// candidate (docs/designs/publication-record.md at 0e0ad767, "serve's count"): the
// first pass after the records took on the count by rows, which accounts
// for a create an older dockhand may have left; every publication serve
// recorded, settled; and every row of a create that may have landed whose
// publication has ended, read for its pull request and its head. Where
// the first pass couldn't read everything, serve opens nothing this pass.
// A served branch whose publication can't be settled is skipped, with why:
// serve submits nothing of it this pass, and the rest go on, where one
// lasting error, as a fork deleted, stopped every pass (#344).
func (e *Engine) settleServe(ctx context.Context) (opens bool, skipped map[model.BranchID]error, err error) {
	branches, err := e.OpenBranches(ctx)
	if err != nil {
		return false, nil, err
	}
	fork, forkErr := e.Fork(ctx, "")
	done, err := e.serveFirstPass(ctx, branches, fork, forkErr)
	if err != nil {
		return false, nil, err
	}
	skip := func(branch model.Branch, err error) {
		if skipped == nil {
			skipped = map[model.BranchID]error{}
		}
		skipped[branch.ID] = err
	}
	for _, branch := range branches {
		if branch.Publication == nil || !branch.Publication.Served {
			continue
		}
		worktree := e.Repo
		if branch.Worktree != "" && exists(branch.Worktree) {
			if worktree, err = e.worktree(ctx, branch); err != nil {
				skip(branch, err)
				continue
			}
		}
		if _, _, _, err := e.settlePublication(ctx, worktree, branch, false); err != nil {
			if ctx.Err() != nil {
				return false, skipped, err
			}
			skip(branch, err)
		}
	}
	if err := e.settleTriedRows(ctx, branches, fork); err != nil {
		return false, skipped, err
	}
	return done, skipped, nil
}

// serveFirstPass accounts once for what an older dockhand's create may
// have left: a branch serve prepared that is on the fork with no pull
// request recorded, which only a submit pushes under its own name. Each
// such branch is read for a pull request and gets a row of a create
// unanswered: one open is charged to the day GitHub made it; one not open
// made on the upgrade's day or before can't be told from an earlier
// branch's of the name, so its number becomes the row's after; one made
// after it is the row's opening. It's marked done only when every such
// branch has been read, and a branch read before isn't written twice.
func (e *Engine) serveFirstPass(ctx context.Context, branches []model.Branch, fork buildenv.Fork, forkErr error) (bool, error) {
	var at time.Time
	var ok, done bool
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		at, done, ok, err = r.ServeUpgrade()
		return err
	})
	if err != nil || !ok || done {
		return err == nil, err
	}
	if forkErr != nil {
		return false, fmt.Errorf("finding the fork to account for older submissions: %w", forkErr)
	}
	var rows []model.ServeOpening
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		rows, err = r.ServeOpenings()
		return err
	}); err != nil {
		return false, err
	}
	base := e.PullRequestRepository() + ":" + UpstreamBranch
	held := false
	for _, branch := range branches {
		if branch.Origin != model.OriginServe || branch.PullRequest != nil || branch.Publication != nil {
			continue
		}
		head := fork.Repository + ":" + branch.Name
		if hasRow(rows, base, head) {
			continue
		}
		// Read and written under the head's lock, the branch and the rows
		// read again under it: a submission working on the head now
		// records its own (#191, group A). One another process holds is
		// read on a later pass, and this one isn't done.
		var row *model.ServeOpening
		err := e.Repo.WithHeadLock(ctx, head, func(ctx context.Context) error {
			var err error
			row, err = e.firstPassRow(ctx, branch.ID, base, head, branch.Name, fork, at)
			return err
		})
		var busy *git.HeadHeldError
		if errors.As(err, &busy) {
			held = true
			continue
		}
		if err != nil {
			return false, err
		}
		if row != nil {
			rows = append(rows, *row)
		}
	}
	if held {
		return false, nil
	}
	return true, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.SetServeFirstPassDone() })
}

// firstPassRow is serveFirstPass's reading of one branch, under its head's
// lock, and the row it writes, nil where it writes none.
func (e *Engine) firstPassRow(ctx context.Context, id model.BranchID, base, head, name string, fork buildenv.Fork, at time.Time) (*model.ServeOpening, error) {
	var skip bool
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		current, err := r.Branch(id)
		if err != nil {
			return err
		}
		rows, err := r.ServeOpenings()
		skip = current.PullRequest != nil || current.Publication != nil || hasRow(rows, base, head)
		return err
	}); err != nil || skip {
		return nil, err
	}
	remote, err := e.Repo.RemoteHead(ctx, fork.PushURL, name)
	if err != nil {
		return nil, fmt.Errorf("reading %s on the fork: %w", name, err)
	}
	if !remote.Exists {
		return nil, nil
	}
	found, err := e.forge().Find(ctx, forge.PullRequestQuery{Repository: e.PullRequestRepository(), HeadRepository: fork.Repository, HeadBranch: name, BaseBranch: UpstreamBranch})
	if err != nil {
		return nil, fmt.Errorf("looking for %s's pull request: %w", name, err)
	}
	row := model.ServeOpening{Base: base, Head: head, Tried: at, Unanswered: true}
	if found.Found {
		pr := found.PullRequest
		created := pr.CreatedAt.In(e.now().Location())
		switch {
		case pr.State == forge.PullRequestOpen:
			row, _ = row.FoundFor(pr.Ref.Number, pr.CreatedAt)
		case !dayStart(created).After(dayStart(at.In(e.now().Location()))):
			row.After = pr.Ref.Number
		default:
			row, _ = row.FoundFor(pr.Ref.Number, pr.CreatedAt)
		}
	}
	if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AddServeOpening(row)
		return err
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

// hasRow reports a row of serve's count already written for a head.
func hasRow(rows []model.ServeOpening, base, head string) bool {
	for _, row := range rows {
		if row.Base == base && row.Head == head {
			return true
		}
	}
	return false
}

// settleTriedRows reads each row of a create that may have landed whose
// publication has ended: a pull request found above its after is charged
// to it; its head gone from the fork at two looks ten minutes apart, with
// none found, releases it (GoneLook), the escape the person chose; a head
// there again clears the mark.
func (e *Engine) settleTriedRows(ctx context.Context, branches []model.Branch, fork buildenv.Fork) error {
	var rows []model.ServeOpening
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		rows, err = r.ServeOpenings()
		return err
	}); err != nil {
		return err
	}
	for _, row := range rows {
		if row.Tried.IsZero() || !row.Opened.IsZero() || publishing(branches, row.Head) {
			continue
		}
		// Read and written under the head's lock, the row and the
		// publications read again under it (#191, group A); a head another
		// process holds is read on a later pass.
		err := e.Repo.WithHeadLock(ctx, row.Head, func(ctx context.Context) error {
			return e.settleTriedRow(ctx, row.ID, fork)
		})
		var busy *git.HeadHeldError
		if err != nil && !errors.As(err, &busy) {
			return err
		}
	}
	return nil
}

// settleTriedRow is settleTriedRows' reading of one row, under its head's
// lock: the row as it is now, unless it opened or a publication works on
// its head since.
func (e *Engine) settleTriedRow(ctx context.Context, id int64, fork buildenv.Fork) error {
	var row model.ServeOpening
	var gone bool
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		rows, err := r.ServeOpenings()
		if err != nil {
			return err
		}
		i := slices.IndexFunc(rows, func(o model.ServeOpening) bool { return o.ID == id })
		if i < 0 {
			gone = true
			return nil
		}
		row = rows[i]
		branches, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		gone = publishing(branches, row.Head)
		return err
	}); err != nil || gone || row.Tried.IsZero() || !row.Opened.IsZero() {
		return err
	}
	baseRepository, baseBranch, _ := strings.Cut(row.Base, ":")
	headRepository, headBranch, _ := strings.Cut(row.Head, ":")
	found, err := e.forge().Find(ctx, forge.PullRequestQuery{Repository: baseRepository, HeadRepository: headRepository, HeadBranch: headBranch, BaseBranch: baseBranch})
	if err != nil {
		return nil
	}
	next, release := row, false
	if found.Found {
		if charged, ok := row.FoundFor(found.PullRequest.Ref.Number, found.PullRequest.CreatedAt); ok {
			next = charged
		}
	}
	if next.Opened.IsZero() && fork.PushURL != "" && headRepository == fork.Repository {
		remote, err := e.Repo.RemoteHead(ctx, fork.PushURL, headBranch)
		if err != nil {
			return nil
		}
		next, release = next.GoneLook(remote.Exists, e.now())
	}
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if release {
			return tx.ReleaseServeOpening(row.ID)
		}
		if next == row {
			return nil
		}
		return tx.UpdateServeOpening(next)
	})
}

// publishing reports a head an unfinished publication still works on,
// whose rows it settles itself.
func publishing(branches []model.Branch, head string) bool {
	for _, branch := range branches {
		if branch.Publication != nil && branch.Publication.Head == head {
			return true
		}
	}
	return false
}
