package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/tart"
)

const DefaultMacPortsVersion = "2.12.6"

type Config struct {
	Executable      string
	Home            string
	Image           string
	Source          string
	MacPortsVersion string
	GuestPrefix     string
	Platform        record.Platform
	Xcode           string
	XcodeArchive    string
	XcodeVersion    string
}

type Options struct {
	Check   bool
	Rebuild bool
}

type Result struct {
	Image             string          `json:"image"`
	GoldenImage       string          `json:"golden_image,omitempty"`
	Source            string          `json:"source,omitempty"`
	Platform          record.Platform `json:"platform"`
	MacPortsVersion   string          `json:"macports_version"`
	GuestAgentVersion string          `json:"guest_agent_version"`
	XcodeVersion      string          `json:"xcode_version,omitempty"`
	Reused            bool            `json:"reused"`
}

type image struct {
	Name    string
	Running bool
}

type validation struct {
	Platform          record.Platform
	MacPortsVersion   string
	GuestAgentVersion string
	XcodeVersion      string
}

type machine interface {
	LockSetup(context.Context, string) (io.Closer, error)
	Images(context.Context) (map[string]image, error)
	Pull(context.Context, string) error
	Clone(context.Context, string, string) error
	Configure(context.Context, string) error
	Start(context.Context, string) error
	BootstrapAgent(context.Context, string) error
	ReadyAgent(context.Context, string) error
	EnsureToolchain(context.Context, string) error
	InstallXcode(context.Context, string, Config) error
	InstallMacPorts(context.Context, string, Config, tart.MacOSRelease) error
	WriteManifest(context.Context, string, []byte) error
	Validate(context.Context, string, Config) (validation, error)
	Stop(context.Context, string) error
	Delete(context.Context, string) error
	Rename(context.Context, string, string) error
	Adopt(context.Context, string, string, bool) error
}

type Provisioner struct {
	Config   Config
	Progress io.Writer
	backend  machine
}

func (p *Provisioner) Run(ctx context.Context, options Options) (Result, error) {
	config, release, err := normalize(p.Config)
	if err != nil {
		return Result{}, err
	}
	machine := p.backend
	if machine == nil {
		machine = newNative(config, p.Progress)
	}
	setupLock, err := machine.LockSetup(ctx, config.Image)
	if err != nil {
		return Result{}, err
	}
	defer setupLock.Close()
	p.say("Checking Tart images for %s...", release.Name)
	images, err := machine.Images(ctx)
	if err != nil {
		return Result{}, err
	}
	golden := goldenName(config.Image)
	if !options.Check && !options.Rebuild && images[config.Image].Name == "" && images[golden].Name != "" {
		if images[golden].Running {
			return Result{}, fmt.Errorf("setup: recovery image %s is running", golden)
		}
		p.say("Restoring %s from %s...", config.Image, golden)
		if err := machine.Adopt(ctx, golden, config.Image, false); err != nil {
			return Result{}, err
		}
		images[config.Image] = image{Name: config.Image}
	}
	if images[config.Image].Name != "" && !options.Rebuild {
		if images[config.Image].Running {
			return Result{}, fmt.Errorf("setup: image %s must be stopped", config.Image)
		}
		return p.check(ctx, machine, config, golden, true)
	}
	if options.Check {
		return Result{}, fmt.Errorf("setup: verification image %s does not exist", config.Image)
	}
	return p.provision(ctx, machine, config, release, golden, images[config.Image].Name != "")
}

func normalize(config Config) (Config, tart.MacOSRelease, error) {
	release, err := tart.ReleaseForPlatform(config.Platform)
	if err != nil {
		return config, release, err
	}
	if config.Executable == "" {
		config.Executable = "tart"
	}
	if config.Home == "" {
		config.Home = os.Getenv("TART_HOME")
	}
	if config.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config, release, err
		}
		config.Home = filepath.Join(home, ".tart")
	}
	config.Home, err = filepath.Abs(config.Home)
	if err != nil {
		return config, release, err
	}
	if config.Xcode != "" {
		config.XcodeArchive, config.XcodeVersion, err = selectXcode(config.Xcode, release)
		if err != nil {
			return config, release, err
		}
	}
	if config.Image == "" {
		if config.XcodeArchive != "" {
			config.Image, err = tart.DefaultXcodeImageName(config.Platform)
		} else {
			config.Image, err = tart.DefaultImageName(config.Platform)
		}
		if err != nil {
			return config, release, err
		}
	}
	if config.Source == "" {
		config.Source, err = tart.DefaultSource(config.Platform)
		if err != nil {
			return config, release, err
		}
	}
	if config.MacPortsVersion == "" {
		config.MacPortsVersion = DefaultMacPortsVersion
	}
	if config.GuestPrefix == "" {
		config.GuestPrefix = "/opt/local"
	}
	if !safeName(config.Image) || strings.TrimSpace(config.Source) != config.Source || strings.ContainsAny(config.Source, "\x00\r\n\t ") || !versionPattern.MatchString(config.MacPortsVersion) {
		return config, release, fmt.Errorf("setup: invalid image, source, or MacPorts version")
	}
	if config.GuestPrefix != "/opt/local" {
		return config, release, fmt.Errorf("setup: the MacPorts package installer requires guest prefix /opt/local")
	}
	if config.XcodeArchive != "" && strings.HasPrefix(config.Image, "dockhand-base-") {
		return config, release, fmt.Errorf("setup: Xcode profiles cannot replace the conventional base image; omit --image or select a distinct image")
	}
	if config.XcodeArchive == "" && strings.HasPrefix(config.Image, "dockhand-xcode-") {
		return config, release, fmt.Errorf("setup: Xcode image %s requires --xcode", config.Image)
	}
	return config, release, nil
}

var versionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)+$`)

func safeName(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "/\\:\x00\r\n\t ")
}

func goldenName(image string) string {
	if suffix, ok := strings.CutPrefix(image, "dockhand-base-"); ok {
		return "dockhand-golden-" + suffix
	}
	if suffix, ok := strings.CutPrefix(image, "dockhand-xcode-"); ok {
		return "dockhand-golden-xcode-" + suffix
	}
	return image + "-golden"
}

func (p *Provisioner) check(ctx context.Context, machine machine, config Config, golden string, reused bool) (Result, error) {
	name := config.Image + "-check"
	if err := discard(ctx, machine, name); err != nil {
		return Result{}, err
	}
	p.say("Validating %s in a disposable clone...", config.Image)
	if err := machine.Clone(ctx, config.Image, name); err != nil {
		return Result{}, err
	}
	defer cleanup(machine, name)
	if err := machine.Start(ctx, name); err != nil {
		return Result{}, err
	}
	if err := machine.ReadyAgent(ctx, name); err != nil {
		return Result{}, err
	}
	checked, err := machine.Validate(ctx, name, config)
	if err != nil {
		return Result{}, fmt.Errorf("setup: image %s failed validation: %w", config.Image, err)
	}
	if checked.Platform != config.Platform {
		return Result{}, fmt.Errorf("setup: image platform is %+v; expected %+v", checked.Platform, config.Platform)
	}
	if checked.MacPortsVersion != config.MacPortsVersion {
		return Result{}, fmt.Errorf("setup: image has MacPorts %s; expected %s; rerun with --rebuild", checked.MacPortsVersion, config.MacPortsVersion)
	}
	if checked.XcodeVersion != config.XcodeVersion {
		return Result{}, fmt.Errorf("setup: image has Xcode %s; expected %s; rerun with --rebuild", checked.XcodeVersion, config.XcodeVersion)
	}
	if err := machine.Stop(ctx, name); err != nil {
		return Result{}, err
	}
	if err := machine.Delete(ctx, name); err != nil {
		return Result{}, err
	}
	return Result{Image: config.Image, GoldenImage: golden, Platform: checked.Platform, MacPortsVersion: checked.MacPortsVersion, GuestAgentVersion: checked.GuestAgentVersion, XcodeVersion: checked.XcodeVersion, Reused: reused}, nil
}

func (p *Provisioner) provision(ctx context.Context, machine machine, config Config, release tart.MacOSRelease, golden string, replacing bool) (Result, error) {
	next, goldenNext := config.Image+"-next", golden+"-next"
	if err := discard(ctx, machine, next); err != nil {
		return Result{}, err
	}
	if err := discard(ctx, machine, goldenNext); err != nil {
		return Result{}, err
	}
	p.say("Pulling %s...", config.Source)
	if err := machine.Pull(ctx, config.Source); err != nil {
		return Result{}, err
	}
	p.say("Preparing %s...", next)
	if err := machine.Clone(ctx, config.Source, next); err != nil {
		return Result{}, err
	}
	keepNext := false
	defer func() {
		if !keepNext {
			cleanup(machine, next)
		}
	}()
	if err := machine.Configure(ctx, next); err != nil {
		return Result{}, err
	}
	if err := machine.Start(ctx, next); err != nil {
		return Result{}, err
	}
	p.say("Installing the Tart guest agent...")
	if err := machine.BootstrapAgent(ctx, next); err != nil {
		return Result{}, err
	}
	if err := machine.ReadyAgent(ctx, next); err != nil {
		return Result{}, err
	}
	p.say("Checking the guest build toolchain...")
	if err := machine.EnsureToolchain(ctx, next); err != nil {
		return Result{}, err
	}
	if config.XcodeArchive != "" {
		p.say("Installing Xcode %s...", config.XcodeVersion)
		if err := machine.InstallXcode(ctx, next, config); err != nil {
			return Result{}, err
		}
	}
	p.say("Installing MacPorts %s...", config.MacPortsVersion)
	if err := machine.InstallMacPorts(ctx, next, config, release); err != nil {
		return Result{}, err
	}
	manifest, err := json.Marshal(tart.ImageManifest{
		Protocol: tart.ImageManifestProtocol, Source: config.Source, Platform: config.Platform,
		MacPortsPrefix: config.GuestPrefix, MacPortsVersion: config.MacPortsVersion,
		GuestAgentVersion: AgentVersion, XcodeVersion: config.XcodeVersion,
	})
	if err != nil {
		return Result{}, err
	}
	if err := machine.WriteManifest(ctx, next, manifest); err != nil {
		return Result{}, err
	}
	checked, err := machine.Validate(ctx, next, config)
	if err != nil {
		return Result{}, err
	}
	if checked.Platform != config.Platform || checked.MacPortsVersion != config.MacPortsVersion || checked.XcodeVersion != config.XcodeVersion {
		return Result{}, fmt.Errorf("setup: provisioned image does not match its requested platform, MacPorts, or Xcode version")
	}
	if err := machine.Stop(ctx, next); err != nil {
		return Result{}, err
	}
	if err := machine.Clone(ctx, next, goldenNext); err != nil {
		return Result{}, err
	}
	keepNext = true
	if err := machine.Adopt(ctx, next, config.Image, replacing); err != nil {
		return Result{}, fmt.Errorf("setup: adopting %s failed; proven candidate remains as %s: %w", config.Image, next, err)
	}
	if err := discard(ctx, machine, golden); err != nil {
		return Result{}, err
	}
	if err := machine.Rename(ctx, goldenNext, golden); err != nil {
		return Result{}, err
	}
	if err := machine.Delete(ctx, next); err != nil {
		return Result{}, err
	}
	keepNext = false
	return Result{Image: config.Image, GoldenImage: golden, Source: config.Source, Platform: checked.Platform, MacPortsVersion: checked.MacPortsVersion, GuestAgentVersion: checked.GuestAgentVersion, XcodeVersion: checked.XcodeVersion}, nil
}

func discard(ctx context.Context, machine machine, name string) error {
	images, err := machine.Images(ctx)
	if err != nil {
		return err
	}
	current := images[name]
	if current.Name == "" {
		return nil
	}
	if current.Running {
		return fmt.Errorf("setup: temporary image %s is still running", name)
	}
	return machine.Delete(ctx, name)
}

func cleanup(machine machine, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = machine.Stop(ctx, name)
	_ = machine.Delete(ctx, name)
}

func (p *Provisioner) say(format string, args ...any) {
	if p.Progress != nil {
		_, _ = fmt.Fprintf(p.Progress, format+"\n", args...)
	}
}
