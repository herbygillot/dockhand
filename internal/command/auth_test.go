package command

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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
	value credential.Login
	err   error
}

func (f deviceFlow) Authorize(_ context.Context, clientID string, present func(credential.DeviceAuthorization) error) (credential.Login, error) {
	if err := present(credential.DeviceAuthorization{UserCode: "ABCD-1234", VerificationURL: "https://github.com/login/device"}); err != nil {
		return credential.Login{}, err
	}
	return f.value, f.err
}

// login is a renewing login for the account, its access token secret.
func login(secret, account string) credential.Login {
	return credential.Login{Access: secret, AccessExpiry: time.Now().Add(8 * time.Hour), Refresh: "refresh-" + secret, RefreshExpiry: time.Now().AddDate(0, 6, 0), Account: account, ClientID: "fixture-client"}
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
	store := withAuth(t, deviceFlow{value: login("secret-token", "ada")})
	var opened string
	openBrowser = func(_ context.Context, url string) error { opened = url; return nil }

	out, errs, err := dockhand(t, "auth", "login")
	require.NoError(t, err)
	require.Contains(t, errs, "Copy this one-time code: ABCD-1234\nOpening https://github.com/login/device in your browser...\n")
	require.Equal(t, "https://github.com/login/device", opened)
	require.Equal(t, "Logged in to github.com as ada, kept in the macOS Keychain. It renews itself while dockhand is used at least once every six months.\n", out)
	saved, err := credential.DecodeLogin(store[github.CredentialKey])
	require.NoError(t, err)
	require.Equal(t, "secret-token", saved.Access)

	// The rest keeps the GitHub CLI out of reach, so a login it has on
	// this machine can't stand in; init needs git, though.
	emptyPath := os.Getenv("PATH")
	t.Setenv("PATH", path)
	newWorld(t)
	out, _, err = dockhand(t, "init")
	require.NoError(t, err)
	until := time.Now().AddDate(0, 6, 0).Format("2 January 2006")
	require.Contains(t, out, "  Publishing   ✓ GitHub login in the Keychain, renewing itself until "+until+"\n")
	t.Setenv("PATH", emptyPath)

	out, _, err = dockhand(t, "auth", "status")
	require.NoError(t, err)
	require.Equal(t, "Logged in to github.com as ada, using Dockhand macOS Keychain.\nIt renews itself until "+until+", six months from its last use.\n", out)
	// With --json, the same, for a script (the rc6 full stage, D-C5).
	out, _, err = dockhand(t, "--json", "auth", "status")
	require.NoError(t, err)
	var envelope struct {
		Result authStatusJSON `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &envelope), out)
	require.Equal(t, "ada", envelope.Result.Account)
	require.Equal(t, "Dockhand macOS Keychain", envelope.Result.Source)
	require.NotNil(t, envelope.Result.RenewsUntil)

	out, _, err = dockhand(t, "auth", "logout")
	require.NoError(t, err)
	require.Contains(t, out, "Removed dockhand's GitHub login from the Keychain.")
	require.Contains(t, out, "GitHub still has dockhand authorized until you revoke it: https://github.com/settings/connections/applications/fixture-client\n")
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

// A login kept by an earlier dockhand, or one that expired, says so in
// the setup line, and a device flow naming an address that isn't https is
// opened nowhere (the auth flow review's plan, step 4).
func TestALoginThatCantRenewIsSaid(t *testing.T) {
	store := withAuth(t, deviceFlow{value: login("secret-token", "ada")})
	store[github.CredentialKey] = "gho_a_bare_token_from_an_earlier_dockhand"
	require.Equal(t, "! the saved GitHub login is from an earlier dockhand; run dockhand setup github", publishing(t.Context()))
	expired := login("secret-token", "ada")
	expired.RefreshExpiry = time.Now().Add(-time.Hour)
	saved, err := expired.Encode()
	require.NoError(t, err)
	store[github.CredentialKey] = saved
	require.Equal(t, "! the GitHub login expired after six months unused; run dockhand setup github", publishing(t.Context()))
}

func TestADeviceFlowAddressThatIsntHTTPSIsntOpened(t *testing.T) {
	withAuth(t, plainFlow{})
	opened := ""
	openBrowser = func(_ context.Context, url string) error { opened = url; return nil }
	_, _, err := dockhand(t, "auth", "login")
	require.ErrorContains(t, err, "isn't an https address; nothing was opened")
	require.Empty(t, opened)
}

// plainFlow names an http address to authorize at.
type plainFlow struct{}

func (plainFlow) Authorize(_ context.Context, _ string, present func(credential.DeviceAuthorization) error) (credential.Login, error) {
	return credential.Login{}, present(credential.DeviceAuthorization{UserCode: "ABCD-1234", VerificationURL: "http://example.invalid/device"})
}
