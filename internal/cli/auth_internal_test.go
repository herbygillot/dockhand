package cli

import (
	"bytes"
	"context"
	"github.com/herbygillot/dockhand/internal/forge/github"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/stretchr/testify/require"
)

func TestAuthLoginCommandPresentsDeviceCodeAndRendersResult(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
			presented := false
			runtime := &runtime{json: jsonOutput, loginGitHub: func(_ context.Context, options app.GitHubLoginOptions) (app.GitHubLoginResult, error) {
				require.Equal(t, "fixture-client", options.ClientID)
				presented = true
				require.NoError(t, options.Present(credential.DeviceAuthorization{UserCode: "ABCD-EFGH", VerificationURL: "https://github.com/login/device"}))
				return app.GitHubLoginResult{Host: "github.com", Account: "fixture-user", Storage: "macOS Keychain"}, nil
			}}
			command := runtime.authLoginCommand()
			command.SetArgs([]string{"--client-id", "fixture-client", "--no-browser"})
			var output, diagnostics bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&diagnostics)
			require.NoError(t, command.ExecuteContext(t.Context()))
			require.True(t, presented)
			require.Contains(t, diagnostics.String(), "ABCD-EFGH")
			require.Contains(t, diagnostics.String(), "https://github.com/login/device")
			if jsonOutput {
				require.JSONEq(t, `{"host":"github.com","account":"fixture-user","storage":"macOS Keychain"}`, output.String())
			} else {
				require.Contains(t, output.String(), "Logged in to github.com as fixture-user")
			}
		})
	}
}

func TestAuthStatusJSONIncludesRejectedSource(t *testing.T) {
	r := &runtime{json: true, statusGitHub: func(context.Context, *github.Client) (app.GitHubAuthStatus, error) {
		return app.GitHubAuthStatus{Host: "github.com", Source: github.SourceKeychain}, github.ErrAuthentication
	}}
	command := r.authStatusCommand()
	command.SilenceErrors = true
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	require.ErrorIs(t, command.ExecuteContext(t.Context()), github.ErrAuthentication)
	require.JSONEq(t, `{"host":"github.com","source":"Dockhand macOS Keychain","authenticated":false}`, output.String())
}

func TestAuthLogoutJSONReportsRemoval(t *testing.T) {
	r := &runtime{json: true, logoutGitHub: func(context.Context, credential.Remover) (app.GitHubLogoutResult, error) {
		return app.GitHubLogoutResult{Host: "github.com", Storage: "macOS Keychain", Removed: true}, nil
	}}
	command := r.authLogoutCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	require.NoError(t, command.ExecuteContext(t.Context()))
	require.JSONEq(t, `{"host":"github.com","storage":"macOS Keychain","removed":true}`, output.String())
}

func TestLoginWarnsAboutEnvironmentOverrideWithoutPrintingValue(t *testing.T) {
	t.Setenv("GH_TOKEN", "a-secret-environment-token")
	r := &runtime{json: true, loginGitHub: func(context.Context, app.GitHubLoginOptions) (app.GitHubLoginResult, error) {
		return app.GitHubLoginResult{Host: "github.com", Account: "fixture", Storage: "macOS Keychain"}, nil
	}}
	command := r.authLoginCommand()
	var output, diagnostics bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&diagnostics)
	require.NoError(t, command.ExecuteContext(t.Context()))
	require.Contains(t, diagnostics.String(), "GH_TOKEN takes precedence")
	require.NotContains(t, diagnostics.String(), "a-secret-environment-token")
	require.JSONEq(t, `{"host":"github.com","account":"fixture","storage":"macOS Keychain"}`, output.String())
}
