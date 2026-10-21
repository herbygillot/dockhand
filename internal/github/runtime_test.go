package github

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The runtime's client holds one login for everything that shares it, and
// an account change lets go of it in one place: the next use reads the
// login as it is then, as serve's did by dropping its forge alone (CX-11).
func TestAnAccountChangeIsForgottenInOnePlace(t *testing.T) {
	asked, account := 0, "ada"
	runtime := &Runtime{Client: &Client{Config: Config{BaseURL: "https://api.github.invalid/"},
		Credentials: TokenSourceFunc(func(context.Context) (Token, error) {
			asked++
			return Token{Secret: account + "-token", Source: SourceExplicit}, nil
		})}}
	first, err := runtime.Client.AuthenticatedAPI(t.Context())
	require.NoError(t, err)
	again, err := runtime.Client.AuthenticatedAPI(t.Context())
	require.NoError(t, err)
	require.Same(t, first, again, "one authenticated client, shared")
	require.Equal(t, 1, asked)

	account = "grace"
	runtime.AccountChanged()
	after, err := runtime.Client.AuthenticatedAPI(t.Context())
	require.NoError(t, err)
	require.NotSame(t, first, after, "made again for the login as it is now")
	require.Equal(t, 2, asked)
	(*Runtime)(nil).AccountChanged()
}
