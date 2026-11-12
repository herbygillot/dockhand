package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// settleServe is what serve's submitting pass does first, before any
// candidate (docs/designs/publication-record.md, "serve's count"): the
// first pass after the records took on the count by rows, which accounts
// for a create an older dockhand may have left; every publication serve
// recorded, settled; and every row of a create that may have landed whose
// publication has ended, read for its pull request and its head. Where
// the first pass couldn't read everything, serve opens nothing this pass.
func (e *Engine) settleServe(ctx context.Context) (opens bool, err error) {
	branches, err := e.OpenBranches(ctx)
	if err != nil {
		return false, err
	}
	fork, forkErr := e.Fork(ctx, "")
	done, err := e.serveFirstPass(ctx, branches, fork, forkErr)
	if err != nil {
		return false, err
	}
	for _, branch := range branches {
		if branch.Publication == nil || !branch.Publication.Served {
			continue
		}
		worktree := e.Repo
		if branch.Worktree != "" && exists(branch.Worktree) {
			if worktree, err = e.worktree(ctx, branch); err != nil {
				return false, err
			}
		}
		if _, _, _, err := e.settlePublication(ctx, worktree, branch, fork.PushURL); err != nil {
			return false, err
		}
	}
	if err := e.settleTriedRows(ctx, branches, fork); err != nil {
		return false, err
	}
	return done, nil
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
	for _, branch := range branches {
		if branch.Origin != model.OriginServe || branch.PullRequest != nil || branch.Publication != nil {
			continue
		}
		head := fork.Repository + ":" + branch.Name
		if hasRow(rows, base, head) {
			continue
		}
		remote, err := e.Repo.RemoteHead(ctx, fork.PushURL, branch.Name)
		if err != nil {
			return false, fmt.Errorf("reading %s on the fork: %w", branch.Name, err)
		}
		if !remote.Exists {
			continue
		}
		found, err := e.forge().Find(ctx, forge.PullRequestQuery{Repository: e.PullRequestRepository(), HeadRepository: fork.Repository, HeadBranch: branch.Name, BaseBranch: UpstreamBranch})
		if err != nil {
			return false, fmt.Errorf("looking for %s's pull request: %w", branch.Name, err)
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
			return false, err
		}
		rows = append(rows, row)
	}
	return true, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.SetServeFirstPassDone() })
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
		baseRepository, baseBranch, _ := strings.Cut(row.Base, ":")
		headRepository, headBranch, _ := strings.Cut(row.Head, ":")
		found, err := e.forge().Find(ctx, forge.PullRequestQuery{Repository: baseRepository, HeadRepository: headRepository, HeadBranch: headBranch, BaseBranch: baseBranch})
		if err != nil {
			continue
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
				continue
			}
			next, release = next.GoneLook(remote.Exists, e.now())
		}
		err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			if release {
				return tx.ReleaseServeOpening(row.ID)
			}
			if next == row {
				return nil
			}
			return tx.UpdateServeOpening(next)
		})
		if err != nil {
			return err
		}
	}
	return nil
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
