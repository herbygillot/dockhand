package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
)

type memoryStore map[credential.Key]string

func (m memoryStore) Get(_ context.Context, key credential.Key) (string, error) {
	value, ok := m[key]
	if !ok {
		return "", credential.ErrNotFound
	}
	return value, nil
}

func (m memoryStore) Put(_ context.Context, key credential.Key, value string) error {
	m[key] = value
	return nil
}

func (m memoryStore) Delete(_ context.Context, key credential.Key) error {
	if _, ok := m[key]; !ok {
		return credential.ErrNotFound
	}
	delete(m, key)
	return nil
}

type deviceFlow struct {
	value credential.Value
	err   error
}

func (f deviceFlow) Authorize(_ context.Context, clientID string, present func(credential.DeviceAuthorization) error) (credential.Value, error) {
	if err := present(credential.DeviceAuthorization{UserCode: "ABCD-1234", VerificationURL: "https://github.com/login/device"}); err != nil {
		return credential.Value{}, err
	}
	return f.value, f.err
}

func withAuth(t *testing.T, flow credential.DeviceFlow) memoryStore {
	t.Helper()
	store := memoryStore{}
	realFlow, realStore, realAPI, realBrowser := authFlow, authStore, authAPI, openBrowser
	t.Cleanup(func() { authFlow, authStore, authAPI, openBrowser = realFlow, realStore, realAPI, realBrowser })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"login": "ada"})
	}))
	t.Cleanup(server.Close)
	authFlow, authStore = flow, store
	authAPI = func(store credential.Store) *github.Client {
		return &github.Client{HTTP: server.Client(), Config: github.Config{BaseURL: server.URL}, Credentials: github.SystemCredentials{Store: store, Key: github.CredentialKey}}
	}
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
	return store
}

func TestAuthLoginStatusAndLogout(t *testing.T) {
	path := os.Getenv("PATH")
	store := withAuth(t, deviceFlow{value: credential.Value{Secret: "secret-token", Account: "ada"}})
	var opened string
	openBrowser = func(_ context.Context, url string) error { opened = url; return nil }

	out, errs, err := dockhand(t, "auth", "login")
	require.NoError(t, err)
	require.Contains(t, errs, "Copy this one-time code: ABCD-1234\nOpening https://github.com/login/device in your browser...\n")
	require.Equal(t, "https://github.com/login/device", opened)
	require.Equal(t, "Logged in to github.com as ada, kept in the macOS Keychain.\n", out)
	require.Equal(t, "secret-token", store[github.CredentialKey])

	// The rest keeps the GitHub CLI out of reach, so a login it has on
	// this machine can't stand in; init needs git, though.
	emptyPath := os.Getenv("PATH")
	t.Setenv("PATH", path)
	newWorld(t)
	out, _, err = dockhand(t, "init")
	require.NoError(t, err)
	require.Contains(t, out, "  Publishing   ✓ GitHub login in the Keychain\n")
	t.Setenv("PATH", emptyPath)

	out, _, err = dockhand(t, "auth", "status")
	require.NoError(t, err)
	require.Equal(t, "Logged in to github.com as ada, using Dockhand macOS Keychain.\n", out)

	out, _, err = dockhand(t, "auth", "logout")
	require.NoError(t, err)
	require.Contains(t, out, "Removed dockhand's GitHub login from the Keychain.")
	out, _, err = dockhand(t, "auth", "logout")
	require.NoError(t, err)
	require.Equal(t, "dockhand keeps no GitHub login in the Keychain.\n", out)
	_, _, err = dockhand(t, "auth", "status")
	require.Error(t, err, "no login anywhere")
}

func TestAFailedLoginSavesNothing(t *testing.T) {
	store := withAuth(t, deviceFlow{err: errors.New("the code expired")})
	_, errs, err := dockhand(t, "auth", "login", "--no-browser")
	require.ErrorContains(t, err, "the code expired")
	require.Contains(t, errs, "Open https://github.com/login/device, enter the code, and authorize dockhand.")
	require.Empty(t, store)
}
