package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
)

// Tag reads one tag: from what the repository already read, when it has
// listed its tags or read this one before, else from the API.
func (r *repository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	refName := "refs/tags/" + name
	if !git.ValidRefName(refName) {
		return forge.Tag{}, fmt.Errorf("github: invalid tag")
	}
	if tag, ok := r.remembered(name); ok {
		return tag, nil
	}
	tag, err := r.readTag(ctx, name, refName)
	if err == nil {
		r.remember(tag)
	}
	return tag, err
}

func (r *repository) remembered(name string) (forge.Tag, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tag, ok := r.tags[name]
	return tag, ok
}

func (r *repository) remember(tags ...forge.Tag) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tags == nil {
		r.tags = map[string]forge.Tag{}
	}
	for _, tag := range tags {
		r.tags[tag.Name] = tag
	}
}

func (r *repository) readTag(ctx context.Context, name, refName string) (forge.Tag, error) {
	client, err := r.client.API(ctx)
	if err != nil {
		return r.gitTag(ctx, name, githubapi.RateLimitError(err))
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	ref, response, err := client.Git.GetRef(ctx, owner, repo, refName)
	if err != nil {
		// The API's 404 is its answer that the tag does not exist; any
		// other failure says nothing about the tag, and git is asked.
		if response != nil && response.StatusCode == http.StatusNotFound {
			return forge.Tag{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return r.gitTag(ctx, name, githubapi.RateLimitError(err))
	}
	if ref.GetRef() != refName {
		return forge.Tag{}, fmt.Errorf("github: response identifies a different ref")
	}
	object := ref.Object
	seen := map[string]bool{}
	for {
		sha := object.GetSHA()
		if !git.ValidObjectID(sha) || seen[sha] {
			return forge.Tag{}, fmt.Errorf("github: invalid or cyclic tag object")
		}
		seen[sha] = true
		if object.GetType() == "commit" {
			return forge.Tag{Name: name, Commit: sha}, nil
		}
		if object.GetType() != "tag" {
			return forge.Tag{}, fmt.Errorf("github: tag does not identify a commit")
		}
		annotated, _, err := client.Git.GetTag(ctx, owner, repo, sha)
		if err != nil {
			return r.gitTag(ctx, name, githubapi.RateLimitError(err))
		}
		if annotated.GetSHA() != sha {
			return forge.Tag{}, fmt.Errorf("github: response identifies a different tag object")
		}
		object = annotated.Object
	}
}

func (r *repository) ListTags(ctx context.Context) ([]forge.Tag, error) {
	// The repository's git service lists every tag, peeled, in one round
	// trip outside the API's limits and its pacer, where the API pages them
	// a hundred at a time, 80 ms apart: teleport's 7,117 tags were 72
	// requests (the upstream-wait measure, 2026-10-31: a look's 801
	// repositories listed in 21 s, 8 at once). A repository git can't
	// list as anyone would, gone or private, goes to the API, as an
	// enterprise server's, or a test's, does.
	if r.client.Client != nil && r.client.Config.BaseURL == "" && r.client.Config.CloneURL == "" {
		tags, err := r.publicTags(ctx)
		if err == nil {
			return tags, nil
		}
		progress.DebugReport(ctx, "Listing the tags of %s with git failed (%v); asking the API", r.name, err)
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return r.gitTags(ctx, githubapi.RateLimitError(err))
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	seen := map[string]bool{}
	var tags []forge.Tag
	for row, err := range client.Repositories.ListTagsIter(ctx, owner, repo, &gh.ListOptions{PerPage: pageSize}) {
		if err != nil {
			return r.gitTags(ctx, githubapi.RateLimitError(err))
		}
		if row == nil {
			return nil, fmt.Errorf("github: invalid repository tag")
		}
		// A tag that names no commit, such as git/git's junio-gpg-pub key
		// blob, or an unusable ref name can never be a release; skip it.
		if !git.ValidRefName("refs/tags/"+row.GetName()) || !git.ValidObjectID(row.GetCommit().GetSHA()) {
			continue
		}
		if seen[row.GetName()] {
			return nil, fmt.Errorf("%w: duplicate tag %s", forge.ErrIncomplete, row.GetName())
		}
		seen[row.GetName()] = true
		tags = append(tags, forge.Tag{Name: row.GetName(), Commit: row.GetCommit().GetSHA()})
		r.answer(row.GetZipballURL())
	}
	// The API names each tag's commit, peeled, as Tag would read it. Git's
	// listing above can't tell a commit from a blob, so it isn't kept.
	r.remember(tags...)
	return tags, nil
}

// publicTags lists the repository's tags from its git service, anonymously
// (git.ListPublicTags), each peeled to the commit it finally names, as the
// API's listing names it, and remembers them for Tag. It begins the read of
// the repository's web page that FindMove waits for.
func (r *repository) publicTags(ctx context.Context) ([]forge.Tag, error) {
	listed, err := git.ListPublicTags(ctx, r.client.Git, r.cloneURL())
	if err != nil {
		return nil, err
	}
	tags := make([]forge.Tag, 0, len(listed))
	for _, tag := range listed {
		tags = append(tags, forge.Tag{Name: tag.Name, Commit: tag.Object})
	}
	r.remember(tags...)
	// GitHub's git said nothing of a move, so its web page is asked, from
	// now, beside the rest of the look (FindMove).
	var move *moveRead
	if r.client.Moves != nil {
		move = r.client.Moves.start(ctx, r.name)
	}
	r.mu.Lock()
	r.unread = true
	if r.move == nil {
		r.move = move
	}
	r.mu.Unlock()
	return tags, nil
}

// gitTag reads one tag with plain git when the API could not answer, as the
// repository's git service answers anonymously where the API may be rate
// limited or refused. The API's failure stays in the error when git fails
// too, so a rate limit is still recognized.
func (r *repository) gitTag(ctx context.Context, name string, cause error) (forge.Tag, error) {
	progress.VerboseReport(ctx, "GitHub API could not read tag %s of %s (%v); reading it with git", name, r.name, cause)
	tags, err := git.ListRemoteTags(ctx, r.client.Git, r.cloneURL(), name)
	if err != nil {
		return forge.Tag{}, errors.Join(cause, fmt.Errorf("github: reading tag %s of %s with git: %w", name, r.name, err))
	}
	if len(tags) != 1 {
		return forge.Tag{}, fmt.Errorf("%w: %s has no tag %s", forge.ErrNotFound, r.name, name)
	}
	return forge.Tag{Name: tags[0].Name, Commit: tags[0].Object}, nil
}

// gitTags lists the tags with plain git when the API could not. Unlike the
// API, git cannot tell a tag that names a blob from one that names a commit;
// such a tag never parses as a release version.
func (r *repository) gitTags(ctx context.Context, cause error) ([]forge.Tag, error) {
	progress.VerboseReport(ctx, "GitHub API could not list the tags of %s (%v); listing them with git", r.name, cause)
	listed, err := git.ListRemoteTags(ctx, r.client.Git, r.cloneURL())
	if err != nil {
		return nil, errors.Join(cause, fmt.Errorf("github: listing the tags of %s with git: %w", r.name, err))
	}
	tags := make([]forge.Tag, 0, len(listed))
	for _, tag := range listed {
		tags = append(tags, forge.Tag{Name: tag.Name, Commit: tag.Object})
	}
	return tags, nil
}
