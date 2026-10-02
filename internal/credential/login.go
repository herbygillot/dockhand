package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Login is a saved login that renews itself: an access token good until
// AccessExpiry, and a refresh token, good until RefreshExpiry, that gets
// another, both replaced each time. Its secrets are kept out of anything
// printed: String and GoString say only whose login it is and until when.
type Login struct {
	Access        string
	AccessExpiry  time.Time
	Refresh       string
	RefreshExpiry time.Time
	Account       string
	// ClientID is the OAuth application the login was made with, which
	// renews it.
	ClientID string
}

// ErrOldLogin is a saved value that isn't a login this dockhand keeps,
// as the bare token earlier dockhands saved.
var ErrOldLogin = errors.New("credential: the saved value isn't a login this dockhand keeps")

func (l Login) String() string {
	return fmt.Sprintf("login for %s, renewable until %s", l.Account, l.RefreshExpiry.Format(time.DateOnly))
}

func (l Login) GoString() string { return l.String() }

// Complete reports a login with each part a renewing login needs.
func (l Login) Complete() bool {
	return l.Access != "" && l.Refresh != "" && !l.AccessExpiry.IsZero() && !l.RefreshExpiry.IsZero() && l.Account != "" && l.ClientID != ""
}

// savedLogin is a Login as the store keeps it, written by Encode alone, so
// no other encoding of a Login carries its secrets.
type savedLogin struct {
	Version       int       `json:"version"`
	Access        string    `json:"access_token"`
	AccessExpiry  time.Time `json:"access_expires"`
	Refresh       string    `json:"refresh_token"`
	RefreshExpiry time.Time `json:"refresh_expires"`
	Account       string    `json:"account"`
	ClientID      string    `json:"client_id"`
}

// loginVersion is the saved form's version.
const loginVersion = 1

// Encode is the login as the store keeps it.
func (l Login) Encode() (string, error) {
	if !l.Complete() {
		return "", errors.New("credential: an incomplete login isn't kept")
	}
	data, err := json.Marshal(savedLogin{Version: loginVersion, Access: l.Access, AccessExpiry: l.AccessExpiry.UTC(), Refresh: l.Refresh,
		RefreshExpiry: l.RefreshExpiry.UTC(), Account: l.Account, ClientID: l.ClientID})
	return string(data), err
}

// DecodeLogin reads a login as Encode kept it; ErrOldLogin for anything
// else, an earlier dockhand's bare token among them.
func DecodeLogin(value string) (Login, error) {
	var saved savedLogin
	if err := json.Unmarshal([]byte(value), &saved); err != nil || saved.Version != loginVersion {
		return Login{}, ErrOldLogin
	}
	login := Login{Access: saved.Access, AccessExpiry: saved.AccessExpiry, Refresh: saved.Refresh, RefreshExpiry: saved.RefreshExpiry, Account: saved.Account, ClientID: saved.ClientID}
	if !login.Complete() {
		return Login{}, ErrOldLogin
	}
	return login, nil
}
