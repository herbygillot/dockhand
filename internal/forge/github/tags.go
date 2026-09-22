package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
)

func (r *repository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	refName := "refs/tags/" + name
	if !git.ValidRefName(refName) {
		return forge.Tag{}, fmt.Errorf("github: invalid tag")
	}
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
	client, err := r.client.API(ctx)
	if err != nil {
		return r.gitTags(ctx, githubapi.RateLimitError(err))
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	seen := map[string]bool{}
	var tags []forge.Tag
	for row, err := range client.Repositories.ListTagsIter(ctx, owner, repo, nil) {
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
	}
	return tags, nil
}

// gitTag reads one tag with plain git when the API could not answer, as the
// repository's git service answers anonymously where the API may be rate
// limited or refused. The API's failure stays in the error when git fails
// too, so a rate limit is still recognized.
func (r *repository) gitTag(ctx context.Context, name string, cause error) (forge.Tag, error) {
	progress.VerboseReport(ctx, "GitHub API could not read tag %s of %s (%v); reading it with git", name, r.name, cause)
	tags, err := git.ListRemoteTags(ctx, r.client.GitExecutable, r.cloneURL(), name)
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
	listed, err := git.ListRemoteTags(ctx, r.client.GitExecutable, r.cloneURL())
	if err != nil {
		return nil, errors.Join(cause, fmt.Errorf("github: listing the tags of %s with git: %w", r.name, err))
	}
	tags := make([]forge.Tag, 0, len(listed))
	for _, tag := range listed {
		tags = append(tags, forge.Tag{Name: tag.Name, Commit: tag.Object})
	}
	return tags, nil
}
