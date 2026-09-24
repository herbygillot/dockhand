package channel_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/channel"
	"github.com/herbygillot/dockhand/internal/tart/host"
	"github.com/stretchr/testify/require"
)

// The channel against a real guest, on a clone of DOCKHAND_TEST_TART_IMAGE:
// the password bootstrap records the image's host keys and installs
// dockhand's key; after it, commands and transfers use the key, 64 MiB go
// each way intact, and a file is read by ranges. Worth running on macOS
// 12–15 guests, whose `tart exec` loses output (openai/tart#1347).
func TestLiveChannel(t *testing.T) {
	image := os.Getenv("DOCKHAND_TEST_TART_IMAGE")
	if image == "" {
		t.Skip("set DOCKHAND_TEST_TART_IMAGE for the opt-in channel test")
	}
	runtime, err := (tart.Client{}).Resolve()
	require.NoError(t, err)
	machine := host.Machine{Client: runtime}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	suffix := make([]byte, 4)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "dockhand-test-" + hex.EncodeToString(suffix)
	require.NoError(t, machine.Clone(ctx, image, name))
	var run *host.Foreground
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if run != nil {
			_ = run.Stop(cleanup, 30*time.Second)
		}
		if err := machine.Delete(cleanup, name); err != nil {
			t.Errorf("scratch VM %s was left behind: %v", name, err)
		}
	})
	run, err = machine.StartForeground(name)
	require.NoError(t, err)
	address, err := machine.IP(ctx, name, 180)
	require.NoError(t, err)

	keys := channel.Keys{Directory: filepath.Join(t.TempDir(), "ssh")}
	bootstrap := &channel.Guest{Address: address, Image: name, Keys: keys, Bootstrap: true}
	var output []byte
	for attempt := 0; ; attempt++ {
		output, err = bootstrap.Command(ctx, nil, "/usr/bin/true")
		if err == nil || attempt == 60 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	require.NoError(t, err, "%s", output)
	require.NoError(t, bootstrap.InstallKey(ctx))
	recorded, err := os.ReadFile(keys.HostKeys(name))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(recorded), name+" "), "the host keys are recorded under the image: %s", recorded)

	guest := &channel.Guest{Address: address, Image: name, Keys: keys}
	defer guest.Close(context.Background())
	version, err := guest.Command(ctx, nil, "/usr/bin/sw_vers", "-productVersion")
	require.NoError(t, err)
	t.Logf("guest macOS %s at %s", strings.TrimSpace(string(version)), address)

	payload := make([]byte, 64<<20)
	_, err = rand.Read(payload)
	require.NoError(t, err)
	local := filepath.Join(t.TempDir(), "payload")
	require.NoError(t, os.WriteFile(local, payload, 0600))
	start := time.Now()
	require.NoError(t, guest.Upload(ctx, local, "/var/tmp/dockhand-channel-test", true))
	t.Logf("64 MiB up in %s", time.Since(start))
	back := filepath.Join(t.TempDir(), "back")
	start = time.Now()
	require.NoError(t, guest.Download(ctx, "/var/tmp/dockhand-channel-test", back, true))
	t.Logf("64 MiB down in %s", time.Since(start))
	received, err := os.ReadFile(back)
	require.NoError(t, err)
	require.True(t, bytes.Equal(payload, received), "64 MiB came back intact")
	for _, offset := range []int64{0, 1 << 20, int64(len(payload)) - 100} {
		chunk, err := guest.Range(ctx, "/var/tmp/dockhand-channel-test", offset, 1<<20, true)
		require.NoError(t, err)
		end := min(offset+1<<20, int64(len(payload)))
		require.True(t, bytes.Equal(payload[offset:end], chunk), "range at %d", offset)
	}

	stranger := &channel.Guest{Address: address, Image: "another-image", Keys: keys}
	_, err = stranger.Command(ctx, nil, "/usr/bin/true")
	require.ErrorIs(t, err, channel.ErrTransport, "a guest is held to the host keys of the image it is named for")
}
