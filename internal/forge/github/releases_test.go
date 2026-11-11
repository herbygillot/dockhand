package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

func releaseRow(tag string) map[string]any {
	return map[string]any{"tag_name": tag, "draft": false, "prerelease": false, "published_at": "2026-01-01T00:00:00Z"}
}

func TestReleaseCatalogPreservesReleaseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		published := releaseRow("v1")
		published["html_url"] = "https://github.com/owner/project/releases/tag/v1"
		prerelease := releaseRow("v2")
		prerelease["prerelease"] = true
		draft := releaseRow("v3")
		draft["draft"], draft["published_at"] = true, nil
		json.NewEncoder(w).Encode([]any{published, prerelease, draft})
	}))
	defer server.Close()
	rows, err := testRepository(t, &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}).Releases(t.Context())
	require.NoError(t, err)
	publishedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Equal(t, []forge.Release{
		{Tag: "v1", URL: "https://github.com/owner/project/releases/tag/v1", PublishedAt: publishedAt},
		{Tag: "v2", Prerelease: true, PublishedAt: publishedAt},
		{Tag: "v3", Draft: true},
	}, rows)
}

func TestCatalogDoesNotReturnPartialEvidence(t *testing.T) {
	for _, mode := range []string{"later failure", "duplicate", "null row", "missing metadata"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "" {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
					json.NewEncoder(w).Encode([]any{releaseRow("v1")})
					return
				}
				switch mode {
				case "later failure":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "duplicate":
					json.NewEncoder(w).Encode([]any{releaseRow("v1")})
				case "null row":
					fmt.Fprint(w, "[null]")
				case "missing metadata":
					fmt.Fprint(w, `[{"tag_name":"v2"}]`)
				}
			}))
			defer server.Close()
			rows, err := testRepository(t, &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}).Releases(t.Context())
			require.Error(t, err)
			require.Nil(t, rows)
			require.NotErrorIs(t, err, forge.ErrNotFound)
			if mode == "duplicate" {
				require.ErrorIs(t, err, forge.ErrIncomplete)
			}
		})
	}
}

func TestTagCatalogMapsTagNamesAndCommits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/project/tags":
			json.NewEncoder(w).Encode([]any{map[string]any{"name": "v2.0", "commit": map[string]string{"sha": strings.Repeat("a", 40)}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	tags, err := testRepository(t, &client).ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Tag{{Name: "v2.0", Commit: strings.Repeat("a", 40)}}, tags)
}

// A listing asks for GitHub's largest page, and a tag it listed is read
// from the listing afterwards, with no request of its own: the listing
// names each tag's commit already.
func TestListedTagsAreNotReadAgain(t *testing.T) {
	var queries, lookups []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/project/tags":
			queries = append(queries, r.URL.RawQuery)
			json.NewEncoder(w).Encode([]any{map[string]any{"name": "v2.0", "commit": map[string]string{"sha": strings.Repeat("a", 40)}}})
		default:
			lookups = append(lookups, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}
	repository := testRepository(t, &client)
	_, err := repository.ListTags(t.Context())
	require.NoError(t, err)
	tag, err := repository.Tag(t.Context(), "v2.0")
	require.NoError(t, err)
	require.Equal(t, forge.Tag{Name: "v2.0", Commit: strings.Repeat("a", 40)}, tag)
	require.Equal(t, []string{"per_page=100"}, queries)
	require.Empty(t, lookups)

	_, err = repository.Tag(t.Context(), "v1.0")
	require.ErrorIs(t, err, forge.ErrNotFound, "a tag the listing didn't have is still asked for")
	require.Equal(t, []string{"/repos/owner/project/git/ref/tags/v1.0"}, lookups)
}

// A release's assets are described as GitHub gives them: each one's name,
// the URL GitHub serves it at, and its size, an absent size told from an
// empty file (#16).
func TestAssetsAreDescribedWithTheirURLAndSize(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/owner/project/releases/tags/v1", r.URL.Path)
		fmt.Fprint(w, `{"tag_name":"v1","assets":[
			{"name":"project-1.tar.gz","browser_download_url":"https://objects.example.com/a/project-1.tar.gz","size":120},
			{"name":"empty.txt","browser_download_url":"https://github.com/owner/project/releases/download/v1/empty.txt","size":0},
			{"name":"unsized.zip","browser_download_url":"https://github.com/owner/project/releases/download/v1/unsized.zip"}]}`)
	}))
	defer server.Close()
	assets, err := testRepository(t, &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}).(forge.AssetRepository).Assets(t.Context(), "v1")
	require.NoError(t, err)
	require.Equal(t, []model.ReleaseAsset{
		{Name: "project-1.tar.gz", URL: "https://objects.example.com/a/project-1.tar.gz", Size: 120, SizeKnown: true},
		{Name: "empty.txt", URL: "https://github.com/owner/project/releases/download/v1/empty.txt", SizeKnown: true},
		{Name: "unsized.zip", URL: "https://github.com/owner/project/releases/download/v1/unsized.zip"},
	}, assets)
}

// An asset with a name that isn't one file's, one without a URL GitHub
// serves, or a name given twice is an incomplete observation, never a
// partial answer, as a release listing's is.
func TestAssetsAreCompleteOrRefused(t *testing.T) {
	t.Parallel()
	for mode, rows := range map[string]string{
		"a path":      `{"name":"dir/project.tar.gz","browser_download_url":"https://example.com/x"}`,
		"no name":     `{"browser_download_url":"https://example.com/x"}`,
		"no URL":      `{"name":"project.tar.gz"}`,
		"a local URL": `{"name":"project.tar.gz","browser_download_url":"file:///etc/passwd"}`,
		"a duplicate": `{"name":"project.tar.gz","browser_download_url":"https://example.com/a"},{"name":"project.tar.gz","browser_download_url":"https://example.com/b"}`,
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"tag_name":"v1","assets":[%s]}`, rows)
			}))
			defer server.Close()
			assets, err := testRepository(t, &github.Client{Client: &githubapi.Client{Config: githubapi.Config{BaseURL: server.URL}}}).(forge.AssetRepository).Assets(t.Context(), "v1")
			require.ErrorIs(t, err, forge.ErrIncomplete)
			require.Nil(t, assets)
		})
	}
}
