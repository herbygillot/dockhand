package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/term"
)

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

func TestSetupGitHubLogsInAndOutAndSetupNamesTheLogin(t *testing.T) {
	t.Parallel()
	path := cmdtest.Getenv(t, "PATH")
	store := cmdtest.WithAuth(t, deviceFlow{value: login("secret-token", "ada")})
	var opened string
	cmdtest.Hosted(t).OpenBrowser = func(_ context.Context, url string) error { opened = url; return nil }

	out, errs, err := cmdtest.Dockhand(t, "setup", "github")
	require.NoError(t, err)
	require.Contains(t, errs, "Copy this one-time code: ABCD-1234\nOpening https://github.com/login/device in your browser...\n")
	require.Equal(t, "https://github.com/login/device", opened)
	require.Equal(t, cmdtest.Flat("Logged in to github.com as ada. Credentials are saved in the macOS Keychain and renew automatically if you use dockhand at least once every six months.\n"), cmdtest.Flat(out))
	saved, err := credential.DecodeLogin(store[github.CredentialKey])
	require.NoError(t, err)
	require.Equal(t, "secret-token", saved.Access)

	// The rest keeps the GitHub CLI out of reach, so a login it has on
	// this machine can't stand in; setup needs git, though.
	emptyPath := cmdtest.Getenv(t, "PATH")
	cmdtest.Setenv(t, "PATH", path)
	cmdtest.NewWorld(t)
	out, _, err = cmdtest.Dockhand(t, "setup", "--yes")
	require.NoError(t, err)
	until := time.Now().AddDate(0, 6, 0).Format("2 January 2006")
	require.Contains(t, out, "  Publishing   ✓ ada, from dockhand's login in the Keychain, renewing itself until "+until+"\n")
	// setup --json says the same for a script, as auth status --json did
	// before it went (the rc9 full stage's D-C4).
	out, _, err = cmdtest.Dockhand(t, "--json", "setup")
	require.NoError(t, err)
	var report struct {
		Result setupReport `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &report), out)
	require.Equal(t, "ada", report.Result.GitHub.Account)
	require.Equal(t, "Dockhand macOS Keychain", report.Result.GitHub.Source)
	require.NotNil(t, report.Result.GitHub.RenewsUntil)
	require.NotEmpty(t, report.Result.Checkout)
	require.NotEmpty(t, report.Result.Git.Version)
	require.Equal(t, "macports/macports-ports", report.Result.Upstream.Repository)
	cmdtest.Setenv(t, "GH_TOKEN", "secret-token")
	_, said := publishing(t.Context(), cmdtest.TestSettings(t))
	require.Equal(t, githubLoginJSON{Account: "ada", Source: "GH_TOKEN"}, said, "GH_TOKEN takes precedence, and doesn't renew")
	cmdtest.Setenv(t, "GH_TOKEN", "")
	cmdtest.Setenv(t, "PATH", emptyPath)

	out, _, err = cmdtest.Dockhand(t, "setup", "github", "--logout")
	require.NoError(t, err)
	require.Contains(t, out, "Removed dockhand's GitHub login from the Keychain.")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("To also revoke dockhand’s access on GitHub, visit https://github.com/settings/connections/applications/fixture-client\n"))
	out, _, err = cmdtest.Dockhand(t, "setup", "github", "--logout")
	require.NoError(t, err)
	require.Equal(t, "No dockhand GitHub credentials are saved in the Keychain.\n", out)
	line, said := publishing(t.Context(), cmdtest.TestSettings(t))
	require.Equal(t, "  not set up: dockhand setup github, when you're ready to submit", line, "no login anywhere")
	require.Equal(t, githubLoginJSON{Problem: "not set up"}, said)
}

func TestAFailedLoginSavesNothing(t *testing.T) {
	t.Parallel()
	store := cmdtest.WithAuth(t, deviceFlow{err: errors.New("the code expired")})
	_, errs, err := cmdtest.Dockhand(t, "setup", "github", "--no-browser")
	require.ErrorContains(t, err, "the code expired")
	require.Contains(t, errs, "Open https://github.com/login/device, enter the code, and authorize dockhand.")
	require.Empty(t, store)
}

// A login kept by an earlier dockhand, or one that expired, says so in
// the setup line, and a device flow naming an address that isn't https is
// opened nowhere (the auth flow review's plan, step 4).
func TestALoginThatCantRenewIsSaid(t *testing.T) {
	t.Parallel()
	store := cmdtest.WithAuth(t, deviceFlow{value: login("secret-token", "ada")})
	store[github.CredentialKey] = "gho_a_bare_token_from_an_earlier_dockhand"
	line, _ := publishing(t.Context(), cmdtest.TestSettings(t))
	require.Equal(t, "! the saved GitHub login uses an older format; run dockhand setup github", line)
	expired := login("secret-token", "ada")
	expired.AccessExpiry, expired.RefreshExpiry = time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
	saved, err := expired.Encode()
	require.NoError(t, err)
	store[github.CredentialKey] = saved
	line, said := publishing(t.Context(), cmdtest.TestSettings(t))
	require.Equal(t, "! the GitHub login expired after six months of inactivity; run dockhand setup github", line)
	require.Equal(t, githubLoginJSON{Problem: "the GitHub login expired after six months of inactivity"}, said)
}

func TestADeviceFlowAddressThatIsntHTTPSIsntOpened(t *testing.T) {
	t.Parallel()
	cmdtest.WithAuth(t, plainFlow{})
	opened := ""
	cmdtest.Hosted(t).OpenBrowser = func(_ context.Context, url string) error { opened = url; return nil }
	_, _, err := cmdtest.Dockhand(t, "setup", "github")
	require.ErrorContains(t, err, "an HTTPS URL is required, so the browser was not opened")
	require.Empty(t, opened)
}

// plainFlow names an http address to authorize at.
type plainFlow struct{}

func (plainFlow) Authorize(_ context.Context, _ string, present func(credential.DeviceAuthorization) error) (credential.Login, error) {
	return credential.Login{}, present(credential.DeviceAuthorization{UserCode: "ABCD-1234", VerificationURL: "http://example.invalid/device"})
}

// A refusal whose producer gave what moves it on says it last, as a Next:
// line, and stays the refusal, its exit code its kind's; a composed exit
// error says its own (R3-31).
func TestARefusalsNextIsSaidLast(t *testing.T) {
	t.Parallel()
	ended := refusal.New(refusal.Auth, "the GitHub login expired after six months of inactivity").
		Then(refusal.Next{Verb: refusal.VerbSetup, Args: []string{"github"}})
	err := view.SayNext(fmt.Errorf("submit: %w", ended))
	require.EqualError(t, err, "submit: the GitHub login expired after six months of inactivity\nNext: dockhand setup github")
	require.Equal(t, refusal.Auth, refusal.KindOf(err))
	composed := term.Exitf(term.ExitAttention, "held")
	require.Equal(t, composed, view.SayNext(composed))
	plain := errors.New("plain")
	require.Equal(t, plain, view.SayNext(plain))
}
