package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// runnerCache is Base's cache of what it found of the developer tools,
// keyed by each Xcode's Info.plist and the tools' version. It is emptied
// before each probe so every selection is read afresh.
const runnerCache = "/opt/local/var/macports/cache"

// harvestRunner probes the Mac it runs on, one of GitHub's macOS runners,
// once with each installed Xcode selected and once with the Command Line
// Tools selected, switching xcode-select with sudo and putting back the
// runner's own selection after. name is the runner's image label,
// macos-15-intel, which the runner doesn't tell. MacPorts must be
// installed, as MacPorts' CI installs it.
func harvestRunner(ctx context.Context, name, out string) (err error) {
	original, err := exec.CommandContext(ctx, "/usr/bin/xcode-select", "-p").Output()
	if err != nil {
		return fmt.Errorf("xcode-select -p: %w", err)
	}
	developers := runnerDevelopers(filepath.Glob, os.Lstat)
	if len(developers) == 0 {
		return errors.New("no Xcode or Command Line Tools to probe")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	script, err := os.CreateTemp("", "facts-probe-*.tcl")
	if err != nil {
		return err
	}
	defer os.Remove(script.Name())
	if _, err := script.Write(probe); err != nil {
		return err
	}
	if err := script.Close(); err != nil {
		return err
	}
	cleanup := context.WithoutCancel(ctx)
	defer func() {
		err = errors.Join(err, sudo(cleanup, "/usr/bin/xcode-select", "--switch", strings.TrimSpace(string(original))))
	}()
	var failed []error
	for _, developer := range developers {
		if err := probeSelected(ctx, name, developer, script.Name(), out); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", developer, err))
			fmt.Fprintf(os.Stderr, "%s: %v\n", developer, err)
		} else {
			fmt.Fprintf(os.Stderr, "%s: probed\n", developer)
		}
	}
	return errors.Join(failed...)
}

// runnerDevelopers are the developer directories to probe: each Xcode in
// /Applications, the links GitHub's images add beside them (Xcode.app,
// Xcode_16.4.0.app) left out, then the Command Line Tools.
func runnerDevelopers(glob func(string) ([]string, error), lstat func(string) (os.FileInfo, error)) []string {
	apps, _ := glob("/Applications/Xcode*.app")
	var developers []string
	for _, app := range apps {
		info, err := lstat(app)
		if err != nil || !info.IsDir() {
			continue
		}
		developers = append(developers, filepath.Join(app, "Contents", "Developer"))
	}
	slices.Sort(developers)
	if info, err := lstat("/Library/Developer/CommandLineTools"); err == nil && info.IsDir() {
		developers = append(developers, "/Library/Developer/CommandLineTools")
	}
	return developers
}

// probeSelected selects one developer directory and probes Base's view of
// it, writing one document as the Tart harvest writes them.
func probeSelected(ctx context.Context, name, developer, script, out string) error {
	if err := sudo(ctx, "/usr/bin/xcode-select", "--switch", developer); err != nil {
		return err
	}
	if err := sudo(ctx, "/bin/rm", "-rf", runnerCache); err != nil {
		return err
	}
	profile, slug := "xcode", strings.TrimSuffix(filepath.Base(strings.TrimSuffix(developer, "/Contents/Developer")), ".app")
	if !strings.HasSuffix(developer, "/Contents/Developer") {
		profile, slug = "clt", "clt"
	}
	doc := probed{Image: name, Slug: slug, Profile: profile, Source: sourceRunner, Developer: developer,
		Date: time.Now().UTC().Format(time.RFC3339), Host: runnerHost()}
	result := filepath.Join(os.TempDir(), "dockhand-facts-"+slug+".json")
	output, err := exec.CommandContext(ctx, "/opt/local/bin/port-tclsh", script, result).CombinedOutput()
	doc.Output = string(output)
	if err != nil {
		doc.Errors = append(doc.Errors, "probe: "+err.Error())
	}
	facts, err := os.ReadFile(result)
	if err != nil {
		return err
	}
	defer os.Remove(result)
	if !json.Valid(facts) {
		return fmt.Errorf("the probe wrote JSON that doesn't parse")
	}
	doc.Facts = facts
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, name+"-"+slug+".json"), append(data, '\n'), 0o644)
}

// runnerHost is what GitHub tells of the runner: its image's version and
// the run that probed it.
func runnerHost() map[string]any {
	host := map[string]any{}
	for key, env := range map[string]string{"image_os": "ImageOS", "image_version": "ImageVersion"} {
		if value := os.Getenv(env); value != "" {
			host[key] = value
		}
	}
	if id := os.Getenv("GITHUB_RUN_ID"); id != "" {
		host["run"] = os.Getenv("GITHUB_SERVER_URL") + "/" + os.Getenv("GITHUB_REPOSITORY") + "/actions/runs/" + id
	}
	return host
}

func sudo(ctx context.Context, args ...string) error {
	output, err := exec.CommandContext(ctx, "/usr/bin/sudo", append([]string{"-n"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
