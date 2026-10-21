package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/credential"
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

// A token GitHub stopped honouring, as a revoked login's, is found at the
// next recheck, as ErrAuthentication (the rc6 full stage, D-C3).
func TestTheIdentityWatcherFindsARevokedToken(t *testing.T) {
	revoked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if revoked {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"login":"ada"}`)
	}))
	defer server.Close()
	watcher := &github.IdentityWatcher{HTTP: server.Client(), BaseURL: server.URL + "/", Recheck: time.Nanosecond, Credentials: github.TokenSourceFunc(func(context.Context) (github.Token, error) {
		return github.Token{Secret: "ada-token", Source: github.SourceKeychain}, nil
	})}
	_, err := watcher.Current(t.Context())
	require.NoError(t, err)
	revoked = true
	_, err = watcher.Current(t.Context())
	require.ErrorIs(t, err, github.ErrAuthentication)
	require.NotErrorIs(t, err, github.ErrNoCredentials)
}

// A client's login status names the account GitHub says the token is,
// where the token came from, and, for dockhand's own login, until when it
// renews itself; a token from GH_TOKEN takes precedence over it, and says
// no renewal (the rc9 full stage's D-C4).
func TestALoginStatusNamesTheAccountAndWhereItsTokenComesFrom(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"login":%q}`, map[string]string{"Bearer ada-token": "ada", "Bearer bob-token": "bob"}[r.Header.Get("Authorization")])
	}))
	defer server.Close()
	renews := time.Now().AddDate(0, 6, 0).Truncate(time.Second)
	saved, err := credential.Login{Access: "ada-token", AccessExpiry: time.Now().Add(time.Hour), Refresh: "refresh", RefreshExpiry: renews, Account: "ada", ClientID: "client"}.Encode()
	require.NoError(t, err)
	client := func(override github.Token) *github.Client {
		return &github.Client{HTTP: server.Client(), Config: github.Config{BaseURL: server.URL + "/"},
			Credentials: github.SystemCredentials{Store: &keptLogins{value: saved}, Key: github.CredentialKey, Override: override}}
	}
	status, err := client(github.Token{}).LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ada", status.Account)
	require.Equal(t, github.SourceKeychain, status.Source)
	require.True(t, renews.Equal(status.RenewsUntil), "%v", status.RenewsUntil)
	require.Equal(t, "dockhand's login in the Keychain", status.Source.Words())

	status, err = client(github.Token{Secret: "bob-token", Source: github.SourceGHEnvironment}).LoginStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, github.LoginStatus{Identity: github.Identity{Account: "bob", Source: github.SourceGHEnvironment}}, status)
	require.Equal(t, "GH_TOKEN", status.Source.Words())
}
