package provision

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
	"github.com/stretchr/testify/require"
)

var testPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

type fakeMachine struct {
	images     map[string]image
	events     []string
	fail       string
	validation validation
	manifest   []byte
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func newFakeMachine(names ...string) *fakeMachine {
	images := make(map[string]image, len(names))
	for _, name := range names {
		images[name] = image{Name: name}
	}
	return &fakeMachine{images: images, validation: validation{Platform: testPlatform, MacPortsVersion: DefaultMacPortsVersion, GuestAgentVersion: AgentVersion}}
}

func (f *fakeMachine) event(name string) error {
	f.events = append(f.events, name)
	if f.fail == name {
		return errors.New("fixture failure")
	}
	return nil
}
func (f *fakeMachine) LockSetup(context.Context, string) (io.Closer, error) {
	return nopCloser{}, f.event("lock")
}
func (f *fakeMachine) Images(context.Context) (map[string]image, error) {
	return f.images, f.event("images")
}
func (f *fakeMachine) Pull(context.Context, string) error { return f.event("pull") }
func (f *fakeMachine) Clone(_ context.Context, source, destination string) error {
	if err := f.event("clone:" + source + ":" + destination); err != nil {
		return err
	}
	f.images[destination] = image{Name: destination}
	return nil
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
func (f *fakeMachine) InstallMacPorts(context.Context, string, Config, tart.MacOSRelease) error {
	return f.event("macports")
}
func (f *fakeMachine) WriteManifest(_ context.Context, _ string, value []byte) error {
	f.manifest = append([]byte(nil), value...)
	return f.event("manifest")
}
func (f *fakeMachine) Validate(context.Context, string, Config) (validation, error) {
	return f.validation, f.event("validate")
}
func (f *fakeMachine) Stop(_ context.Context, name string) error {
	current := f.images[name]
	current.Running = false
	f.images[name] = current
	return f.event("stop:" + name)
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
func (f *fakeMachine) Adopt(_ context.Context, source, destination string, replace bool) error {
	if replace {
		delete(f.images, destination)
	}
	if err := f.event("adopt:" + source + ":" + destination); err != nil {
		return err
	}
	f.images[destination] = image{Name: destination}
	return nil
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
	require.NotContains(t, machine.images, "dockhand-base-tahoe")
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
	require.Contains(t, script, "/v"+AgentVersion+"/")
	require.Contains(t, script, agentDigest)
	require.Contains(t, script, "shasum -a 256 -c")
	require.NotContains(t, script, "homebrew")
	var document struct{}
	require.NoError(t, xml.Unmarshal([]byte(agentPlist("fixture", "--run-agent", "/tmp")), &document))
}
