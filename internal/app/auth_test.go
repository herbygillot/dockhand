package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/credential"
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
