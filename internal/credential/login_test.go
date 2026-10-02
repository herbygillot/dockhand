package credential

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestALoginIsKeptAndReadAndPrintsNoSecret(t *testing.T) {
	when := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
	login := Login{Access: "ghu_access", AccessExpiry: when.Add(8 * time.Hour), Refresh: "ghr_refresh", RefreshExpiry: when.AddDate(0, 6, 0), Account: "ada", ClientID: "Ov23"}
	saved, err := login.Encode()
	require.NoError(t, err)
	read, err := DecodeLogin(saved)
	require.NoError(t, err)
	require.Equal(t, login, read)
	for _, printed := range []string{login.String(), fmt.Sprintf("%v %+v %#v %s", login, login, login, login)} {
		require.NotContains(t, printed, "ghu_access")
		require.NotContains(t, printed, "ghr_refresh")
	}
	_, err = DecodeLogin("gho_bare_token_from_an_earlier_dockhand")
	require.ErrorIs(t, err, ErrOldLogin)
	_, err = (Login{Access: "a", Account: "ada"}).Encode()
	require.Error(t, err, "an incomplete login isn't kept")
}
