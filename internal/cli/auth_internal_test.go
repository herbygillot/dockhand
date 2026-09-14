package cli

import (
	"bytes"
	"context"
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
