package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/progress"
)

// LoginLock is the file processes renewing dockhand's login take in turn,
// beside its configuration: ~/.dockhand/github-login.lock. The command
// line sets it where its configuration is elsewhere.
var LoginLock = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "dockhand-github-login.lock")
	}
	return filepath.Join(home, ".dockhand", "github-login.lock")
}()

// lockWait is how long a renewal waits for another process's.
var lockWait = 30 * time.Second

// WithRenewal is the credentials renewing their login through flow, under
// the lock at path, for tests and other GitHubs; flow's endpoint is
// GitHub's where it's empty.
func (s SystemCredentials) WithRenewal(flow *DeviceFlow, path string) SystemCredentials {
	s.Flow, s.Lock = flow, path
	if s.renewed == nil {
		s.renewed = &pendingLogin{}
	}
	return s
}

// pendingLogin is a renewed login the store didn't take: the refresh
// token it replaced is spent, so it's used until a save takes.
type pendingLogin struct {
	mu    sync.Mutex
	login *credential.Login
	// from is the refresh token of the login it renewed: a login in the
	// store with another is one saved since, as by dockhand setup github.
	from string
}

func (p *pendingLogin) get() *credential.Login {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.login
}

func (p *pendingLogin) set(login *credential.Login, from string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.login, p.from = login, from
}

// renewedFrom is the refresh token of the login the pending one renewed.
func (p *pendingLogin) renewedFrom() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.from
}

// errOldLogin and errExpiredLogin are a saved login that can't renew
// itself: one an earlier dockhand kept, a bare token, or one unused six
// months.
// ErrLoginEnded is a saved login that can't renew itself any more, which
// only dockhand setup github answers: serve says it once, rather than a
// failure for each pull request.
var ErrLoginEnded = errors.New("github: the saved login can't renew itself")

// loginEnded is an error that is ErrLoginEnded, in its own words.
type loginEnded struct{ error }

func (e loginEnded) Unwrap() error        { return e.error }
func (e loginEnded) Is(target error) bool { return target == ErrLoginEnded }

var (
	errOldLogin     = loginEnded{fmt.Errorf("%w: the saved GitHub login is from an earlier dockhand; run dockhand setup github", ErrAuthentication)}
	errExpiredLogin = loginEnded{fmt.Errorf("%w: the GitHub login expired after six months unused; run dockhand setup github", ErrAuthentication)}
)

// login is the token of the login the store keeps, renewed where its
// access token is within renewBefore of expiring, or is rejected's; found
// is false where the store keeps none (the GitHub auth flow review's
// plan, step 3).
func (s SystemCredentials) login(ctx context.Context, rejected Token) (Token, bool, error) {
	saved, err := s.saved(ctx)
	if errors.Is(err, credential.ErrNotFound) {
		return Token{}, false, nil
	}
	if err != nil {
		return Token{}, true, err
	}
	if token, ok := usable(saved, rejected, s.clock()); ok {
		return token, true, nil
	}
	if !s.clock().Before(saved.RefreshExpiry) {
		return Token{}, true, errExpiredLogin
	}
	// Renewing spends the refresh token, which is good once: two processes
	// renewing at once would leave the second with a spent one, and the
	// person logged out. Each takes the lock, and reads the login again
	// under it, where the other may have renewed it already.
	waiting, cancel := context.WithTimeout(ctx, lockWait)
	defer cancel()
	path := s.Lock
	if path == "" {
		path = LoginLock
	}
	lock, err := filelock.Acquire(waiting, path, filelock.Exclusive)
	if err != nil {
		return Token{}, true, fmt.Errorf("github: waiting to renew the GitHub login, which another dockhand is renewing: %w", err)
	}
	defer lock.Close()
	if saved, err = s.saved(ctx); err != nil {
		return Token{}, true, err
	}
	if token, ok := usable(saved, rejected, s.clock()); ok {
		return token, true, nil
	}
	if !s.clock().Before(saved.RefreshExpiry) {
		return Token{}, true, errExpiredLogin
	}
	renewed, err := s.refresh(ctx, saved)
	if err != nil {
		return Token{}, true, err
	}
	s.keep(ctx, renewed, saved.Refresh)
	return tokenOf(renewed), true, nil
}

// saved is the login the store keeps, or the one renewed it didn't take.
// A login saved since the renewal, as by dockhand setup github, is the
// store's: the pending one, of the login it replaced, goes. It stood in
// front of the new login, and a later save wrote it over it: serve, given
// a new login after its own was revoked, never read it (the rc8 full
// stage's D-C3).
func (s SystemCredentials) saved(ctx context.Context) (credential.Login, error) {
	if pending := s.renewed.get(); pending != nil {
		if value, err := s.Store.Get(ctx, s.Key); err == nil {
			if stored, err := credential.DecodeLogin(value); err == nil && stored.Refresh != s.renewed.renewedFrom() && stored.Refresh != pending.Refresh {
				s.renewed.set(nil, "")
				return stored, nil
			}
		}
		// A save that failed is tried again, the next time it's asked.
		if encoded, err := pending.Encode(); err == nil && s.Store.Put(ctx, s.Key, encoded) == nil {
			s.renewed.set(nil, "")
		}
		return *pending, nil
	}
	value, err := s.Store.Get(ctx, s.Key)
	if errors.Is(err, credential.ErrNotFound) {
		return credential.Login{}, err
	}
	// Locked, it keeps the login still: no other source is tried, as for
	// any login that's there (the rc10 full stage's F5).
	if errors.Is(err, credential.ErrLocked) {
		return credential.Login{}, fmt.Errorf("github: the saved GitHub login can't be read: %w, or GH_TOKEN set for the command stands in for it", err)
	}
	if err != nil {
		return credential.Login{}, fmt.Errorf("github: reading saved credential: %w", err)
	}
	login, err := credential.DecodeLogin(value)
	if err != nil {
		return credential.Login{}, errOldLogin
	}
	return login, nil
}

// keep saves a renewed login. Its refresh token replaced the old one,
// which is spent: one the store doesn't take is used from memory, and
// saved the next time the login is asked for, and said, since a process
// that ends before then leaves the person logged out.
func (s SystemCredentials) keep(ctx context.Context, renewed credential.Login, from string) {
	encoded, err := renewed.Encode()
	if err == nil {
		err = s.Store.Put(ctx, s.Key, encoded)
	}
	if err != nil {
		s.renewed.set(&renewed, from)
		progress.Report(ctx, "Couldn't save the renewed GitHub login to the Keychain (%v); this dockhand uses it, and tries again. If it ends first, run dockhand setup github.", err)
	}
}

// refresh renews the login with its refresh token. GitHub refusing the
// refresh token is the login's end; anything else, a network's failure or
// GitHub's, leaves the login as it was, to try again.
func (s SystemCredentials) refresh(ctx context.Context, saved credential.Login) (credential.Login, error) {
	flow := s.Flow
	if flow == nil {
		flow = &DeviceFlow{}
	}
	config, ctx := flow.config(ctx, saved.ClientID)
	token, err := config.TokenSource(ctx, &oauth2.Token{RefreshToken: saved.Refresh, Expiry: time.Unix(1, 0)}).Token()
	var refused *oauth2.RetrieveError
	switch {
	case errors.As(err, &refused) && (refused.ErrorCode == "bad_refresh_token" || refused.ErrorCode == "invalid_grant" || refused.ErrorCode == "unauthorized_client"):
		return credential.Login{}, loginEnded{fmt.Errorf("%w: GitHub refused to renew the login (%s); run dockhand setup github", ErrAuthentication, refused.ErrorCode)}
	case err != nil:
		return credential.Login{}, fmt.Errorf("github: renewing the GitHub login, which stays as it was: %w", RateLimitError(err))
	}
	secret, err := validToken(token.AccessToken)
	if err != nil {
		return credential.Login{}, err
	}
	renewal, err := renewalOf(token, flow.clock())
	if err != nil {
		return credential.Login{}, err
	}
	renewed := credential.Login{Access: secret, AccessExpiry: token.Expiry, Refresh: token.RefreshToken, RefreshExpiry: renewal, Account: saved.Account, ClientID: saved.ClientID}
	if !renewed.Complete() {
		return credential.Login{}, fmt.Errorf("github: GitHub renewed the login without an expiry; it stays as it was")
	}
	return renewed, nil
}

func (s SystemCredentials) clock() time.Time {
	if s.Flow != nil {
		return s.Flow.clock()
	}
	return time.Now()
}

// usable is the login's token, where it's good for more than renewBefore
// and isn't the one GitHub rejected.
func usable(login credential.Login, rejected Token, now time.Time) (Token, bool) {
	if login.Access == rejected.Secret || !now.Add(renewBefore).Before(login.AccessExpiry) {
		return Token{}, false
	}
	return tokenOf(login), true
}

func tokenOf(login credential.Login) Token {
	return Token{Secret: login.Access, Source: SourceKeychain, Expiry: login.AccessExpiry}
}

// SavedLogin is the login the store keeps, as the person sees it: whose,
// and until when it renews itself; ErrLoginEnded, wrapped, where it's from
// an earlier dockhand or expired, and the store's ErrNotFound where it
// keeps none. It asks GitHub nothing.
func SavedLogin(ctx context.Context, store credential.Store) (credential.Login, error) {
	value, err := store.Get(ctx, CredentialKey)
	if err != nil {
		return credential.Login{}, err
	}
	login, err := credential.DecodeLogin(value)
	if err != nil {
		return credential.Login{}, errOldLogin
	}
	if !time.Now().Before(login.RefreshExpiry) {
		return login, errExpiredLogin
	}
	return login, nil
}

// RevocationPage is where the person revokes the login an OAuth app was
// given, at GitHub.
func RevocationPage(clientID string) string {
	return "https://github.com/settings/connections/applications/" + clientID
}
