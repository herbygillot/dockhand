package macos

import (
	"context"
	"strings"
)

// ForeignPackageManagers reports known competing package-manager paths without changing them.
func ForeignPackageManagers(ctx context.Context, command Command) ([]string, error) {
	script := `for path in /opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg /etc/paths.d/homebrew /etc/paths.d/fink; do [ ! -e "$path" ] || printf '%s\n' "$path"; done`
	out, err := command(ctx, nil, "/bin/sh", "-c", script)
	if err != nil {
		return nil, err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return nil, nil
	}
	return strings.Split(value, "\n"), nil
}
