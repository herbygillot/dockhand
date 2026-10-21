package github_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
)

// codeAt is a device flow whose code is entered at address, giving login.
type codeAt struct {
	address string
	login   credential.Login
}

func (f codeAt) Authorize(_ context.Context, _ string, present func(credential.DeviceAuthorization) error) (credential.Login, error) {
	if err := present(credential.DeviceAuthorization{UserCode: "ABCD-1234", VerificationURL: f.address}); err != nil {
		return credential.Login{}, err
	}
	return f.login, nil
}

// removable is keptLogins, which logging out removes from.
type removable struct{ keptLogins }

func (r *removable) Delete(context.Context, credential.Key) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.value == "" {
		return credential.ErrNotFound
	}
	r.value = ""
	return nil
}

func TestLoginKeepsTheLoginAndLogoutRemovesIt(t *testing.T) {
	now := time.Now()
	login := credential.Login{Access: "gho_access", AccessExpiry: now.Add(8 * time.Hour), Refresh: "ghr_refresh", RefreshExpiry: now.Add(4380 * time.Hour), Account: "ada", ClientID: "Iv1.other"}
	store := &removable{}
	var shown []string
	got, err := github.Login(t.Context(), codeAt{address: "https://github.com/login/device", login: login}, store, "Iv1.other", func(authorization credential.DeviceAuthorization) error {
		shown = append(shown, authorization.UserCode)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "ada", got.Account)
	require.Equal(t, []string{"ABCD-1234"}, shown)
	require.Equal(t, "ada", store.login(t).Account)

	revoke, err := github.Logout(t.Context(), store)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/settings/connections/applications/Iv1.other", revoke, "the saved login's app is revoked")
	_, err = github.Logout(t.Context(), store)
	require.ErrorIs(t, err, credential.ErrNotFound)
}

func TestLoginShowsOnlyAnHTTPSAddressAndKeepsNothingUnfinished(t *testing.T) {
	store := &removable{}
	shown := false
	_, err := github.Login(t.Context(), codeAt{address: "http://github.com/login/device"}, store, github.DefaultOAuthClientID, func(credential.DeviceAuthorization) error {
		shown = true
		return nil
	})
	require.ErrorContains(t, err, "an HTTPS URL is required")
	require.False(t, shown)

	_, err = github.Login(t.Context(), codeAt{address: "https://github.com/login/device"}, store, github.DefaultOAuthClientID, func(credential.DeviceAuthorization) error { return nil })
	require.ErrorContains(t, err, "incomplete login; nothing was saved")
	_, err = store.Get(t.Context(), github.CredentialKey)
	require.ErrorIs(t, err, credential.ErrNotFound)
}
