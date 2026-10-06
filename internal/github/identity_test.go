package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/github"
)

// The watcher reads the credential chain each time, and asks GitHub who
// a token is only when the token changes (the rc6 full stage, D-C2).
func TestTheIdentityWatcherReadsTheLoginAfresh(t *testing.T) {
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"login":%q}`, map[string]string{"Bearer ada-token": "ada", "Bearer bob-token": "bob"}[r.Header.Get("Authorization")])
	}))
	defer server.Close()
	token := "ada-token"
	watcher := &github.IdentityWatcher{HTTP: server.Client(), BaseURL: server.URL + "/", Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) {
		if token == "" {
			return github.Token{}, github.ErrNoCredentials
		}
		return github.Token{Secret: token, Source: github.SourceKeychain}, nil
	})}
	for range 2 {
		identity, err := watcher.Current(t.Context())
		require.NoError(t, err)
		require.Equal(t, "ada", identity.Account)
	}
	require.Equal(t, 1, asked, "the same token is asked about once")
	token = ""
	_, err := watcher.Current(t.Context())
	require.ErrorIs(t, err, github.ErrNoCredentials)
	token = "bob-token"
	identity, err := watcher.Current(t.Context())
	require.NoError(t, err)
	require.Equal(t, "bob", identity.Account)
	require.Equal(t, 2, asked)
}
