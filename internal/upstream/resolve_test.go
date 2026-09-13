package upstream_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/stretchr/testify/require"
)

type tagFunc func(context.Context, string, string) (upstream.Tag, error)

func (f tagFunc) Tag(ctx context.Context, repo, tag string) (upstream.Tag, error) {
	return f(ctx, repo, tag)
}
func githubPort() macports.PortInfo {
	return macports.PortInfo{Name: "fixture", Version: "1.0", Options: map[string]string{"github.author": "owner", "github.project": "project", "github.version": "1.0", "github.tag_prefix": "v", "github.tag_suffix": "", "git.branch": "v1.0"}}
}

func TestResolveUsesObservedTagsAndChecksRecordedCommit(t *testing.T) {
	for _, request := range []string{"2.0", "v2.0"} {
		t.Run(request, func(t *testing.T) {
			var calls []string
			commit := strings.Repeat("a", 40)
			service := upstream.Service{Tags: tagFunc(func(_ context.Context, repo, tag string) (upstream.Tag, error) {
				require.Equal(t, "owner/project", repo)
				calls = append(calls, tag)
				if tag != "v2.0" {
					return upstream.Tag{}, upstream.ErrTagMissing
				}
				return upstream.Tag{Name: tag, Commit: commit}, nil
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
		{"missing", func(context.Context, string, string) (upstream.Tag, error) {
			return upstream.Tag{}, upstream.ErrTagMissing
		}, upstream.ErrReleaseMissing},
		{"server failure", func(context.Context, string, string) (upstream.Tag, error) { return upstream.Tag{}, outage }, outage},
		{"ambiguous", func(_ context.Context, _ string, tag string) (upstream.Tag, error) {
			return upstream.Tag{Name: tag, Commit: strings.Repeat("a", 40)}, nil
		}, upstream.ErrReleaseAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := upstream.Service{Tags: test.lookup}
			_, err := service.Resolve(t.Context(), githubPort(), "2.0")
			require.ErrorIs(t, err, test.expected)
		})
	}
	service := upstream.Service{Tags: tagFunc(func(_ context.Context, _ string, tag string) (upstream.Tag, error) {
		return upstream.Tag{Name: tag, Commit: strings.Repeat("a", 40)}, nil
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
