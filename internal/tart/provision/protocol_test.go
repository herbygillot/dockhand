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

// setupPin is the digest of the provisioning code tart.SetupProtocol 2
// covers: this package's, and the MacPorts installation it runs.
const setupPin = "ae7c74001b7a699af6bea0d1888fb974dd4fa38e75ce14e9e6ae458ac3a4948b"

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
	require.Equal(t, 2, tart.SetupProtocol)
	require.Equal(t, setupPin, pin, "the provisioning code changed: raise tart.SetupProtocol if images made now hold something else, or update setupPin if not")
}
