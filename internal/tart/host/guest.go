package host

import (
	"context"
	"io"
	"strings"
	"time"
)

type guestCommand func(context.Context, io.Reader, ...string) ([]byte, error)

// ObserveGuestAgentVersion retains diagnostic output when available. An absent
// --version command or an unfamiliar spelling does not establish incompatibility.
func ObserveGuestAgentVersion(ctx context.Context, run guestCommand) (string, error) {
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
