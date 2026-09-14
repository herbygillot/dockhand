package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/spf13/cobra"
)

func (r *runtime) authCommand() *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage credentials", Args: cobra.NoArgs, Annotations: map[string]string{stateIndependentHelp: "true"}, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	command.AddCommand(r.authLoginCommand())
	return command
}

func (r *runtime) authLoginCommand() *cobra.Command {
	clientID := os.Getenv("DOCKHAND_GITHUB_CLIENT_ID")
	if clientID == "" {
		clientID = app.DefaultGitHubOAuthClientID
	}
	noBrowser := false
	command := &cobra.Command{
		Use:         "login",
		Short:       "Log in to GitHub and save the credential in macOS Keychain",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{stateIndependentHelp: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := r.loginGitHub(cmd.Context(), app.GitHubLoginOptions{ClientID: clientID, Present: func(authorization credential.DeviceAuthorization) error {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Copy this one-time code: %s\n", plain(authorization.UserCode)); err != nil {
					return err
				}
				if noBrowser {
					_, err := fmt.Fprintf(cmd.ErrOrStderr(), "Open %s in a browser, enter the code, and authorize Dockhand.\nWaiting for authorization...\n", plain(authorization.VerificationURL))
					return err
				}
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Opening %s in your browser...\n", plain(authorization.VerificationURL)); err != nil {
					return err
				}
				if err := openBrowser(cmd, authorization.VerificationURL); err != nil {
					return fmt.Errorf("auth: opening browser: %w; rerun with --no-browser to open the URL manually", err)
				}
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Waiting for authorization...")
				return err
			}})
			if err != nil {
				return err
			}
			if r.json {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s as %s. Credential saved in %s.\n", plain(result.Host), plain(result.Account), plain(result.Storage))
			return err
		},
	}
	command.Flags().StringVar(&clientID, "client-id", clientID, "GitHub OAuth application client ID (DOCKHAND_GITHUB_CLIENT_ID)")
	command.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the authorization URL without opening a browser")
	return command
}

func openBrowser(cmd *cobra.Command, target string) error {
	path, err := exec.LookPath("open")
	if err != nil {
		return err
	}
	return exec.CommandContext(cmd.Context(), path, target).Run()
}
