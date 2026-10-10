package github_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// keptLogins is a store holding logins as the Keychain would, which can
// refuse saves.
type keptLogins struct {
	mu      sync.Mutex
	value   string
	refuse  bool
	refused int
}

func (k *keptLogins) Get(context.Context, credential.Key) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.value == "" {
		return "", credential.ErrNotFound
	}
	return k.value, nil
}

func (k *keptLogins) Put(_ context.Context, _ credential.Key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.refuse {
		k.refused++
		return errors.New("the keychain is locked")
	}
	k.value = value
	return nil
}

func (k *keptLogins) login(t *testing.T) credential.Login {
	k.mu.Lock()
	defer k.mu.Unlock()
	login, err := credential.DecodeLogin(k.value)
	require.NoError(t, err)
	return login
}

// tokenEndpoint is GitHub's token endpoint, renewing refresh token R1 to
// access token A2 and refresh token R2, or answering as answer says,
// counting what it's asked.
func tokenEndpoint(t *testing.T, answer string) (*httptest.Server, *atomic.Int64) {
	asked := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		require.Equal(t, "fixture-client", r.Form.Get("client_id"))
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case answer == "500":
			w.WriteHeader(http.StatusInternalServerError)
		case answer != "":
			fmt.Fprintf(w, `{"error":%q,"error_description":"The refresh token passed is incorrect or expired."}`, answer)
		case r.Form.Get("refresh_token") != "R1":
			fmt.Fprint(w, `{"error":"bad_refresh_token"}`)
		default:
			fmt.Fprint(w, `{"access_token":"A2","token_type":"bearer","expires_in":28800,"refresh_token":"R2","refresh_token_expires_in":15897600}`)
		}
	}))
	t.Cleanup(server.Close)
	return server, asked
}

// expiring is a login whose access token A1 is a minute from expiring, so
// it's renewed when asked for.
func expiring(t *testing.T, refreshExpiry time.Time) *keptLogins {
	value, err := credential.Login{Access: "A1", AccessExpiry: time.Now().Add(time.Minute), Refresh: "R1", RefreshExpiry: refreshExpiry, Account: "ada", ClientID: "fixture-client"}.Encode()
	require.NoError(t, err)
	return &keptLogins{value: value}
}

func renewer(server *httptest.Server, store credential.Store, lock string) github.SystemCredentials {
	flow := &github.DeviceFlow{HTTP: server.Client(), Endpoint: oauth2.Endpoint{TokenURL: server.URL + "/access_token", AuthStyle: oauth2.AuthStyleInParams}}
	return github.SystemClient(store).Credentials.(github.SystemCredentials).WithRenewal(flow, lock)
}

func noEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
}

// Two processes renewing at once renew once: the second takes the lock
// after the first, reads the login the first saved, and spends nothing,
// where spending the one-use refresh token twice would log the person out
// (the GitHub auth flow review's plan, step 3).
func TestTwoRenewalsAtOnceRenewOnce(t *testing.T) {
	noEnvironment(t)
	server, asked := tokenEndpoint(t, "")
	store := expiring(t, time.Now().AddDate(0, 6, 0))
	lock := filepath.Join(t.TempDir(), "github-login.lock")
	var wait sync.WaitGroup
	got := make([]github.Token, 2)
	errs := make([]error, 2)
	for i := range got {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got[i], errs[i] = renewer(server, store, lock).Token(t.Context())
		}()
	}
	wait.Wait()
	require.NoError(t, errors.Join(errs...))
	require.EqualValues(t, 1, asked.Load())
	for _, token := range got {
		require.Equal(t, "A2", token.Secret)
		require.Equal(t, github.SourceKeychain, token.Source)
	}
	saved := store.login(t)
	require.Equal(t, "R2", saved.Refresh)
	require.WithinDuration(t, time.Now().Add(8*time.Hour), saved.AccessExpiry, time.Minute)
}

// A login unused six months renews nothing, and asks GitHub nothing; one
// GitHub refuses to renew is over; one GitHub couldn't renew just now
// stays as it was.
func TestALoginThatCantRenewSaysWhy(t *testing.T) {
	noEnvironment(t)
	lock := filepath.Join(t.TempDir(), "github-login.lock")

	server, asked := tokenEndpoint(t, "")
	_, err := renewer(server, expiring(t, time.Now().Add(-time.Hour)), lock).Token(t.Context())
	require.EqualError(t, err, "forge: authentication is required: the GitHub login expired after six months unused; run dockhand setup github")
	require.Zero(t, asked.Load())

	server, _ = tokenEndpoint(t, "bad_refresh_token")
	_, err = renewer(server, expiring(t, time.Now().AddDate(0, 6, 0)), lock).Token(t.Context())
	require.ErrorIs(t, err, github.ErrAuthentication)
	require.EqualError(t, err, "forge: authentication is required: GitHub refused to renew the login (bad_refresh_token); run dockhand setup github")

	server, _ = tokenEndpoint(t, "500")
	store := expiring(t, time.Now().AddDate(0, 6, 0))
	before := store.value
	_, err = renewer(server, store, lock).Token(t.Context())
	require.ErrorContains(t, err, "renewing the GitHub login, which stays as it was")
	require.NotErrorIs(t, err, github.ErrAuthentication)
	require.Equal(t, before, store.value)
	for _, secret := range []string{"A1", "R1"} {
		require.NotContains(t, err.Error(), secret)
	}
}

// lockedStore is a Keychain that keeps a login but is locked.
type lockedStore struct{ keptLogins }

func (*lockedStore) Get(context.Context, credential.Key) (string, error) {
	return "", locked{}
}

type locked struct{}

func (locked) Error() string        { return "the login Keychain is locked" }
func (locked) Is(target error) bool { return target == credential.ErrLocked }

// A locked Keychain is said with the way out, and the GitHub CLI's login
// isn't tried in its place: the saved login is still the one meant (the
// rc10 full stage's F5).
func TestALockedKeychainIsSaidAndNothingElseTried(t *testing.T) {
	noEnvironment(t)
	bin := t.TempDir()
	testsupport.WriteExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\necho gho_cli\n")
	t.Setenv("PATH", bin)
	_, err := github.SystemClient(&lockedStore{}).Credentials.Token(t.Context())
	require.ErrorIs(t, err, credential.ErrLocked)
	require.EqualError(t, err, "github: the saved GitHub login can't be read: the login Keychain is locked, or GH_TOKEN set for the command stands in for it")
}

// A renewed login the store didn't take is used, said, and saved the next
// time the login is asked for: the refresh token it replaced is spent.
func TestARenewedLoginTheStoreRefusedIsUsedAndSavedLater(t *testing.T) {
	noEnvironment(t)
	server, asked := tokenEndpoint(t, "")
	store := expiring(t, time.Now().AddDate(0, 6, 0))
	store.refuse = true
	var said []string
	ctx := progress.WithReporter(t.Context(), func(update progress.Update) { said = append(said, update.Message) })
	source := renewer(server, store, filepath.Join(t.TempDir(), "github-login.lock"))
	token, err := source.Token(ctx)
	require.NoError(t, err)
	require.Equal(t, "A2", token.Secret)
	require.Len(t, said, 1)
	require.Contains(t, said[0], "Couldn't save the renewed GitHub login to the Keychain")
	require.NotContains(t, said[0], "A2")
	require.NotContains(t, said[0], "R2")

	store.mu.Lock()
	store.refuse = false
	store.mu.Unlock()
	token, err = source.Token(ctx)
	require.NoError(t, err)
	require.Equal(t, "A2", token.Secret)
	require.Equal(t, "R2", store.login(t).Refresh, "saved now")
	require.EqualValues(t, 1, asked.Load(), "renewed once")
}

// A login saved since a renewal the store didn't take, as dockhand setup
// github saves one, is the one used, and the renewal, of the login it
// replaced, never writes over it: serve, given a new login after its own
// was revoked, kept the old one (the rc8 full stage's D-C3).
func TestALoginSavedSinceAnUnsavedRenewalWins(t *testing.T) {
	noEnvironment(t)
	server, _ := tokenEndpoint(t, "")
	store := expiring(t, time.Now().AddDate(0, 6, 0))
	store.refuse = true
	source := renewer(server, store, filepath.Join(t.TempDir(), "github-login.lock"))
	token, err := source.Token(t.Context())
	require.NoError(t, err)
	require.Equal(t, "A2", token.Secret, "renewed, and held, since the store refused it")

	fresh, err := credential.Login{Access: "A9", AccessExpiry: time.Now().Add(time.Hour), Refresh: "R9", RefreshExpiry: time.Now().AddDate(0, 6, 0), Account: "ada", ClientID: "fixture-client"}.Encode()
	require.NoError(t, err)
	store.mu.Lock()
	store.value, store.refuse = fresh, false // as another process's setup github saves it
	store.mu.Unlock()
	token, err = source.Token(t.Context())
	require.NoError(t, err)
	require.Equal(t, "A9", token.Secret, "the login saved since")
	require.Equal(t, "R9", store.login(t).Refresh, "the held renewal didn't write over it")
	token, err = source.Token(t.Context())
	require.NoError(t, err)
	require.Equal(t, "A9", token.Secret)
}

// A token GitHub rejects is renewed though it hadn't expired, and a
// client's request tried again with the new one.
func TestARejectedLoginIsRenewed(t *testing.T) {
	noEnvironment(t)
	tokens, asked := tokenEndpoint(t, "")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer A2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"login":"ada"}`)
	}))
	t.Cleanup(api.Close)
	value, err := credential.Login{Access: "A1", AccessExpiry: time.Now().Add(8 * time.Hour), Refresh: "R1", RefreshExpiry: time.Now().AddDate(0, 6, 0), Account: "ada", ClientID: "fixture-client"}.Encode()
	require.NoError(t, err)
	store := &keptLogins{value: value}
	client := &github.Client{HTTP: api.Client(), Config: github.Config{BaseURL: api.URL + "/"}, Credentials: renewer(tokens, store, filepath.Join(t.TempDir(), "github-login.lock"))}
	login, err := client.AuthenticatedUser(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ada", login)
	require.EqualValues(t, 1, asked.Load())
}
