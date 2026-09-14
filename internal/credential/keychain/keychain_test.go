package keychain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/stretchr/testify/require"
)

func TestStoreRoundTripsWithoutPuttingTheSecretInArguments(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "security")
	secretFile := filepath.Join(dir, "secret")
	argumentsFile := filepath.Join(dir, "arguments")
	t.Setenv("KEYCHAIN_SECRET_FILE", secretFile)
	t.Setenv("KEYCHAIN_ARGUMENTS_FILE", argumentsFile)
	script := `#!/bin/sh
case "$1" in
find-generic-password)
  [ -f "$KEYCHAIN_SECRET_FILE" ] || exit 44
  /bin/cat "$KEYCHAIN_SECRET_FILE"
  ;;
-i)
  printf '%s\n' "$@" > "$KEYCHAIN_ARGUMENTS_FILE"
  IFS= read -r command
  printf '%s' "${command##* -w }" > "$KEYCHAIN_SECRET_FILE"
  ;;
*) exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(executable, []byte(script), 0700))
	store := keychain.Store{Executable: executable}
	key := credential.Key{Service: "fixture.service", Account: "github.com"}
	_, err := store.Get(t.Context(), key)
	require.ErrorIs(t, err, credential.ErrNotFound)
	const secret = "credential-that-must-not-be-an-argument"
	require.NoError(t, store.Put(t.Context(), key, secret))
	stored, err := store.Get(t.Context(), key)
	require.NoError(t, err)
	require.Equal(t, secret, stored)
	arguments, err := os.ReadFile(argumentsFile)
	require.NoError(t, err)
	require.NotContains(t, string(arguments), secret)
	require.NotContains(t, string(arguments), "fixture.service")
	require.Equal(t, "-i", lastLine(string(arguments)))
}

func TestStoreDistinguishesKeychainFailuresFromMissingItems(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "security")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nexit 2\n"), 0700))
	_, err := (keychain.Store{Executable: executable}).Get(t.Context(), credential.Key{Service: "fixture", Account: "github.com"})
	require.Error(t, err)
	require.NotErrorIs(t, err, credential.ErrNotFound)
}

func lastLine(value string) string {
	lines := []byte(value)
	for len(lines) > 0 && (lines[len(lines)-1] == '\n' || lines[len(lines)-1] == '\r') {
		lines = lines[:len(lines)-1]
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == '\n' {
			return string(lines[i+1:])
		}
	}
	return string(lines)
}
