package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tart"
)

// setupPin is the digest of the provisioning code tart.SetupProtocol 1
// covers.
const setupPin = "4316e74cad4616762207e408827cc0b9adbe0434e2b3ed3a03b4e1d6d413894f"

// What setup puts in an image is identified by tart.SetupProtocol, which
// evidence's reuse compares (decision 28). A change to the provisioning
// code fails this test until its author decides: raise the protocol, when
// the change alters what an image holds, which ends reuse of evidence from
// images made before; or, for a change of wording only, update the pin.
func TestTheSetupProtocolCoversTheProvisioningCode(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if name := entry.Name(); strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			names = append(names, name)
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
	require.Equal(t, 1, tart.SetupProtocol)
	require.Equal(t, setupPin, pin, "the provisioning code changed: raise tart.SetupProtocol if images made now hold something else, or update setupPin if not")
}
