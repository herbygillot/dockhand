package tart

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	guestprogram "github.com/herbygillot/dockhand/internal/buildenv/guest"
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
	provider := &Provider{Tart: tartvm.Client{Env: os.Environ(), Home: home}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsCommandLine}
	identity, err := provider.Identity(t.Context(), tahoe)
	require.NoError(t, err)
	require.Empty(t, identity, "no record")

	manifest := tartvm.ImageManifest{Protocol: tartvm.ImageManifestProtocol, Source: "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest",
		SourceDigest: "sha256:eeec54bfe1f076e27786c5d92b89187a05b1d109b5071eb2dcdf02d596e34640", MacPortsVersion: "2.12.6", CommandLineTools: "26.6", SetupProtocol: tartvm.SetupProtocol}
	require.NoError(t, tartvm.WriteImageRecord(home, "dockhand-base-tahoe", manifest))
	identity, err = provider.Identity(t.Context(), tahoe)
	require.NoError(t, err)
	require.Equal(t, manifest.Origin()+"; verifier 3", identity)

	xcode := tahoe
	xcode.DeveloperTools = model.DeveloperToolsXcode
	identity, err = provider.Identity(t.Context(), xcode)
	require.NoError(t, err)
	require.Empty(t, identity, "the Xcode image is another image, and has no record")
}

// guestPin is the digest of the guest program VerifierProtocol 3 covers.
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
//
// The expected commit is read as a string since: an abbreviation of
// octal digits led by a zero, 0157, had been taken for the number 111 and
// its fetch failed as moved. Only such a Git-fetched port is judged
// otherwise, and what failed so stood for nothing, so the protocol stays 2.
//
// Since batch 14 the program also records where each step of a target's
// build begins in its log, for logs --port to find the port's own build
// after its dependencies' (the hugo exercise). It runs the same steps in
// the same order and judges them as before, so the protocol stays 2, and
// the results it recorded earlier stand, without their steps.
//
// Since batch 24 a failure's reason is read from where the failing step
// began rather than from the whole log, so an earlier step's Error line,
// as lint's the rust run quoted for its tests' failure (#35084), isn't
// taken for it. That changes a result's detail, its words, and nothing
// it is judged by, so the protocol stays 2.
//
// Since batch 26 lint ends at 10 minutes and a build at 6 hours (D16),
// with what each started. A build that passed before passed within them,
// as any that took less does now, so the results stand, and the protocol
// stays 2.
//
// Since batch 43 a failure's detail names the command MacPorts says
// failed and what the tool said last before it: its words, again, and the
// protocol stays 2.
//
// Since batch 90 each active port's entry also says where its archive is,
// for the host to keep a dependency's for a later guest. That reports
// more, and builds and judges nothing otherwise: a dependency installed
// from a kept archive is recorded by its digest, as one from MacPorts'
// own archives always was, so the protocol stays 2.
//
// Protocol 3: what a dependency built from source needed to build is
// deactivated before the target builds, which judges a target otherwise,
// since one that passed with another's build tools may not pass without
// them, so a result recorded before isn't reused (field testing,
// 2026-10-07: taisei passed with git's gettext).
//
// Since then the program also writes which target it is building as it
// begins it, for the host to record the one a lost guest was building as
// interrupted. That reports more, and builds and judges nothing
// otherwise, so the protocol stays 3.
//
// Since item 5 of v0.5.0's part 1 the program installs a target's
// dependencies without -d where none builds from source, and appends the
// tests' own logs to a target's log after a failed test phase (the exiv2
// field notes, 2026-10-19). The log says less of archives' installs and
// more of a test's failure; nothing is built or judged otherwise, so the
// protocol stays 3.
//
// Since the exiv2 session's notes (2026-10-21) each target's log is named
// by its target, as the host names it, and a failure installing its
// dependencies is at a phase of its own, dependencies, with MacPorts' line
// naming the dependency for its detail, where it read as install's. A
// failed result is no evidence a later check reuses, and nothing is built
// otherwise, so the protocol stays 3.
//
// Since v0.5.0's part 6, a passed target's result says what its
// installed Mach-O files link under the prefix, read after its build with
// port contents, otool, and port provides: an observation beside the
// result, which builds and judges nothing otherwise, so the protocol stays
// 3.
//
// Since v0.6.0's local provider, the program also runs on this Mac in a
// prefix of the person's, where input's local says so: there it builds
// as-is, snapshots the prefix, and puts it back. In a clone, where local
// is absent, it builds and judges as it did, so the protocol stays 3.
//
// Since field testing's outdated-ports notes, a failure's detail passes
// over make's and ninja's own failure lines and a compiler's count for the
// tool's error: its words alone, so the protocol stays 3.
//
// Since Then's item 9, a passed target's result keeps what port contents
// listed, which the link scan read and discarded, and its own libraries'
// install names and compatibility versions, from otool -l: observations,
// which build and judge nothing otherwise, so the protocol stays 3.
//
// Since Then's item 10, it also keeps the configure summary's lines from
// a passed build's log: an observation, so the protocol stays 3.
//
// Since the docs dig's I13, it lints a port the branch adds with
// --nitpick and keeps each target's lint warnings. Lint fails on errors
// alone, and every --nitpick finding is a warning (base's portlint.tcl at
// v2.12.6), so nothing is built or judged otherwise, and the protocol
// stays 3.
//
// Since serve's sudo rule names one command, the local provider names the
// check's directory as the program's argument, and the program sets
// MacPorts' PORTSRC and PATH itself, as /usr/bin/env did: the same
// environment, so nothing is built or judged otherwise, and the protocol
// stays 3.
const guestPin = "59334ed6b7e7542e253b3daa5f4c2f3273b113cdb85d2ec02a28b6ab729462b1"

// How the guest program builds is identified by VerifierProtocol, part of
// an environment's origin (decision 28). A change to guest.tcl fails this
// test until its author decides: raise the protocol, when the change
// alters how ports are built or judged, which ends reuse of evidence the
// program recorded before; or, for a change of wording only, update the
// pin.
func TestTheVerifierProtocolCoversTheGuestProgram(t *testing.T) {
	sum := sha256.Sum256(guestprogram.Program)
	require.Equal(t, 3, VerifierProtocol)
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
	require.Equal(t, "dockhand has begun to build each target with its own dependencies alone active, deactivating what a dependency built from source needed to build",
		p.IdentityChange(model.Environment{}, image+"; verifier 2", image+"; verifier 3"))
	require.Equal(t, "dockhand has begun to build otherwise", p.IdentityChange(model.Environment{}, image+"; verifier 2", image+"; verifier 9"))
	require.Empty(t, p.IdentityChange(model.Environment{}, image+"; verifier 1", "source sha256:ffff; setup 3; macports 2.12.6; tools 26.6; verifier 2"), "another image")
	require.Empty(t, p.IdentityChange(model.Environment{}, image+"; verifier 2", image+"; verifier 2"))
	require.Empty(t, p.IdentityChange(model.Environment{}, "", image+"; verifier 2"))
}
