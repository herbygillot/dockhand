package upstream_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/stretchr/testify/require"
)

type tagFunc func(context.Context, string, string) (forge.Tag, error)

func (f tagFunc) Repository(name string) (forge.Repository, error) {
	return &tagRepository{name: name, tag: f}, nil
}

type tagRepository struct {
	forge.Repository
	name string
	tag  tagFunc
}

func (r *tagRepository) Name() string { return r.name }
func (r *tagRepository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	return r.tag(ctx, r.name, name)
}

func githubPort() macports.PortInfo {
	return macports.PortInfo{Name: "fixture", Version: "1.0", Options: map[string]string{"github.author": "owner", "github.project": "project", "github.version": "1.0", "github.tag_prefix": "v", "github.tag_suffix": "", "git.branch": "v1.0"}}
}

func TestResolveUsesObservedTagsAndChecksRecordedCommit(t *testing.T) {
	for _, request := range []string{"2.0", "v2.0"} {
		t.Run(request, func(t *testing.T) {
			var calls []string
			commit := strings.Repeat("a", 40)
			service := upstream.Service{Repositories: tagFunc(func(_ context.Context, repo, tag string) (forge.Tag, error) {
				require.Equal(t, "owner/project", repo)
				calls = append(calls, tag)
				if tag != "v2.0" {
					return forge.Tag{}, forge.ErrNotFound
				}
				return forge.Tag{Name: tag, Commit: commit}, nil
			})}
			release, err := service.Resolve(t.Context(), githubPort(), request)
			require.NoError(t, err)
			require.Equal(t, "2.0", release.Version)
			require.Equal(t, "v2.0", release.Tag)
			require.Equal(t, request, release.Requested)
			require.Equal(t, commit, release.Commit)
			require.False(t, release.ObservedAt.IsZero())
			if request == "2.0" {
				require.Equal(t, []string{"2.0", "v2.0"}, calls)
			} else {
				require.Equal(t, []string{"v2.0"}, calls)
			}
			require.NoError(t, service.Check(t.Context(), githubPort(), release))
			commit = strings.Repeat("b", 40)
			require.ErrorIs(t, service.Check(t.Context(), githubPort(), release), upstream.ErrSourceChanged)
		})
	}
}
func TestResolveDoesNotHideFailuresOrAmbiguity(t *testing.T) {
	outage := errors.New("rate limited")
	for _, test := range []struct {
		name     string
		lookup   tagFunc
		expected error
	}{
		{"missing", func(context.Context, string, string) (forge.Tag, error) {
			return forge.Tag{}, forge.ErrNotFound
		}, upstream.ErrReleaseMissing},
		{"server failure", func(context.Context, string, string) (forge.Tag, error) { return forge.Tag{}, outage }, outage},
		{"ambiguous", func(_ context.Context, _ string, tag string) (forge.Tag, error) {
			return forge.Tag{Name: tag, Commit: strings.Repeat("a", 40)}, nil
		}, upstream.ErrReleaseAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := upstream.Service{Repositories: test.lookup}
			_, err := service.Resolve(t.Context(), githubPort(), "2.0")
			require.ErrorIs(t, err, test.expected)
		})
	}
	service := upstream.Service{Repositories: tagFunc(func(_ context.Context, _ string, tag string) (forge.Tag, error) {
		return forge.Tag{Name: tag, Commit: strings.Repeat("a", 40)}, nil
	})}
	_, err := service.Resolve(t.Context(), githubPort(), "v1.0")
	require.ErrorContains(t, err, "already at version")
	port := githubPort()
	port.Options["git.branch"] = "different"
	_, err = service.Resolve(t.Context(), port, "2.0")
	require.ErrorIs(t, err, upstream.ErrTagPattern)
	port = githubPort()
	port.OptionErrors = map[string]string{"github.version": "failed"}
	_, err = service.Resolve(t.Context(), port, "2.0")
	require.ErrorContains(t, err, "cannot evaluate")
}

type repositoryFunc func(string) (forge.Repository, error)

func (f repositoryFunc) Repository(name string) (forge.Repository, error) { return f(name) }

func TestResolutionRejectsFailedOrMismatchedRepositoryBinding(t *testing.T) {
	unavailable := errors.New("repository unavailable")
	for _, test := range []struct {
		name       string
		repository forge.Repository
		err        error
	}{
		{name: "absent"},
		{name: "failed", err: unavailable},
		{name: "wrong source", repository: &tagRepository{name: "someone/else"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := upstream.Service{Repositories: repositoryFunc(func(name string) (forge.Repository, error) {
				require.Equal(t, "owner/project", name)
				return test.repository, test.err
			})}
			_, err := service.Resolve(t.Context(), githubPort(), "2.0")
			require.Error(t, err)
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
			}
		})
	}
}
