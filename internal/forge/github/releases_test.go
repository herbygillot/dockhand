package github_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/stretchr/testify/require"
)

func releaseRow(tag string) map[string]any {
	return map[string]any{"tag_name": tag, "draft": false, "prerelease": false, "published_at": "2026-01-01T00:00:00Z"}
}

func TestReleaseCatalogReadsEveryPageAndRetainsEligibilityMetadata(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("per_page") != "100" {
			w.WriteHeader(400)
			return
		}
		rows := []map[string]any{}
		switch r.URL.Query().Get("page") {
		case "1":
			for n := range 100 {
				rows = append(rows, releaseRow(fmt.Sprintf("v1.%d", n)))
			}
		case "2":
			row := releaseRow("v2.0")
			row["prerelease"] = true
			rows = append(rows, row)
			row = releaseRow("v3.0")
			row["draft"] = true
			row["published_at"] = nil
			rows = append(rows, row)
		default:
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	client := github.Client{Config: github.Config{BaseURL: server.URL}}
	rows, err := testRepository(t, &client).Releases(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 102)
	require.True(t, rows[100].Prerelease)
	require.True(t, rows[101].Draft)
	require.Equal(t, int64(2), calls.Load())
}

func TestCatalogDoesNotReturnPartialEvidence(t *testing.T) {
	for _, mode := range []string{"later failure", "duplicate", "truncated", "null", "missing metadata"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if mode == "null" {
					fmt.Fprint(w, "null")
					return
				}
				if mode == "missing metadata" {
					fmt.Fprint(w, `[{"tag_name":"v1"}]`)
					return
				}
				if mode == "later failure" && page == 2 {
					w.WriteHeader(503)
					return
				}
				rows := []map[string]any{}
				if mode == "duplicate" && page == 2 {
					rows = append(rows, releaseRow("v1.0"))
				} else {
					for n := range 100 {
						rows = append(rows, releaseRow(fmt.Sprintf("v%d.%d", page, n)))
					}
				}
				json.NewEncoder(w).Encode(rows)
			}))
			defer server.Close()
			client := github.Client{Config: github.Config{BaseURL: server.URL}}
			rows, err := testRepository(t, &client).Releases(t.Context())
			require.Error(t, err)
			require.Nil(t, rows)
			if mode == "duplicate" || mode == "truncated" {
				require.ErrorIs(t, err, forge.ErrIncomplete)
			}
			require.NotErrorIs(t, err, forge.ErrNotFound)
		})
	}
}

func TestRepositoryTagsIncludeProjectsWithoutReleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/project/releases":
			fmt.Fprint(w, "[]")
		case "/repos/owner/project/tags":
			json.NewEncoder(w).Encode([]any{map[string]any{"name": "v2.0", "commit": map[string]string{"sha": strings.Repeat("a", 40)}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := github.Client{Config: github.Config{BaseURL: server.URL}}
	releases, err := testRepository(t, &client).Releases(t.Context())
	require.NoError(t, err)
	require.Empty(t, releases)
	tags, err := testRepository(t, &client).ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Tag{{Name: "v2.0", Commit: strings.Repeat("a", 40)}}, tags)
}
