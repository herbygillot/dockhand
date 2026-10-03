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
// imports, none of which changes an image.
const setupPin = "f59674bdf43b096555905c1d962dac63dc59856c4c16a0a15e1eb063bf0906e6"

// What setup puts in an image is identified by tart.SetupProtocol, which
// evidence's reuse compares (decision 28). A change to the provisioning
// code fails this test until its author decides: raise the protocol, when
// the change alters what an image holds, which ends reuse of evidence from
// images made before; or, for a change of wording only, update the pin.
func TestTheSetupProtocolCoversTheProvisioningCode(t *testing.T) {
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
