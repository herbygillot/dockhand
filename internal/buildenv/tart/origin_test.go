package tart

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
)

// An environment's identity is its image's origin, as setup recorded it on
// the host, with the guest program's protocol; an image setup recorded no
// origin of has none.
func TestAnEnvironmentsIdentityIsItsImagesOrigin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.MkdirAll(home, 0o700))
	provider := &Provider{Tart: tartvm.Client{Home: home}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	identity, err := provider.Identity(t.Context(), tahoe)
	require.NoError(t, err)
	require.Empty(t, identity, "no record")

	manifest := tartvm.ImageManifest{Protocol: tartvm.ImageManifestProtocol, Source: "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest",
		SourceDigest: "sha256:eeec54bfe1f076e27786c5d92b89187a05b1d109b5071eb2dcdf02d596e34640", MacPortsVersion: "2.12.6", CommandLineTools: "26.6", SetupProtocol: tartvm.SetupProtocol}
	require.NoError(t, tartvm.WriteImageRecord(home, "dockhand-base-tahoe", manifest))
	identity, err = provider.Identity(t.Context(), tahoe)
	require.NoError(t, err)
	require.Equal(t, manifest.Origin()+"; verifier 2", identity)

	xcode := tahoe
	xcode.DeveloperTools = model.DeveloperToolsXcode
	identity, err = provider.Identity(t.Context(), xcode)
	require.NoError(t, err)
	require.Empty(t, identity, "the Xcode image is another image, and has no record")
}

// guestPin is the digest of the guest program VerifierProtocol 2 covers.
// Protocol 2 builds each target from its source, never from a published
// archive, and cleans its earlier work first (the s2n-tls run's findings 1
// and 2): what protocol 1 recorded may be an archive install taken for a
// build, so none of it stands any more.
const guestPin = "8ec69645a5d087fc9e9b735ef59e055088696308e32b7978fe78e5c8b3d98db3"

// How the guest program builds is identified by VerifierProtocol, part of
// an environment's origin (decision 28). A change to guest.tcl fails this
// test until its author decides: raise the protocol, when the change
// alters how ports are built or judged, which ends reuse of evidence the
// program recorded before; or, for a change of wording only, update the
// pin.
func TestTheVerifierProtocolCoversTheGuestProgram(t *testing.T) {
	data, err := os.ReadFile("guest.tcl")
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	require.Equal(t, 2, VerifierProtocol)
	require.Equal(t, guestPin, hex.EncodeToString(sum[:]), "guest.tcl changed: raise VerifierProtocol if ports are built or judged otherwise, or update guestPin if not")
}
