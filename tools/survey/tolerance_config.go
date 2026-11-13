package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
)

type measurementConfig struct {
	Version                                                                              int
	Baseline, Policy, BinarySHA, CodeCommit, BaseVersion, BaseSources, Prefix, Selection string
}

func measurementIdentity(selection, prefix string) (measurementConfig, error) {
	var c measurementConfig
	exe, err := os.Executable()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return c, err
	}
	c = measurementConfig{Version: toleranceVersion, Baseline: "a0298da0", Policy: "normal-vs-no-follower", BinarySHA: digest(data), Prefix: prefix, Selection: selection, BaseVersion: baseVersion(filepath.Join(prefix, "bin", "port-tclsh"))}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				c.CodeCommit = s.Value
			}
		}
	}
	files, err := filepath.Glob(filepath.Join(prefix, "libexec/macports/lib/port1.0/*.tcl"))
	if err != nil {
		return c, err
	}
	if len(files) == 0 {
		return c, fmt.Errorf("measurement needs the selected Base's port1.0 sources")
	}
	var base []byte
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return c, err
		}
		base = append(base, []byte(filepath.Base(f)+"\x00")...)
		base = append(base, b...)
	}
	c.BaseSources = digest(base)
	return c, nil
}
func (c measurementConfig) key() string { data, _ := json.Marshal(c); return string(data) }
