package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/verify"
)

func settings(c Config) (Config, error) {
	if c.Capacity == 0 {
		c.Capacity = 2
	}
	if c.GuestPrefix == "" {
		c.GuestPrefix = macports.DefaultPrefix
	}
	runtime, err := (tartvm.Client{Executable: c.Executable, Home: c.Home}).Resolve()
	if err != nil {
		return c, err
	}
	c.Executable, c.Home = runtime.Executable, runtime.Home
	if c.ArtifactDirectory == "" || !filepath.IsAbs(c.GuestPrefix) || c.Capacity < 1 || strings.ContainsAny(c.GuestPrefix, "\x00\r\n") {
		return c, fmt.Errorf("tart: artifact directory, positive capacity, and an absolute guest prefix are required")
	}
	c.ArtifactDirectory, err = tartvm.CanonicalDirectory(c.ArtifactDirectory)
	return c, err
}
func (p *Provider) machineFor(c Config, guard *os.File) machine {
	if p.backend != nil {
		return p.backend
	}
	return newNative(c, guard, &p.images, p.State)
}
func (p *Provider) Capabilities(ctx context.Context) (verify.Capabilities, error) {
	c, err := settings(p.Config)
	if err != nil {
		return verify.Capabilities{}, err
	}

	if _, err = p.machineFor(c, nil).Running(ctx); err != nil {
		return verify.Capabilities{}, err
	}
	var platforms []record.Platform
	if c.Platform != (record.Platform{}) {
		platforms = []record.Platform{c.Platform}
	}
	return verify.Capabilities{Name: verify.ProviderTart, Platforms: platforms, Isolated: true, Capacity: c.Capacity}, nil
}
func (p *Provider) BuildConfigForImage(ctx context.Context, platform record.Platform, options BuildOptions, image string) (record.BuildConfig, error) {
	config := p.Config
	config.Image = image
	provider := &Provider{Config: config, State: p.State, Repository: p.Repository, Repo: p.Repo, HTTP: p.HTTP, backend: p.backend}
	return provider.BuildConfig(ctx, platform, options)
}

func (p *Provider) BuildConfig(ctx context.Context, platform record.Platform, options BuildOptions) (record.BuildConfig, error) {
	if options.Tests == record.TestWorkflow {
		return record.BuildConfig{}, fmt.Errorf("tart: workflow test policy is unsupported")
	}
	c, err := settings(p.Config)
	if err != nil {
		return record.BuildConfig{}, err
	}
	if c.Image == "" {
		if options.NeedsXcode {
			c.Image, err = tartvm.DefaultXcodeImageName(platform)
		} else {
			c.Image, err = tartvm.DefaultImageName(platform)
		}
		if err != nil {
			return record.BuildConfig{}, err
		}
	}
	c.Platform = platform
	if c.PortIndexURL == "" {
		c.PortIndexURL, err = portindex.DefaultMirrorURL(platform)
		if err != nil {
			return record.BuildConfig{}, err
		}
	}
	if p.State != nil {
		pool, e := p.State.ProviderPool(ctx, "tart_"+digest([]byte(c.Home)))
		if e == nil && (pool.Directory != c.ArtifactDirectory || p.Config.Capacity != 0 && pool.Capacity != p.Config.Capacity) {
			return record.BuildConfig{}, fmt.Errorf("tart: configuration differs from the existing pool")
		}
		if e != nil && !errors.Is(e, state.ErrNotFound) {
			return record.BuildConfig{}, e
		}
	}
	progress.VerboseReport(ctx, "Inspecting Tart image %s", c.Image)
	environment, err := p.machineFor(c, nil).Environment(ctx)
	if err != nil {
		if p.Config.Image == "" {
			setup := "run dockhand setup"
			if options.NeedsXcode {
				setup = "run dockhand setup --xcode <archive-or-directory>"
			}
			return record.BuildConfig{}, fmt.Errorf("tart: default image %s is unavailable; %s or select --image: %w", c.Image, setup, err)
		}
		return record.BuildConfig{}, err
	}
	capabilities, observed, err := p.cachedImageCapabilities(ctx, environment.Digest)
	if err != nil {
		return record.BuildConfig{}, err
	}
	if observed {
		accepted := record.BuildConfig{Provider: verify.ProviderTart, Platform: platform, EnvironmentDigest: environment.Digest, NeedsXcode: options.NeedsXcode, CapabilitiesRequired: true}
		if problem := capabilityProblem(capabilities, c, accepted); problem != "" {
			return record.BuildConfig{}, fmt.Errorf("%w: image %s is incompatible: %s", ErrImageUnavailable, c.Image, problem)
		}
		warnMacPortsVersionSkew(ctx, options.HostMacPortsVersion, c.Image, capabilities.Capabilities.MacPortsVersion)
	}
	resolvedIndex, err := portindex.ResolveTool(ctx, portindex.Config{Executable: c.PortIndexExecutable, Digest: c.PortIndexDigest})
	if err != nil {
		return record.BuildConfig{}, err
	}
	c.PortIndexExecutable, c.PortIndexDigest = resolvedIndex.Executable, resolvedIndex.Digest
	// Capacity is pool policy; zero permits an existing pool's recorded limit.
	c.Capacity = p.Config.Capacity
	raw, err := json.Marshal(c)
	if err != nil {
		return record.BuildConfig{}, err
	}
	config := record.BuildConfig{Provider: verify.ProviderTart, Platform: platform, EnvironmentDigest: environment.Digest, VerifierDigest: verifierDigest(), ProviderConfig: raw, NeedsXcode: options.NeedsXcode, CapabilitiesRequired: true, Tests: options.Tests, FromSource: options.FromSource}
	if observed {
		config.CapabilityDigest = capabilities.CapabilityDigest
	}
	return config, verify.ValidateConfig(config)
}

// warnMacPortsVersionSkew says so when the host evaluates ports with one
// MacPorts Base and the image builds with another. Neither is wrong on its
// own, but the evaluation that chose the edit and the build that proves it
// then run on different Base releases. An image never observed yet cannot be
// compared here; setup warns at provisioning time instead.
func warnMacPortsVersionSkew(ctx context.Context, host, image, guest string) {
	if host == "" || guest == "" || host == guest {
		return
	}
	progress.Report(ctx, "Warning: the host evaluates ports with MacPorts %s, but image %s builds with MacPorts %s; pass -p or set MACPORTS_PREFIX to the install that matches, or run dockhand setup --macports-version %s", host, image, guest, host)
}

func validateRequest(r verify.Request) error {
	if !requestID(r.ID) || r.AttemptID == "" || r.Spec.Config.Provider != verify.ProviderTart || !r.Spec.Config.CapabilitiesRequired || len(r.Spec.Inputs) != 0 {
		return fmt.Errorf("tart: one concrete verification target without artifact inputs is required")
	}
	if r.Spec.Config.Tests == record.TestWorkflow {
		return fmt.Errorf("tart: workflow test policy is unsupported")
	}
	if err := verify.ValidateConfig(r.Spec.Config); err != nil {
		return err
	}
	if (r.Spec.Source.Commit != "" && !git.ValidObjectID(string(r.Spec.Source.Commit))) || !git.ValidObjectID(string(r.Spec.Source.Tree)) {
		return fmt.Errorf("tart: immutable source tree and an optional valid commit are required")
	}
	if r.Spec.Config.VerifierDigest != "" && r.Spec.Config.VerifierDigest != verifierDigest() {
		return fmt.Errorf("tart: verifier implementation changed; submit a new verification request")
	}
	for _, target := range append([]record.Target{r.Spec.Target}, r.Spec.Preinstall...) {
		if !safeToken(target.Name) || (target.Subport != "" && !safeToken(target.Subport)) || !validPortfile(target.Portfile) {
			return fmt.Errorf("tart: invalid target")
		}
		for variant := range target.Variants {
			if !safeToken(variant) || strings.HasPrefix(variant, "+") || strings.HasPrefix(variant, "-") {
				return fmt.Errorf("tart: invalid variant")
			}
		}
	}
	return nil
}

var _ verify.Provider = (*Provider)(nil)

func requestID(id record.RequestID) bool {
	return id != "" && !strings.ContainsAny(string(id), "\x00\r\n\t ")
}

type Config struct {
	Executable          string
	Image               string
	ArtifactDirectory   string
	Home                string
	Capacity            int
	Platform            record.Platform
	GuestPrefix         string
	PortIndexExecutable string
	PortIndexDigest     string
	PortIndexURL        string
	// TestTimeout bounds the port's test phase in the guest; zero means
	// DefaultTestTimeout. A timed-out test counts as a failed test.
	TestTimeout time.Duration
}

// DefaultTestTimeout is how long the guest lets a port's tests run.
const DefaultTestTimeout = 30 * time.Minute

func (c Config) testTimeout() time.Duration {
	if c.TestTimeout <= 0 {
		return DefaultTestTimeout
	}
	return c.TestTimeout
}

type Environment struct {
	Digest   string
	Platform record.Platform
}

type BuildOptions struct {
	Tests      record.TestPolicy
	FromSource bool
	NeedsXcode bool
	// HostMacPortsVersion is the MacPorts Base that evaluated the port on the
	// host. It selects nothing; the provider warns when the image's observed
	// Base differs, since host evaluation and guest builds then disagree.
	HostMacPortsVersion string
}
