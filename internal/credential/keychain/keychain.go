// Package keychain stores credentials in the current user's macOS Keychain.
package keychain

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/credential"
)

const encodedPrefix = "dockhand-base64:"

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
	output, err := exec.CommandContext(ctx, path, "find-generic-password", "-a", key.Account, "-s", key.Service, "-w").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return "", credential.ErrNotFound
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
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
	command := exec.CommandContext(ctx, path, "-i")
	command.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -a %s -s %s -w %s\n", key.Account, key.Service, encoded))
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
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
	err = exec.CommandContext(ctx, path, "delete-generic-password", "-a", key.Account, "-s", key.Service).Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 {
		return credential.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("keychain: removing credential: %w", err)
	}
	return nil
}
