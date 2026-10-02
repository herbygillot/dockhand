package github

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Finding no credentials is remembered for a few minutes, so anonymous
// reads don't each ask the keychain and gh again; after that, and for an
// operation that needs a login, it's looked for again, so a login made
// meanwhile is used (the code-organization review's finding 45).
func TestNoCredentialsIsRememberedAWhile(t *testing.T) {
	asked := 0
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	client := &Client{Config: Config{BaseURL: "https://api.github.invalid/"}, now: func() time.Time { return at },
		Credentials: TokenSourceFunc(func(context.Context) (Token, error) {
			asked++
			return Token{}, ErrNoCredentials
		})}
	for range 3 {
		_, err := client.API(t.Context())
		require.NoError(t, err, "anonymous")
	}
	require.Equal(t, 1, asked, "asked once, and remembered")
	_, err := client.AuthenticatedAPI(t.Context())
	require.ErrorIs(t, err, ErrNoCredentials)
	require.Equal(t, 2, asked, "an operation that needs a login always looks")
	at = at.Add(anonymousFor)
	_, err = client.API(t.Context())
	require.NoError(t, err)
	require.Equal(t, 3, asked, "and after a while, an anonymous read looks again")
}
