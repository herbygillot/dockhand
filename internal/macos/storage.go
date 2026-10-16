package macos

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// EnsureAPFSSpace expands the supplied APFS container only when path has insufficient
// free space. The caller chooses the disk and container and authorizes their repair.
func EnsureAPFSSpace(ctx context.Context, run Command, path, disk, container string, minimumGiB int) error {
	if path == "" || disk == "" || container == "" || minimumGiB <= 0 {
		return fmt.Errorf("macos: APFS expansion requires a path, disk, container, and positive space requirement")
	}
	script := `set -eu
path=$1
disk=$2
container=$3
minimum=$4
available_space() { /bin/df -g "$path" | /usr/bin/awk 'NR==2 {print $4}'; }
available=$(available_space)
[ "$available" -lt "$minimum" ] || exit 0
sudo -n /bin/sh -c 'yes | /usr/sbin/diskutil repairDisk "$1"' dockhand "$disk"
# A resize can fail once, -69606, and pass when tried again (field testing,
# sequoia, 2026-10-16): it's tried three times, five seconds apart.
for attempt in 1 2 3; do
  if sudo -n /usr/sbin/diskutil apfs resizeContainer "$container" 0; then
    break
  fi
  echo "APFS resize failed (attempt $attempt of 3)" >&2
  [ "$attempt" -eq 3 ] || /bin/sleep 5
done
available=$(available_space)
[ "$available" -ge "$minimum" ] || { echo "only ${available} GB free; need at least ${minimum} GB"; exit 1; }`
	if output, err := run(ctx, nil, "/bin/sh", "-c", script, "dockhand", path, disk, container, strconv.Itoa(minimumGiB)); err != nil {
		return fmt.Errorf("macos: expanding APFS storage: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
