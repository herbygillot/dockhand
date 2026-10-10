package keychain

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential"
	"github.com/herbygillot/dockhand/internal/subprocess"
)

const encodedPrefix = "dockhand-base64:"

// security exits with the low byte of the Security framework's status:
// 44 for errSecItemNotFound (-25300), and 36 for
// errSecInteractionNotAllowed (-25308), which a locked Keychain gives
// where it can't ask to be unlocked, as over SSH with nobody at the
// screen (the rc10 full stage's F5, which read a raw "exit status 36").
const interactionNotAllowed = 36

// errLocked is the locked Keychain, said with how to unlock it.
var errLocked error = lockedError{}

type lockedError struct{}

func (lockedError) Error() string {
	return "the login Keychain is locked, as it is over SSH with nobody at the screen; security unlock-keychain unlocks it"
}

func (lockedError) Is(target error) bool { return target == credential.ErrLocked }

func isLocked(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == interactionNotAllowed
}

type Store struct {
	Executable string
}

func (s Store) Get(ctx context.Context, key credential.Key) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	path, err := s.executable()
	if err != nil {
		return "", err
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "security", Path: path, Args: []string{"find-generic-password", "-a", key.Account, "-s", key.Service, "-w"}, Limit: 1 << 16})
	output := result.Output
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return "", credential.ErrNotFound
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if isLocked(err) {
			return "", errLocked
		}
		return "", fmt.Errorf("keychain: reading credential: %w", err)
	}
	secret := strings.TrimRight(string(output), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("keychain: stored credential is empty")
	}
	if strings.HasPrefix(secret, encodedPrefix) {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, encodedPrefix))
		if err != nil || len(decoded) == 0 {
			return "", fmt.Errorf("keychain: stored credential is malformed")
		}
		return string(decoded), nil
	}
	return secret, nil
}

func (s Store) Put(ctx context.Context, key credential.Key, secret string) error {
	if err := validKey(key); err != nil {
		return err
	}
	if secret == "" || strings.ContainsAny(secret, "\r\n") {
		return fmt.Errorf("keychain: credential is empty or malformed")
	}
	path, err := s.executable()
	if err != nil {
		return err
	}
	encoded := encodedPrefix + base64.StdEncoding.EncodeToString([]byte(secret))
	// What it's given on standard input is the secret, which an error
	// never repeats: only how it ended is said, never what it wrote.
	_, err = subprocess.Run(ctx, subprocess.Spec{Tool: "security", Path: path, Args: []string{"-i"}, Limit: 1 << 16,
		Stdin: strings.NewReader(fmt.Sprintf("add-generic-password -U -a %s -s %s -w %s\n", key.Account, key.Service, encoded))})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var failed *subprocess.Error
		if errors.As(err, &failed) {
			err = failed.Cause
		}
		return fmt.Errorf("keychain: storing credential: %w", err)
	}
	return nil
}

func (s Store) executable() (string, error) {
	if s.Executable != "" {
		return s.Executable, nil
	}
	path, err := exec.LookPath("security")
	if err != nil {
		return "", fmt.Errorf("keychain: macOS security tool is unavailable: %w", err)
	}
	return path, nil
}

func validKey(key credential.Key) error {
	if key.Service == "" || key.Account == "" || !safeName(key.Service) || !safeName(key.Account) {
		return fmt.Errorf("keychain: service and account are required")
	}
	return nil
}

func safeName(value string) bool {
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/@:-", r) {
			continue
		}
		return false
	}
	return true
}

func (s Store) Delete(ctx context.Context, key credential.Key) error {
	if err := validKey(key); err != nil {
		return err
	}
	path, err := s.executable()
	if err != nil {
		return err
	}
	_, err = subprocess.Run(ctx, subprocess.Spec{Tool: "security", Path: path, Args: []string{"delete-generic-password", "-a", key.Account, "-s", key.Service}, Limit: 1 << 16})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 {
		return credential.ErrNotFound
	}
	if isLocked(err) {
		return errLocked
	}
	if err != nil {
		return fmt.Errorf("keychain: removing credential: %w", err)
	}
	return nil
}
