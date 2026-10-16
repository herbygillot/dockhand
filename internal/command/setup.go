package command

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildenv/tart"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
)

// setupGitHubCommand logs in to GitHub, or with --logout removes the
// login.
func setupGitHubCommand(streams Streams) *cobra.Command {
	clientID := cmp.Or(os.Getenv("DOCKHAND_GITHUB_CLIENT_ID"), github.DefaultOAuthClientID)
	var noBrowser, logout bool
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Log in to GitHub and keep the login in the macOS Keychain",
		Long: `Logs in to GitHub with a one-time code, authorizing dockhand to open pull
requests from your fork (the public_repo scope), and keeps the login in the
macOS Keychain. GH_TOKEN or GITHUB_TOKEN, when set, take precedence over it.

--logout removes only the login dockhand keeps in the Keychain. GH_TOKEN,
GITHUB_TOKEN, and the GitHub CLI's login are untouched. GitHub keeps dockhand
authorized until you revoke it on GitHub's page for it, which it names.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if logout {
				return githubLogout(cmd.Context(), streams)
			}
			return githubLogin(cmd.Context(), streams, clientID, noBrowser)
		},
	}
	cmd.Flags().StringVar(&clientID, "client-id", clientID, "the GitHub OAuth application's client ID (DOCKHAND_GITHUB_CLIENT_ID)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the address instead of opening a browser")
	cmd.Flags().BoolVar(&logout, "logout", false, "remove dockhand's GitHub login from the Keychain instead")
	return cmd
}

// offerSetup offers, on a terminal, what setup found missing, each in
// turn: the GitHub login, the maintainers line the ports naming that
// login write, written to the configuration file, and, last and off by
// default for its download, the Tart image of this Mac's macOS. Each is
// the person's to decline, and one that fails is said, and the rest go on.
// It says the maintainers line it wrote, or nothing.
func offerSetup(ctx context.Context, s *settings, e *engine.Engine, streams Streams, file config.File, configPath string) (string, error) {
	written := ""
	fmt.Fprintln(streams.Out)
	if overridingToken() == "" {
		if _, err := github.SavedLogin(ctx, authStore); err != nil {
			login, err := streams.askYes("? Log in to GitHub now, for submit and checks in your fork? [Y/n] ", true)
			if err != nil {
				return written, err
			}
			if login {
				if err := githubLogin(ctx, streams, cmp.Or(os.Getenv("DOCKHAND_GITHUB_CLIENT_ID"), github.DefaultOAuthClientID), false); err != nil {
					fmt.Fprintf(streams.Err, "Not logged in: %v\n", err)
				}
			}
		}
	}
	if file.Maintainer == "" {
		suggestion := e.SuggestMaintainerAtMaster(ctx)
		if len(suggestion.Spellings) > 0 {
			line := suggestion.Spellings[0].Maintainer.String()
			write, err := streams.askYes(fmt.Sprintf("? Write maintainer = %q to %s, as the ports naming @%s write it? [Y/n] ", line, config.Tilde(configPath), suggestion.Login), true)
			if err != nil {
				return written, err
			}
			if write {
				if err := config.SetMaintainer(configPath, line); err != nil {
					return "", err
				}
				written = line
				fmt.Fprintf(streams.Out, "Wrote maintainer = %q, for --mine, create, and serve's daily look.\n", line)
			}
		} else {
			fmt.Fprintf(streams.Out, "· maintainer: %s, for --mine, create, and serve's daily look\n", engine.MaintainerWords(suggestion, config.Tilde(configPath)))
		}
	}
	if images := images(); images != nil {
		makeImage := false
		if status, err := images.Status(ctx); err == nil && !status.HasHostImage() {
			if makeImage, err = streams.askYes(fmt.Sprintf("? Make the Tart image for this Mac's macOS now? It downloads macOS and takes up to %s of disk. [y/N] ", tart.SetupDisk), false); err != nil {
				return written, err
			}
		}
		if makeImage {
			setup := setupTartCommand(s, streams)
			setup.SetContext(ctx)
			if err := setup.RunE(setup, nil); err != nil {
				fmt.Fprintf(streams.Err, "No image made: %v\n", err)
			}
		}
	}
	return written, nil
}
