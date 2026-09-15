package app_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type loginFlow struct {
	clientID string
	present  bool
}

func (f *loginFlow) Authorize(ctx context.Context, clientID string, present func(credential.DeviceAuthorization) error) (credential.Value, error) {
	f.clientID = clientID
	f.present = true
	if err := present(credential.DeviceAuthorization{UserCode: "ABCD-EFGH", VerificationURL: "https://example.invalid/device"}); err != nil {
		return credential.Value{}, err
	}
	return credential.Value{Secret: "fixture-secret", Account: "fixture-user"}, nil
}

type loginStore struct {
	key    credential.Key
	secret string
	err    error
}

func (*loginStore) Get(context.Context, credential.Key) (string, error) {
	return "", credential.ErrNotFound
}
func (s *loginStore) Put(_ context.Context, key credential.Key, secret string) error {
	s.key, s.secret = key, secret
	return s.err
}

func TestGitHubLoginAuthorizesBeforeSavingToKeychain(t *testing.T) {
	flow, store := &loginFlow{}, &loginStore{}
	presented := false
	result, err := app.LoginGitHub(t.Context(), app.GitHubLoginOptions{ClientID: "fixture-client", Flow: flow, Store: store, Present: func(authorization credential.DeviceAuthorization) error {
		presented = true
		require.Equal(t, "ABCD-EFGH", authorization.UserCode)
		return nil
	}})
	require.NoError(t, err)
	require.True(t, presented)
	require.Equal(t, "fixture-client", flow.clientID)
	require.Equal(t, "github.com/herbygillot/dockhand", store.key.Service)
	require.Equal(t, "github.com", store.key.Account)
	require.Equal(t, "fixture-secret", store.secret)
	require.Equal(t, app.GitHubLoginResult{Host: "github.com", Account: "fixture-user", Storage: "macOS Keychain"}, result)
}

func TestGitHubLoginRequiresConfigurationAndPropagatesStorageFailure(t *testing.T) {
	_, err := app.LoginGitHub(t.Context(), app.GitHubLoginOptions{})
	require.ErrorContains(t, err, "client ID")
	flow, store := &loginFlow{}, &loginStore{err: errors.New("keychain locked")}
	_, err = app.LoginGitHub(t.Context(), app.GitHubLoginOptions{ClientID: "fixture", Flow: flow, Store: store, Present: func(credential.DeviceAuthorization) error { return nil }})
	require.ErrorContains(t, err, "keychain locked")
	assert.NotContains(t, err.Error(), "fixture-secret")
}

func (s *loginStore) Delete(_ context.Context, key credential.Key) error { s.key = key; return s.err }

func TestGitHubLogoutRemovesOnlyDockhandEntry(t *testing.T) {
	for _, failure := range []error{nil, credential.ErrNotFound, errors.New("keychain locked")} {
		store := &loginStore{err: failure}
		result, err := app.LogoutGitHub(t.Context(), store)
		require.Equal(t, credential.Key{Service: "github.com/herbygillot/dockhand", Account: "github.com"}, store.key)
		if failure == nil || errors.Is(failure, credential.ErrNotFound) {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, failure)
		}
		require.Equal(t, failure == nil, result.Removed)
	}
}

func TestGitHubStatusReturnsSourceOnRejection(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if accepted {
				fmt.Fprint(w, `{"login":"fixture-user"}`)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
		}))
		client := &github.Client{Config: github.Config{BaseURL: server.URL, Token: "fixture-secret"}}
		result, err := app.StatusGitHub(t.Context(), client)
		require.Equal(t, accepted, result.Authenticated)
		require.Equal(t, github.SourceExplicit, result.Source)
		if accepted {
			require.NoError(t, err)
			require.Equal(t, "fixture-user", result.Account)
		} else {
			require.ErrorIs(t, err, github.ErrAuthentication)
			require.Empty(t, result.Account)
		}
		server.Close()
	}
}
