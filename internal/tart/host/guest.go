package host

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type GuestCommand func(context.Context, io.Reader, ...string) ([]byte, error)

// CheckGuestTransport checks the command features used by staging and verification.
// It neither writes guest files nor requires a particular agent build or location.
func CheckGuestTransport(ctx context.Context, run GuestCommand) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token := rand.Text() + "\n"
	output, err := run(ctx, strings.NewReader(token), "/bin/cat")
	if err != nil {
		return fmt.Errorf("guest transport: stdin/stdout probe: %w", err)
	}
	if string(output) != token {
		return fmt.Errorf("guest transport: stdin/stdout probe returned different contents")
	}
	output, err = run(ctx, nil, "/bin/sh", "-c", `printf '%s\n' "$1"; exit 1`, "dockhand-probe", strings.TrimSpace(token))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exit *exec.ExitError
	if string(output) != token || !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return fmt.Errorf("guest transport: command failure was not propagated correctly: %w", errors.Join(err, errors.New("expected probe output and exit status 1")))
	}
	return nil
}

// ObserveGuestAgentVersion retains diagnostic output when available. An absent
// --version command or an unfamiliar spelling does not establish incompatibility.
func ObserveGuestAgentVersion(ctx context.Context, run GuestCommand) (string, error) {
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := run(call, nil, "/opt/dockhand/bin/tart-guest-agent", "--version")
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(output)), "tart-guest-agent version ")), nil
}
