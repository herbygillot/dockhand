package command

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/github"
)

// credentials is where the GitHub login is kept.
type credentials interface {
	credential.Store
	credential.Remover
}

// The login's parts, which tests stand in for.
var (
	authFlow  credential.DeviceFlow = &github.DeviceFlow{HTTP: http.DefaultClient}
	authStore credentials           = keychain.Store{}
	authAPI                         = func(store credential.Store) *github.Client {
		return &github.Client{HTTP: http.DefaultClient, Credentials: github.SystemCredentials{Store: store, Key: github.CredentialKey}}
	}
	openBrowser = func(ctx context.Context, url string) error {
		path, err := exec.LookPath("open")
		if err != nil {
			return err
		}
		return exec.CommandContext(ctx, path, url).Run()
	}
)

func authCommand(streams Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in to GitHub, for submit",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(authLoginCommand(streams), authStatusCommand(streams), authLogoutCommand(streams))
	return cmd
}

func authLoginCommand(streams Streams) *cobra.Command {
	clientID := firstOf(os.Getenv("DOCKHAND_GITHUB_CLIENT_ID"), github.DefaultOAuthClientID)
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to GitHub and keep the login in the macOS Keychain",
		Long: `Logs in to GitHub with a one-time code, authorizing dockhand to open pull
requests from your fork (the public_repo scope), and keeps the login in the
macOS Keychain. GH_TOKEN or GITHUB_TOKEN, when set, take precedence over it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			value, err := authFlow.Authorize(ctx, clientID, func(authorization credential.DeviceAuthorization) error {
				fmt.Fprintf(streams.Err, "Copy this one-time code: %s\n", authorization.UserCode)
				if noBrowser {
					fmt.Fprintf(streams.Err, "Open %s, enter the code, and authorize dockhand.\nWaiting for authorization...\n", authorization.VerificationURL)
					return nil
				}
				fmt.Fprintf(streams.Err, "Opening %s in your browser...\n", authorization.VerificationURL)
				if err := openBrowser(ctx, authorization.VerificationURL); err != nil {
					return fmt.Errorf("opening the browser: %w; run dockhand auth login --no-browser and open the address yourself", err)
				}
				fmt.Fprintln(streams.Err, "Waiting for authorization...")
				return nil
			})
			if err != nil {
				return err
			}
			if value.Secret == "" || value.Account == "" {
				return errors.New("GitHub returned an incomplete login; nothing was saved")
			}
			if err := authStore.Put(ctx, github.CredentialKey, value.Secret); err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "Logged in to github.com as %s, kept in the macOS Keychain.\n", value.Account)
			if name := overridingToken(); name != "" {
				fmt.Fprintf(streams.Err, "%s is set and takes precedence over this login; unset it to use the Keychain's.\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", clientID, "the GitHub OAuth application's client ID (DOCKHAND_GITHUB_CLIENT_ID)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the address instead of opening a browser")
	return cmd
}

func authStatusCommand(streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which GitHub account submit would use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := authAPI(authStore)
			account, err := client.AuthenticatedUser(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "Logged in to github.com as %s, using %s.\n", account, client.CredentialSource())
			return nil
		},
	}
}

func authLogoutCommand(streams Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove dockhand's GitHub login from the macOS Keychain",
		Long: `Removes only the login dockhand keeps in the macOS Keychain. GH_TOKEN,
GITHUB_TOKEN, and the GitHub CLI's login are untouched, and the token is not
revoked at GitHub.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := authStore.Delete(cmd.Context(), github.CredentialKey)
			switch {
			case errors.Is(err, credential.ErrNotFound):
				fmt.Fprintln(streams.Out, "dockhand keeps no GitHub login in the Keychain.")
			case err != nil:
				return err
			default:
				fmt.Fprintln(streams.Out, "Removed dockhand's GitHub login from the Keychain. GH_TOKEN, GITHUB_TOKEN, and the GitHub CLI's login are unchanged.")
			}
			return nil
		},
	}
}

func overridingToken() string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return name
		}
	}
	return ""
}
