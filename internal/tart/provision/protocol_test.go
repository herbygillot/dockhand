package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tart"
)

// setupPin is the digest of the provisioning code tart.SetupProtocol 3
// covers: this package's, and the MacPorts installation it runs. Batch 26
// bounded setup's wait on a blocked listing, and batch 36 made its SSH
// wait guestssh.AwaitSSH, and batch 39 renamed the packages it imports
// (channel to guestssh, text to textedit), and batch 48 regrouped their
// imports, and v0.3.1 took a guest MacPorts newer than asked, which
// MacPorts' installer's selfupdate makes, and the architecture review of
// 5897a82's R3-18 refused an old Tart with tart.TooOld's words, and the
// test analysis ran Validate through a command function, to test it off
// a Mac, none of which changes an image.
const setupPin = "53c65015d18fe56dd46d592ef278011c42c7df1186971576444630f2af3061fb"

// What setup puts in an image is identified by tart.SetupProtocol, which
// evidence's reuse compares (decision 28). A change to the provisioning
// code fails this test until its author decides: raise the protocol, when
// the change alters what an image holds, which ends reuse of evidence from
// images made before; or, for a change of wording only, update the pin.
func TestTheSetupProtocolCoversTheProvisioningCode(t *testing.T) {
	t.Parallel()
	var names []string
	for _, directory := range []string{".", "../../macports/installation"} {
		entries, err := os.ReadDir(directory)
		require.NoError(t, err)
		for _, entry := range entries {
			if name := entry.Name(); strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				names = append(names, filepath.Join(directory, name))
			}
		}
	}
	slices.Sort(names)
	digest := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		digest.Write([]byte(name + "\x00"))
		digest.Write(data)
	}
	pin := hex.EncodeToString(digest.Sum(nil))
	require.Equal(t, 3, tart.SetupProtocol)
	require.Equal(t, setupPin, pin, "the provisioning code changed: raise tart.SetupProtocol if images made now hold something else, or update setupPin if not")
}
