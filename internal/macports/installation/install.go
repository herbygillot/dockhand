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

// KeepArchives sets portimage_mode to directory_and_archive in the
// installation's macports.conf, the documented setting under which
// MacPorts keeps each port's archive beside the directory it activates
// from. Its default on APFS, directory, deletes an archive once it is
// extracted, which leaves nothing to identify a port by or to install
// again. The setting is added once, after the file's own, which it
// overrides.
func KeepArchives(ctx context.Context, command macos.Command, prefix string) error {
	script := `set -eu
conf="$1/etc/macports/macports.conf"
setting='portimage_mode directory_and_archive'
if ! /usr/bin/grep -qx "$setting" "$conf"; then
  printf '\n# dockhand keeps each port'\''s archive, to identify it and install it again.\n%s\n' "$setting" | sudo -n /usr/bin/tee -a "$conf" >/dev/null
fi`
	_, err := command(ctx, nil, "/bin/sh", "-c", script, "dockhand", prefix)
	return err
}
