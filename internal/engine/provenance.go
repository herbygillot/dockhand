package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
)

// UnfoundBuild is a dockhand build a branch's commits name in Generated-By
// that nobody else can find: dockhand's repository on GitHub doesn't have
// the commit or the release it names, or it names neither, recording no
// revision.
type UnfoundBuild struct {
	// Build is the build as the trailer names it.
	Build string
	// Commits are the branch's commits naming it.
	Commits []string
	// Source is what GitHub was asked for; neither field, for a build
	// that names nothing to ask about.
	Source buildinfo.Source
}

// unfoundBuilds asks dockhand's repository on GitHub for what each build
// the commits name in Generated-By was built from, each build once: a
// build of a commit never pushed names one nobody else can find, as a
// build of uncommitted source does (the hugo exercise), and tidy reads
// nothing remote to know it. A build of uncommitted source is
// ModifiedBuilds', and isn't asked about. Why GitHub couldn't be asked,
// for want of a login, the network, or its rate limit, is said once, and
// the builds after it aren't asked; nothing here holds a submission.
func (e *Engine) unfoundBuilds(ctx context.Context, commits []git.HistoryCommit) ([]UnfoundBuild, string) {
	var builds []UnfoundBuild
	for _, commit := range commits {
		build, ok := commitmsg.Build(commit.Message)
		if !ok || buildinfo.TagModified(build) {
			continue
		}
		if i := slices.IndexFunc(builds, func(b UnfoundBuild) bool { return b.Build == build }); i >= 0 {
			builds[i].Commits = append(builds[i].Commits, commit.ID)
			continue
		}
		builds = append(builds, UnfoundBuild{Build: build, Commits: []string{commit.ID}, Source: buildinfo.SourceOf(build)})
	}
	var unfound []UnfoundBuild
	var repository forge.Repository
	problem := ""
	for _, build := range builds {
		if build.Source == (buildinfo.Source{}) {
			unfound = append(unfound, build)
			continue
		}
		if problem != "" {
			continue
		}
		if repository == nil {
			var err error
			if repository, err = e.dockhandRepository(); err != nil {
				problem = err.Error()
				continue
			}
		}
		found, err := hasSource(ctx, repository, build.Source)
		switch {
		case err != nil:
			problem = err.Error()
		case !found:
			unfound = append(unfound, build)
		}
	}
	return unfound, problem
}

// dockhandRepository is dockhand's own repository on GitHub, as the forge
// reads it.
func (e *Engine) dockhandRepository() (forge.Repository, error) {
	name, err := github.PageRepository(buildinfo.ProjectURL)
	if err != nil {
		return nil, err
	}
	return e.forge().Repository("https://github.com", name)
}

// hasSource reports whether a repository has a build's source: its commit,
// or its release's tag.
func hasSource(ctx context.Context, repository forge.Repository, source buildinfo.Source) (bool, error) {
	if source.Commit == "" {
		_, err := repository.Tag(ctx, source.Release)
		if errors.Is(err, forge.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	}
	commits, ok := repository.(forge.CommitRepository)
	if !ok {
		return false, fmt.Errorf("%s can't be asked for a commit", repository.Name())
	}
	return commits.HasCommit(ctx, source.Commit)
}
