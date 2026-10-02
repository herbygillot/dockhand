package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/channel"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

// probe asks MacPorts Base in a guest what its toolchain is, the way Base
// itself asks: through its own variables and procedures in the parent and
// in a port worker. It writes one JSON object.
//
//go:embed probe.tcl
var probe []byte

// probed is one image's probe, as the 2026-09-23 driver wrote them, so
// generate reads the old harvests and the new alike.
type probed struct {
	Image   string          `json:"image"`
	Slug    string          `json:"slug"`
	Profile string          `json:"profile"`
	Date    string          `json:"date"`
	Host    map[string]any  `json:"host"`
	Output  string          `json:"probe_output"`
	Errors  []string        `json:"driver_errors"`
	Facts   json.RawMessage `json:"facts"`
}

// harvestTart probes dockhand's images, the base and Xcode images of every
// release when none are named, each through a clone it deletes after. The
// images themselves are only ever read.
func harvestTart(ctx context.Context, executable, out string, parallel int, images []string) error {
	client, err := tart.Client{Executable: executable}.Resolve()
	if err != nil {
		return err
	}
	machine := host.Machine{Client: client}
	keys, err := channel.DefaultKeys()
	if err != nil {
		return err
	}
	if len(images) == 0 {
		listed, err := machine.Images(ctx)
		if err != nil {
			return err
		}
		for _, image := range listed {
			if _, prepared := tart.ParsePrepared(image.Name); prepared {
				images = append(images, image.Name)
			}
		}
		sort.Strings(images)
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
	version, _ := machine.Version(ctx)
	jobs := make(chan string)
	var mu sync.Mutex
	var failed []error
	var wg sync.WaitGroup
	for range max(parallel, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for image := range jobs {
				err := probeImage(ctx, machine, keys, script.Name(), version, image, out)
				mu.Lock()
				if err != nil {
					failed = append(failed, fmt.Errorf("%s: %w", image, err))
					fmt.Fprintf(os.Stderr, "%s: %v\n", image, err)
				} else {
					fmt.Fprintf(os.Stderr, "%s: probed\n", image)
				}
				mu.Unlock()
			}
		}()
	}
	for _, image := range images {
		jobs <- image
	}
	close(jobs)
	wg.Wait()
	return errors.Join(failed...)
}

// probeImage clones an image, boots the clone, runs the probe over the
// channel, and stops and deletes the clone whatever happens.
func probeImage(ctx context.Context, machine host.Machine, keys channel.Keys, script, version, image, out string) (err error) {
	profile, slug, ok := strings.Cut(strings.TrimPrefix(image, "dockhand-"), "-")
	if !ok || (profile != "base" && profile != "xcode") {
		return fmt.Errorf("not one of dockhand's base or Xcode images")
	}
	vm := fmt.Sprintf("dockhand-facts-%s-%d", strings.TrimPrefix(image, "dockhand-"), os.Getpid())
	if err := machine.Clone(ctx, image, vm); err != nil {
		return err
	}
	cleanup := context.WithoutCancel(ctx)
	defer func() { err = errors.Join(err, machine.Delete(cleanup, vm)) }()
	run, err := machine.StartForeground(vm)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, run.Stop(cleanup, time.Minute)) }()
	address, err := machine.IP(ctx, vm, 300)
	if err != nil {
		return err
	}
	// Every clone presents its image's host keys, recorded at setup.
	guest := &channel.Guest{Address: address, Image: image, Keys: keys}
	defer guest.Close(cleanup)
	if err := channel.AwaitSSH(ctx, guest, run, 4*time.Minute, 5*time.Second); err != nil {
		return err
	}
	doc := probed{Image: image, Slug: slug, Profile: profile, Date: time.Now().UTC().Format(time.RFC3339), Host: map[string]any{"tart": version}}
	if err := guest.Upload(ctx, script, "/tmp/dockhand-facts-probe.tcl", false); err != nil {
		return err
	}
	output, err := guest.Command(ctx, nil, "/opt/local/bin/port-tclsh", "/tmp/dockhand-facts-probe.tcl", "/tmp/dockhand-facts.json")
	doc.Output = string(output)
	if err != nil {
		doc.Errors = append(doc.Errors, "probe: "+err.Error())
	}
	facts, err := guest.Read(ctx, "/tmp/dockhand-facts.json", false)
	if err != nil {
		return err
	}
	if !json.Valid(facts) {
		return fmt.Errorf("the probe wrote JSON that doesn't parse")
	}
	doc.Facts = facts
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, slug+"-"+profile+".json"), append(data, '\n'), 0o644)
}
