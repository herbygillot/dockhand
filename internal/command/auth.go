package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/term"
)

// githubLogin logs in to GitHub with the device flow and keeps the login
// in the Keychain, as setup github and setup's offer do.
func githubLogin(ctx context.Context, s *settings.Settings, streams term.Streams, clientID string, noBrowser bool) error {
	login, err := github.Login(ctx, s.Host.AuthFlow, s.Host.AuthStore, clientID, func(authorization credential.DeviceAuthorization) error {
		fmt.Fprintf(streams.Err, "Copy this one-time code: %s\n", authorization.UserCode)
		if noBrowser {
			fmt.Fprintf(streams.Err, "Open %s, enter the code, and authorize dockhand.\nWaiting for authorization...\n", authorization.VerificationURL)
			return nil
		}
		fmt.Fprintf(streams.Err, "Opening %s in your browser...\n", authorization.VerificationURL)
		if err := s.Host.OpenBrowser(ctx, authorization.VerificationURL); err != nil {
			return fmt.Errorf("could not open the browser: %w; run dockhand setup github --no-browser to sign in using a URL you open manually", err)
		}
		fmt.Fprintln(streams.Err, "Waiting for authorization...")
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "Logged in to github.com as %s. Credentials are saved in the macOS Keychain and renew automatically if you use dockhand at least once every six months.\n", login.Account)
	if name := s.Signin().Token.Source; name != "" {
		fmt.Fprintf(streams.Err, "%s is set and overrides this login. Unset it to use the credentials saved in the Keychain.\n", name)
	}
	return nil
}

// githubLogout removes dockhand's login from the Keychain, as setup github
// --logout does.
func githubLogout(ctx context.Context, s *settings.Settings, streams term.Streams) error {
	revoke, err := github.Logout(ctx, s.Host.AuthStore)
	switch {
	case errors.Is(err, credential.ErrNotFound):
		fmt.Fprintln(streams.Out, "No dockhand GitHub credentials are saved in the Keychain.")
	case err != nil:
		return err
	default:
		fmt.Fprintln(streams.Out, "Removed dockhand's GitHub login from the Keychain. GH_TOKEN, GITHUB_TOKEN, and the GitHub CLI's login are unchanged.")
		fmt.Fprintf(streams.Out, "To also revoke dockhand’s access on GitHub, visit %s\n", revoke)
	}
	return nil
}
