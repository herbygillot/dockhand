package macos

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPFSSpaceChecksCapacityAndPreservesResizeErrors(t *testing.T) {
	for _, tc := range []struct {
		name, initial, after        string
		fail, wantError, wantResize bool
	}{
		{"already expanded", "80", "80", true, false, false},
		{"expands small container", "20", "80", false, false, true},
		{"failed resize stays too small", "20", "20", true, true, true},
		{"capacity sufficient despite resize error", "20", "80", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "space")
			calls := filepath.Join(root, "calls")
			df := filepath.Join(root, "df")
			disk := filepath.Join(root, "diskutil")
			require.NoError(t, os.WriteFile(state, []byte(tc.initial), 0600))
			require.NoError(t, os.WriteFile(df, []byte("#!/bin/sh\nprintf 'Filesystem Size Used Available\\nfixture 100 20 %s\\n' \"$(cat \"$TEST_SPACE\")\"\n"), 0700))
			require.NoError(t, os.WriteFile(disk, []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$TEST_CALLS"
if [ "$1" = apfs ]; then
 printf '%s' "$TEST_AFTER" > "$TEST_SPACE"
 if [ "$TEST_FAIL" = yes ]; then echo 'fixture: resize unavailable' >&2; exit 1; fi
fi
`), 0700))
			var output []byte
			err := EnsureAPFSSpace(t.Context(), func(ctx context.Context, _ io.Reader, args ...string) ([]byte, error) {
				script := strings.ReplaceAll(args[2], "/bin/df", `"$TEST_DF"`)
				script = strings.ReplaceAll(script, "/usr/sbin/diskutil", `"$TEST_DISK"`)
				script = strings.ReplaceAll(script, "sudo -n ", "")
				commandArgs := append([]string{"-c", script}, args[3:]...)
				command := exec.CommandContext(ctx, "/bin/sh", commandArgs...)
				fail := "no"
				if tc.fail {
					fail = "yes"
				}
				command.Env = append(os.Environ(), "TEST_SPACE="+state, "TEST_CALLS="+calls, "TEST_DF="+df, "TEST_DISK="+disk, "TEST_AFTER="+tc.after, "TEST_FAIL="+fail)
				var err error
				output, err = command.CombinedOutput()
				return output, err
			}, "/volume with spaces", "disk7", "disk7s2", 70)
			if tc.wantError {
				require.Error(t, err)
				require.ErrorContains(t, err, "only 20 GB free; need at least 70 GB")
				require.ErrorContains(t, err, "fixture: resize unavailable")
			} else {
				require.NoError(t, err, string(output))
			}
			if tc.wantResize {
				data, err := os.ReadFile(calls)
				require.NoError(t, err)
				require.Contains(t, string(data), "repairDisk disk7")
				require.Contains(t, string(data), "apfs resizeContainer disk7s2 0")
				if tc.fail {
					require.Contains(t, string(output), "fixture: resize unavailable")
				}
			} else {
				require.NoFileExists(t, calls)
			}
		})
	}
}
