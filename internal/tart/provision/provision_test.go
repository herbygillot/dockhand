package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/herbygillot/dockhand/internal/macports"
	"io"
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/stretchr/testify/require"
)

var testPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

type fakeMachine struct {
	images     map[string]image
	personal   map[string]image
	hostKeys   map[string]bool
	events     []string
	fail       string
	failures   map[string]error
	format     string
	validation validation
	onValidate func(*validation)
	manifest   []byte
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func newFakeMachine(names ...string) *fakeMachine {
	images := make(map[string]image, len(names))
	for _, name := range names {
		images[name] = image{Name: name}
	}
	keys := map[string]bool{}
	for _, name := range names {
		keys[name] = true
	}
	return &fakeMachine{images: images, personal: map[string]image{}, hostKeys: keys, validation: validation{Platform: testPlatform, MacPortsVersion: macports.DefaultBaseVersion, GuestAgentVersion: "development snapshot", CommandLineTools: "26.6"}}
}

func (f *fakeMachine) event(name string) error {
	f.events = append(f.events, name)
	if f.fail == name {
		return errors.New("fixture failure")
	}
	return f.failures[name]
}
func (f *fakeMachine) LockSetup(context.Context, string) (io.Closer, error) {
	return nopCloser{}, f.event("lock")
}
func (f *fakeMachine) Images(context.Context) (map[string]image, error) {
	return f.images, f.event("images")
}
func (f *fakeMachine) PersonalImages(context.Context) (map[string]image, error) {
	return f.personal, f.event("personal")
}
func (f *fakeMachine) Import(_ context.Context, source, destination string) error {
	if err := f.event("import:" + source + ":" + destination); err != nil {
		return err
	}
	f.images[destination] = image{Name: destination}
	return nil
}
func (f *fakeMachine) Connect(_ context.Context, name, alias string, bootstrap bool) error {
	if bootstrap {
		f.hostKeys[alias] = true
		return f.event("bootstrap:" + name + ":" + alias)
	}
	if !f.hostKeys[alias] {
		return errors.New("no host keys recorded for " + alias)
	}
	return f.event("connect:" + name + ":" + alias)
}
func (f *fakeMachine) HostKeysRecorded(image string) bool { return f.hostKeys[image] }
func (f *fakeMachine) RecordHostKeys(from, to string) error {
	if !f.hostKeys[from] {
		return errors.New("no host keys recorded for " + from)
	}
	f.hostKeys[to] = true
	return f.event("keys:" + from + ":" + to)
}
func (f *fakeMachine) ForgetHostKeys(image string) error {
	delete(f.hostKeys, image)
	return f.event("forget:" + image)
}
func (f *fakeMachine) Pull(context.Context, string) error { return f.event("pull") }
func (f *fakeMachine) Clone(_ context.Context, source, destination string) error {
	if err := f.event("clone:" + source + ":" + destination); err != nil {
		return err
	}
	f.images[destination] = image{Name: destination}
	return nil
}
func (f *fakeMachine) DiskFormat(_ context.Context, name string) (string, error) {
	format := f.format
	if format == "" {
		format = "raw"
	}
	return format, f.event("format:" + name)
}
func (f *fakeMachine) Configure(context.Context, string) error { return f.event("configure") }
func (f *fakeMachine) Start(_ context.Context, name string) error {
	f.images[name] = image{Name: name, Running: true}
	return f.event("start")
}
func (f *fakeMachine) BootstrapAgent(context.Context, string) error { return f.event("agent") }
func (f *fakeMachine) ReadyAgent(context.Context, string) error     { return f.event("ready") }
func (f *fakeMachine) EnsureToolchain(context.Context, string) error {
	return f.event("toolchain")
}
func (f *fakeMachine) InstallXcode(_ context.Context, _ string, config Config) error {
	f.validation.XcodeVersion = config.XcodeVersion
	return f.event("xcode")
}
func (f *fakeMachine) InstallMacPorts(context.Context, string, Config, macos.Release) error {
	return f.event("macports")
}
func (f *fakeMachine) WriteManifest(_ context.Context, _ string, value []byte) error {
	f.manifest = append([]byte(nil), value...)
	return f.event("manifest")
}
func (f *fakeMachine) Validate(context.Context, string, Config) (validation, error) {
	if f.onValidate != nil {
		f.onValidate(&f.validation)
	}
	return f.validation, f.event("validate")
}
func (f *fakeMachine) Stop(_ context.Context, name string) error {
	current := f.images[name]
	current.Running = false
	if err := f.event("stop:" + name); err != nil {
		return err
	}
	f.images[name] = current
	return nil
}
func (f *fakeMachine) Delete(_ context.Context, name string) error {
	if err := f.event("delete:" + name); err != nil {
		return err
	}
	delete(f.images, name)
	return nil
}
func (f *fakeMachine) Rename(_ context.Context, from, to string) error {
	if err := f.event("rename:" + from + ":" + to); err != nil {
		return err
	}
	delete(f.images, from)
	f.images[to] = image{Name: to}
	return nil
}
func (f *fakeMachine) Adopt(ctx context.Context, source, destination string, replace bool) error {
	if err := f.event("adopt:" + source + ":" + destination); err != nil {
		return err
	}
	return adopt(ctx, f, source, destination, replace)
}

func testProvisioner(machine *fakeMachine) *Provisioner {
	return &Provisioner{Config: Config{Platform: testPlatform, Home: "/tmp/tart"}, backend: machine}
}

func TestExistingDefaultImageIsValidatedInDisposableClone(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe")
	result, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	require.True(t, result.Reused)
	require.Equal(t, "dockhand-base-tahoe", result.Image)
	require.Contains(t, machine.events, "clone:dockhand-base-tahoe:dockhand-base-tahoe-check")
	require.NotContains(t, machine.events, "pull")
	require.NotContains(t, machine.images, "dockhand-base-tahoe-check")
}

func TestMissingImageIsProvisionedAndAdoptedAfterValidation(t *testing.T) {
	machine := newFakeMachine()
	result, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	require.False(t, result.Reused)
	require.Contains(t, machine.images, "dockhand-base-tahoe")
	require.Contains(t, machine.images, "dockhand-golden-tahoe")
	require.NotContains(t, machine.images, "dockhand-base-tahoe-next")
	require.NotContains(t, machine.images, "dockhand-golden-tahoe-next")
	require.Less(t, index(machine.events, "validate"), index(machine.events, "adopt:dockhand-base-tahoe-next:dockhand-base-tahoe"))
	var manifest tart.ImageManifest
	require.NoError(t, json.Unmarshal(machine.manifest, &manifest))
	require.Equal(t, tart.ImageManifestProtocol, manifest.Protocol)
	require.Equal(t, "/opt/local", manifest.MacPortsPrefix)
	require.Equal(t, testPlatform, manifest.Platform)
}

func TestManifestRecordsObservedGuestAgentVersion(t *testing.T) {
	machine := newFakeMachine()
	machine.validation.GuestAgentVersion = tart.GuestAgentRelease + "-cb39b12"
	result, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	var manifest tart.ImageManifest
	require.NoError(t, json.Unmarshal(machine.manifest, &manifest))
	require.Equal(t, machine.validation.GuestAgentVersion, manifest.GuestAgentVersion)
	require.Equal(t, result.GuestAgentVersion, manifest.GuestAgentVersion)
	require.Less(t, index(machine.events, "validate"), index(machine.events, "manifest"))
}

func TestXcodeProfileInstallsXcodeBeforeMacPorts(t *testing.T) {
	directory := t.TempDir()
	archive := directory + "/Xcode_26.6_Apple_silicon.xip"
	require.NoError(t, os.WriteFile(archive, nil, 0o600))
	machine := newFakeMachine()
	provisioner := testProvisioner(machine)
	provisioner.Config.Xcode = directory
	result, err := provisioner.Run(t.Context(), Options{})
	require.NoError(t, err)
	require.Equal(t, "dockhand-xcode-tahoe", result.Image)
	require.Equal(t, "dockhand-golden-xcode-tahoe", result.GoldenImage)
	require.Equal(t, "26.6", result.XcodeVersion)
	require.Less(t, index(machine.events, "toolchain"), index(machine.events, "xcode"))
	require.Less(t, index(machine.events, "xcode"), index(machine.events, "macports"))
}

func TestConventionalImageNamesRequireTheirMatchingProfile(t *testing.T) {
	directory := t.TempDir()
	archive := directory + "/Xcode_26.6.xip"
	require.NoError(t, os.WriteFile(archive, nil, 0o600))
	_, _, err := normalize(Config{Platform: testPlatform, Image: "dockhand-base-tahoe", Xcode: archive})
	require.ErrorContains(t, err, "cannot replace the conventional base image")
	_, _, err = normalize(Config{Platform: testPlatform, Image: "dockhand-xcode-tahoe"})
	require.ErrorContains(t, err, "requires --xcode")
}

func TestFailedRebuildPreservesExistingImages(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe", "dockhand-golden-tahoe")
	machine.fail = "macports"
	_, err := testProvisioner(machine).Run(t.Context(), Options{Rebuild: true})
	require.ErrorContains(t, err, "fixture failure")
	require.Contains(t, machine.images, "dockhand-base-tahoe")
	require.Contains(t, machine.images, "dockhand-golden-tahoe")
	require.NotContains(t, machine.events, "delete:dockhand-base-tahoe")
}

func TestFailedAdoptionRetainsProvenCandidate(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe", "dockhand-golden-tahoe")
	machine.fail = "adopt:dockhand-base-tahoe-next:dockhand-base-tahoe"
	_, err := testProvisioner(machine).Run(t.Context(), Options{Rebuild: true})
	require.ErrorContains(t, err, "proven candidate remains")
	require.Contains(t, machine.images, "dockhand-base-tahoe-next")
	require.Contains(t, machine.images, "dockhand-base-tahoe")
}

func TestCheckRefusesMissingImage(t *testing.T) {
	machine := newFakeMachine()
	_, err := testProvisioner(machine).Run(t.Context(), Options{Check: true})
	require.ErrorContains(t, err, "does not exist")
	require.NotContains(t, machine.events, "pull")
}

func index(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}
	return -1
}

func TestAgentBootstrapPinsAndChecksTheReleaseAsset(t *testing.T) {
	script := agentInstallScript()
	require.Contains(t, script, "/v"+tart.GuestAgentRelease+"/")
	require.Contains(t, script, tart.GuestAgentDigest)
	require.Contains(t, script, "shasum -a 256 -c")
	require.NotContains(t, script, "homebrew")
	var document struct{}
	require.NoError(t, xml.Unmarshal([]byte(agentPlist("fixture", "--run-agent", "/tmp", "")), &document))
	daemon := agentPlist("fixture", "--run-daemon", "/var/empty", agentDaemonLog)
	require.NoError(t, xml.Unmarshal([]byte(daemon), &document))
	require.Contains(t, daemon, "<key>StandardErrorPath</key><string>"+agentDaemonLog+"</string>", "the daemon's disk resize is logged")
	require.Contains(t, script, "<key>StandardOutPath</key><string>"+agentDaemonLog+"</string>")
}

func TestInterruptedAdoptionDoesNotHidePreviousImageWithGoldenRestore(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe-previous", "dockhand-golden-tahoe")
	_, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.ErrorContains(t, err, "previous image is preserved")
	require.NotContains(t, machine.events, "adopt:dockhand-golden-tahoe:dockhand-base-tahoe")
	require.Contains(t, machine.images, "dockhand-base-tahoe-previous")
}

// A source with an ASIF disk is declined while it is a stopped clone, before
// it ever runs: a running ASIF VM keeps Tart from listing any VM
// (openai/tart#1344). The clone is cleaned up.
func TestASIFSourceIsDeclinedBeforeItRuns(t *testing.T) {
	machine := newFakeMachine()
	machine.format = "asif"
	_, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.ErrorContains(t, err, "has an ASIF disk")
	require.ErrorContains(t, err, "openai/tart#1344")
	require.NotContains(t, machine.events, "configure")
	require.NotContains(t, machine.events, "start")
	require.Contains(t, machine.events, "delete:dockhand-base-tahoe-next")
	require.NotContains(t, machine.images, "dockhand-base-tahoe-next")
}

// A failed setup's cleanup deletes its guest even when stopping it failed,
// and says what went wrong with either.
func TestCleanupReportsItsErrorsAndStillDeletes(t *testing.T) {
	machine := newFakeMachine()
	machine.fail = "macports"
	machine.failures = map[string]error{"stop:dockhand-base-tahoe-next": errors.New("fixture stop failure")}
	_, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.ErrorContains(t, err, "fixture failure")
	require.ErrorContains(t, err, "cleaning up dockhand-base-tahoe-next")
	require.ErrorContains(t, err, "fixture stop failure")
	require.Contains(t, machine.events, "delete:dockhand-base-tahoe-next")

	machine = newFakeMachine()
	machine.fail = "macports"
	machine.failures = map[string]error{"delete:dockhand-base-tahoe-next": errors.New("fixture delete failure")}
	_, err = testProvisioner(machine).Run(t.Context(), Options{})
	require.ErrorContains(t, err, "fixture delete failure")
	require.ErrorContains(t, err, "tart delete dockhand-base-tahoe-next")
}

// An image carrying Command Line Tools of another generation than its
// release's, as both Tahoe images carried the macOS 27 tools, is reported
// by --check and refused by a build (decision 13).
func TestToolsOfAnotherGenerationAreReportedAndRefused(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe")
	machine.validation.CommandLineTools = "27.0"
	_, err := testProvisioner(machine).Run(t.Context(), Options{Check: true})
	require.ErrorContains(t, err, "image dockhand-base-tahoe has Command Line Tools 27.0; Tahoe uses generation 26; rerun with --rebuild")

	machine = newFakeMachine()
	machine.validation.CommandLineTools = "27.0"
	_, err = testProvisioner(machine).Run(t.Context(), Options{})
	require.ErrorContains(t, err, "provisioned image has Command Line Tools 27.0; Tahoe uses generation 26")
	require.NotContains(t, machine.images, "dockhand-base-tahoe")

	machine = newFakeMachine()
	result, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	require.Equal(t, "26.6", result.CommandLineTools)
}

// A fresh image is reached by bootstrap before anything is installed, and
// the host keys its candidate presented are recorded under the image and
// its golden copy, not the candidate's temporary name.
func TestProvisionRecordsHostKeysUnderTheImage(t *testing.T) {
	machine := newFakeMachine()
	_, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	require.Less(t, index(machine.events, "bootstrap:dockhand-base-tahoe-next:dockhand-base-tahoe-next"), index(machine.events, "agent"))
	require.Contains(t, machine.events, "keys:dockhand-base-tahoe-next:dockhand-base-tahoe")
	require.Contains(t, machine.events, "keys:dockhand-base-tahoe-next:dockhand-golden-tahoe")
	require.True(t, machine.hostKeys["dockhand-base-tahoe"])
	require.True(t, machine.hostKeys["dockhand-golden-tahoe"])
	require.False(t, machine.hostKeys["dockhand-base-tahoe-next"])
}

// An existing image checks the way verification reaches a clone, by its
// recorded host keys and dockhand's key.
func TestCheckReachesTheCloneByTheImagesKeys(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe")
	_, err := testProvisioner(machine).Run(t.Context(), Options{Check: true})
	require.NoError(t, err)
	require.Contains(t, machine.events, "connect:dockhand-base-tahoe-check:dockhand-base-tahoe")
}

// An image made before dockhand reached guests over SSH gets dockhand's key
// on its next setup, in a candidate adopted like a rebuild, without
// installing anything again; --check says to run setup instead.
func TestAnImageWithoutTheKeyIsGivenIt(t *testing.T) {
	machine := newFakeMachine("dockhand-base-tahoe", "dockhand-golden-tahoe")
	machine.hostKeys = map[string]bool{}
	_, err := testProvisioner(machine).Run(t.Context(), Options{Check: true})
	require.ErrorContains(t, err, "predates dockhand's SSH key; run dockhand setup without --check")

	result, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	require.False(t, result.Reused)
	require.Contains(t, machine.events, "clone:dockhand-base-tahoe:dockhand-base-tahoe-next")
	require.Contains(t, machine.events, "bootstrap:dockhand-base-tahoe-next:dockhand-base-tahoe-next")
	require.Contains(t, machine.events, "adopt:dockhand-base-tahoe-next:dockhand-base-tahoe")
	require.NotContains(t, machine.events, "pull")
	require.NotContains(t, machine.events, "macports", "nothing is installed again")
	require.True(t, machine.hostKeys["dockhand-base-tahoe"])
	require.True(t, machine.hostKeys["dockhand-golden-tahoe"])
}

// An image an earlier dockhand made in the person's own Tart home is
// copied into dockhand's when it still validates, and provisioned afresh
// when it does not, as a Tahoe image with the macOS 27 tools does.
func TestAnImageInThePersonsHomeIsImportedWhenItValidates(t *testing.T) {
	machine := newFakeMachine()
	machine.personal = map[string]image{"dockhand-base-tahoe": {Name: "dockhand-base-tahoe"}}
	result, err := testProvisioner(machine).Run(t.Context(), Options{})
	require.NoError(t, err)
	require.Equal(t, "26.6", result.CommandLineTools)
	require.Contains(t, machine.events, "import:dockhand-base-tahoe:dockhand-base-tahoe-next")
	require.NotContains(t, machine.events, "pull")
	require.Contains(t, machine.images, "dockhand-base-tahoe")

	machine = newFakeMachine()
	machine.personal = map[string]image{"dockhand-base-tahoe": {Name: "dockhand-base-tahoe"}}
	machine.validation.CommandLineTools = "27.0"
	machine.failures = map[string]error{}
	var progress bytes.Buffer
	provisioner := testProvisioner(machine)
	provisioner.Progress = &progress
	calls := 0
	machine.onValidate = func(v *validation) {
		calls++
		if calls > 1 {
			v.CommandLineTools = "26.6"
		}
	}
	result, err = provisioner.Run(t.Context(), Options{})
	require.NoError(t, err)
	require.Contains(t, machine.events, "import:dockhand-base-tahoe:dockhand-base-tahoe-next")
	require.Contains(t, machine.events, "pull", "the unsuitable copy is replaced by a new image")
	require.Contains(t, progress.String(), "cannot be used")
	require.Contains(t, progress.String(), "Command Line Tools 27.0")
	require.Equal(t, "26.6", result.CommandLineTools)
}
