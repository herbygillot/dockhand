package installation

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
)

func Install(ctx context.Context, command macos.Command, version string, release macos.Release) error {
	filename := fmt.Sprintf("MacPorts-%s-%s-%s.pkg", version, release.Product, strings.ReplaceAll(release.Name, " ", ""))
	address := "https://distfiles.macports.org/MacPorts/" + filename
	script := `set -eu
file="/tmp/$1"
/usr/bin/curl -fsSL -o "$file" "$2"
sudo -n /usr/sbin/installer -pkg "$file" -target /
/bin/rm -f "$file"`
	_, err := command(ctx, nil, "/bin/sh", "-c", script, "dockhand", filename, address)
	return err
}
