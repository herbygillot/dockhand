package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
)

// overridingToken is the variable whose token takes precedence over
// dockhand's login, GH_TOKEN or GITHUB_TOKEN, or empty.
func overridingToken() string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return name
		}
	}
	return ""
}

// githubLogin logs in to GitHub with the device flow and keeps the login
// in the Keychain, as setup github and setup's offer do.
func (h *host) githubLogin(ctx context.Context, streams Streams, clientID string, noBrowser bool) error {
	login, err := github.Login(ctx, h.authFlow, h.authStore, clientID, func(authorization credential.DeviceAuthorization) error {
		fmt.Fprintf(streams.Err, "Copy this one-time code: %s\n", authorization.UserCode)
		if noBrowser {
			fmt.Fprintf(streams.Err, "Open %s, enter the code, and authorize dockhand.\nWaiting for authorization...\n", authorization.VerificationURL)
			return nil
		}
		fmt.Fprintf(streams.Err, "Opening %s in your browser...\n", authorization.VerificationURL)
		if err := h.openBrowser(ctx, authorization.VerificationURL); err != nil {
			return fmt.Errorf("could not open the browser: %w; run dockhand setup github --no-browser to sign in using a URL you open manually", err)
		}
		fmt.Fprintln(streams.Err, "Waiting for authorization...")
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "Logged in to github.com as %s. Credentials are saved in the macOS Keychain and renew automatically if you use dockhand at least once every six months.\n", login.Account)
	if name := overridingToken(); name != "" {
		fmt.Fprintf(streams.Err, "%s is set and overrides this login. Unset it to use the credentials saved in the Keychain.\n", name)
	}
	return nil
}

// githubLogout removes dockhand's login from the Keychain, as setup github
// --logout does.
func (h *host) githubLogout(ctx context.Context, streams Streams) error {
	revoke, err := github.Logout(ctx, h.authStore)
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
