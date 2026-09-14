package github_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/stretchr/testify/require"
)

func TestExactTagAndAnnotatedTagResolution(t *testing.T) {
	commit, annotation := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, annotated := range []bool{false, true} {
		t.Run(fmt.Sprint(annotated), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/repos/owner/project/git/ref/tags/release/2.0":
					kind, sha := "commit", commit
					if annotated {
						kind, sha = "tag", annotation
					}
					fmt.Fprintf(w, `{"ref":"refs/tags/release/2.0","object":{"type":%q,"sha":%q}}`, kind, sha)
				case "/api/repos/owner/project/git/tags/" + annotation:
					fmt.Fprintf(w, `{"sha":%q,"object":{"type":"commit","sha":%q}}`, annotation, commit)
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := github.Client{Config: github.Config{BaseURL: server.URL + "/api"}}
			tag, err := testRepository(t, &client).Tag(t.Context(), "release/2.0")
			require.NoError(t, err)
			require.Equal(t, forge.Tag{Name: "release/2.0", Commit: commit}, tag)
		})
	}
}
func TestTagFailuresStayDistinctAndUntrustedResponsesAreRejected(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		missing bool
	}{
		{"missing", 404, "", true}, {"rate limited", 403, "", false}, {"server", 500, "", false},
		{"wrong ref", 200, `{"ref":"refs/tags/other"}`, false},
		{"non commit", 200, fmt.Sprintf(`{"ref":"refs/tags/v2","object":{"type":"tree","sha":%q}}`, strings.Repeat("a", 40)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); fmt.Fprint(w, test.body) }))
			defer server.Close()
			c := github.Client{Config: github.Config{BaseURL: server.URL}}
			_, err := testRepository(t, &c).Tag(t.Context(), "v2")
			require.Error(t, err)
			if test.missing {
				require.ErrorIs(t, err, forge.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, forge.ErrNotFound)
			}
		})
	}
}
func TestTagReaderRejectsRedirectsBeforeCredentialsLeaveOrigin(t *testing.T) {
	var requests atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer server.Close()
	c := github.Client{Config: github.Config{BaseURL: server.URL, Token: "test-token"}}
	_, err := testRepository(t, &c).Tag(t.Context(), "v2")
	require.ErrorContains(t, err, "left configured API origin")
	require.Zero(t, requests.Load())
}

func TestMissingAnnotationAndCatalogAreNotMissingTagEvidence(t *testing.T) {
	annotation := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/project/git/ref/tags/v2" {
			fmt.Fprintf(w, `{"ref":"refs/tags/v2","object":{"type":"tag","sha":%q}}`, annotation)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	repository := testRepository(t, &github.Client{Config: github.Config{BaseURL: server.URL}})
	_, err := repository.Tag(t.Context(), "missing")
	require.ErrorIs(t, err, forge.ErrNotFound)
	for _, call := range []func() error{
		func() error { _, err := repository.Tag(t.Context(), "v2"); return err },
		func() error { _, err := repository.Releases(t.Context()); return err },
		func() error { _, err := repository.ListTags(t.Context()); return err },
	} {
		err := call()
		require.Error(t, err)
		require.NotErrorIs(t, err, forge.ErrNotFound)
	}
}

func TestNestedAnnotatedTagsResolveAndCyclesFail(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(fmt.Sprint(cycle), func(t *testing.T) {
			commit := strings.Repeat("a", 40)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/git/ref/") {
					fmt.Fprintf(w, `{"ref":"refs/tags/v2","object":{"type":"tag","sha":%q}}`, fmt.Sprintf("%040x", 1))
					return
				}
				sha := strings.TrimPrefix(r.URL.Path, "/repos/owner/project/git/tags/")
				var index int
				fmt.Sscanf(sha, "%x", &index)
				kind, next := "tag", fmt.Sprintf("%040x", index+1)
				if index == 3 {
					kind, next = "commit", commit
				}
				if cycle {
					kind, next = "tag", sha
				}
				fmt.Fprintf(w, `{"sha":%q,"object":{"type":%q,"sha":%q}}`, sha, kind, next)
			}))
			defer server.Close()
			tag, err := testRepository(t, &github.Client{Config: github.Config{BaseURL: server.URL}}).Tag(t.Context(), "v2")
			if cycle {
				require.ErrorContains(t, err, "cyclic")
				return
			}
			require.NoError(t, err)
			require.Equal(t, commit, tag.Commit)
		})
	}
}
