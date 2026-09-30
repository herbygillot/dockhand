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
//
// Since batch 20 the program also reads the commit a Git fetch checked
// out, and stops a target whose fetch checked out another than the check
// expected. That judges only Git-fetched ports otherwise, and what the
// program recorded of them before stands for no later check anyway: it
// recorded no commit, which reuse and evidence now require of them
// (reuse.Current, engine.Counts). Every other port is built and judged as
// before, so its evidence stands, and the protocol stays 2.
const guestPin = "3942a23f0bdcaaa0ec0a9a3da3c1708fa15bc3cffa33d3f152e77c765ed287bb"

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

// Where only the guest protocol changed, the image is the one it was, and
// what's other is how dockhand builds in it, which Tart says; a changed
// image, or an identity it doesn't know, is left for the engine to say
// (the s2n-tls run's note 1).
func TestAChangedIdentitySaysWhatChanged(t *testing.T) {
	p := &Provider{}
	image := "source sha256:eeec; setup 3; macports 2.12.6; tools 26.6"
	require.Equal(t, "dockhand has begun to build each target from its source, never from a published archive, and from clean work",
		p.IdentityChange(model.Environment{}, image+"; verifier 1", image+"; verifier 2"))
	require.Equal(t, "dockhand has begun to build otherwise", p.IdentityChange(model.Environment{}, image+"; verifier 2", image+"; verifier 9"))
	require.Empty(t, p.IdentityChange(model.Environment{}, image+"; verifier 1", "source sha256:ffff; setup 3; macports 2.12.6; tools 26.6; verifier 2"), "another image")
	require.Empty(t, p.IdentityChange(model.Environment{}, image+"; verifier 2", image+"; verifier 2"))
	require.Empty(t, p.IdentityChange(model.Environment{}, "", image+"; verifier 2"))
}
