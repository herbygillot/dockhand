package gitlab_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	forgegitlab "github.com/herbygillot/dockhand/v2/internal/forge/gitlab"
	"github.com/stretchr/testify/require"
)

func TestRepositoryMapsInstancePathIntoGitLabProjectIdentity(t *testing.T) {
	commit := strings.Repeat("a", 40)
	var escapedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		json.NewEncoder(w).Encode(map[string]any{"name": "v2.0", "commit": map[string]string{"id": commit}})
	}))
	defer server.Close()
	client := forgegitlab.Client{HTTP: server.Client()}
	repository, err := client.Repository(server.URL+"/tpo", "core/project")
	require.NoError(t, err)
	require.Equal(t, "core/project", repository.Name())
	tag, err := repository.Tag(t.Context(), "v2.0")
	require.NoError(t, err)
	require.Equal(t, forge.Tag{Name: "v2.0", Commit: commit}, tag)
	require.Equal(t, "/api/v4/projects/tpo%2Fcore%2Fproject/repository/tags/v2%2E0", escapedPath)
}

func TestRepositoryReadsEveryGitLabTagPage(t *testing.T) {
	commits := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		index := 0
		if page == "2" {
			index = 1
		} else {
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("X-Page", "1")
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"name": fmt.Sprintf("v%d.0", index+1), "commit": map[string]string{"id": commits[index]}}})
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "owner/project")
	require.NoError(t, err)
	tags, err := repository.ListTags(t.Context())
	require.NoError(t, err)
	require.Equal(t, []forge.Tag{{Name: "v1.0", Commit: commits[0]}, {Name: "v2.0", Commit: commits[1]}}, tags)
}

func TestRepositoryRejectsDuplicateGitLabTags(t *testing.T) {
	commit := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{
			map[string]any{"name": "v1.0", "commit": map[string]string{"id": commit}},
			map[string]any{"name": "v1.0", "commit": map[string]string{"id": commit}},
		})
	}))
	defer server.Close()
	repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "owner/project")
	require.NoError(t, err)
	_, err = repository.ListTags(t.Context())
	require.ErrorIs(t, err, forge.ErrIncomplete)
}

func TestGitLabTagFailuresRemainDistinct(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		missing bool
	}{
		{name: "missing", status: http.StatusNotFound, missing: true},
		{name: "rejected", status: http.StatusForbidden},
		{name: "invalid", status: http.StatusOK, body: `{"name":"v2","commit":{"id":"short"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			repository, err := (&forgegitlab.Client{HTTP: server.Client()}).Repository(server.URL, "owner/project")
			require.NoError(t, err)
			_, err = repository.Tag(t.Context(), "v2")
			require.Error(t, err)
			if test.missing {
				require.ErrorIs(t, err, forge.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, forge.ErrNotFound)
			}
		})
	}
}
