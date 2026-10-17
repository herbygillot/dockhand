package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macos"
)

// credentials is where the GitHub login is kept.
type credentials interface {
	credential.Store
	credential.Remover
}

// The login's parts, which tests stand in for.
var (
	authFlow    credential.DeviceFlow = &github.DeviceFlow{}
	authStore   credentials           = keychain.Store{}
	authAPI                           = github.SystemClient
	openBrowser                       = macos.Open
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
func githubLogin(ctx context.Context, streams Streams, clientID string, noBrowser bool) error {
	value, err := authFlow.Authorize(ctx, clientID, func(authorization credential.DeviceAuthorization) error {
		// The address is opened, or followed, only where it's
		// GitHub's over https (the auth flow review's plan, step 4).
		if address, err := url.Parse(authorization.VerificationURL); err != nil || address.Scheme != "https" || address.Host == "" {
			return fmt.Errorf("GitHub returned an invalid authorization URL, %q; an HTTPS URL is required, so the browser was not opened", authorization.VerificationURL)
		}
		fmt.Fprintf(streams.Err, "Copy this one-time code: %s\n", authorization.UserCode)
		if noBrowser {
			fmt.Fprintf(streams.Err, "Open %s, enter the code, and authorize dockhand.\nWaiting for authorization...\n", authorization.VerificationURL)
			return nil
		}
		fmt.Fprintf(streams.Err, "Opening %s in your browser...\n", authorization.VerificationURL)
		if err := openBrowser(ctx, authorization.VerificationURL); err != nil {
			return fmt.Errorf("could not open the browser: %w; run dockhand setup github --no-browser to sign in using a URL you open manually", err)
		}
		fmt.Fprintln(streams.Err, "Waiting for authorization...")
		return nil
	})
	if err != nil {
		return err
	}
	saved, err := value.Encode()
	if err != nil {
		return errors.New("GitHub returned an incomplete login; nothing was saved")
	}
	if err := authStore.Put(ctx, github.CredentialKey, saved); err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "Logged in to github.com as %s. Credentials are saved in the macOS Keychain and renew automatically if you use dockhand at least once every six months.\n", value.Account)
	if name := overridingToken(); name != "" {
		fmt.Fprintf(streams.Err, "%s is set and overrides this login. Unset it to use the credentials saved in the Keychain.\n", name)
	}
	return nil
}

// githubLogout removes dockhand's login from the Keychain, as setup github
// --logout does.
func githubLogout(ctx context.Context, streams Streams) error {
	clientID := github.DefaultOAuthClientID
	if login, err := github.SavedLogin(ctx, authStore); err == nil || login.ClientID != "" {
		clientID = cmp.Or(login.ClientID, clientID)
	}
	err := authStore.Delete(ctx, github.CredentialKey)
	switch {
	case errors.Is(err, credential.ErrNotFound):
		fmt.Fprintln(streams.Out, "No dockhand GitHub credentials are saved in the Keychain.")
	case err != nil:
		return err
	default:
		fmt.Fprintln(streams.Out, "Removed dockhand's GitHub login from the Keychain. GH_TOKEN, GITHUB_TOKEN, and the GitHub CLI's login are unchanged.")
		fmt.Fprintf(streams.Out, "To also revoke dockhand’s access on GitHub, visit %s\n", github.RevocationPage(clientID))
	}
	return nil
}
