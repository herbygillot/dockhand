package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/spf13/cobra"
)

func (r *runtime) authCommand() *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage credentials", Args: cobra.NoArgs, Annotations: map[string]string{stateIndependentHelp: "true"}, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	command.AddCommand(r.authLoginCommand(), r.authStatusCommand(), r.authLogoutCommand())
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

			if name := overridingCredential(); name != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s takes precedence over the saved login; unset it to use this Keychain credential.\n", name)
			}
			if r.json {
				return r.emit(result)
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

func overridingCredential() string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return name
		}
	}
	return ""
}

func (r *runtime) authStatusCommand() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Check the selected GitHub credential and account", Args: cobra.NoArgs,
		Annotations: map[string]string{stateIndependentHelp: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := r.statusGitHub(cmd.Context(), nil)
			if r.json {
				if writeErr := r.emit(result); writeErr != nil {
					return writeErr
				}
			} else if err == nil {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to %s as %s using %s.\n", plain(result.Host), plain(result.Account), plain(string(result.Source)))
			}
			return err
		},
	}
}

func (r *runtime) authLogoutCommand() *cobra.Command {
	return &cobra.Command{Use: "logout", Short: "Remove Dockhand's saved GitHub credential from macOS Keychain",
		Long: "Remove only Dockhand's saved GitHub credential from macOS Keychain.\nEnvironment tokens and the GitHub CLI login remain available; this does not revoke the token at GitHub.",
		Args: cobra.NoArgs, Annotations: map[string]string{stateIndependentHelp: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := r.logoutGitHub(cmd.Context(), nil)
			if err != nil {
				return err
			}
			if r.json {
				return r.emit(result)
			}
			message := "No Dockhand GitHub credential was saved in macOS Keychain."
			if result.Removed {
				message = "Removed Dockhand's GitHub credential from macOS Keychain."
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), message+" Environment tokens and GitHub CLI credentials are unchanged; use dockhand auth status to check the selected account.")
			return err
		},
	}
}
