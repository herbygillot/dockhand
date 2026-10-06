package macos

import (
	"bytes"
	"encoding/xml"
	"sort"
	"strings"
)

func xmlString(s string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(s))
	return out.String()
}

// LaunchdJob is a launchd job as dockhand writes one: what it runs, with
// its environment, and the log its output and errors share. Every value is
// escaped, keys as well as strings.
type LaunchdJob struct {
	Label       string
	Arguments   []string
	Log         string
	Environment map[string]string
	// KeepAlive restarts the job whenever it ends; a one-shot job's is
	// false.
	KeepAlive bool
	// KeepAliveWhile, a path, narrows KeepAlive to while that path exists
	// (launchd.plist's PathState): an agent whose program is uninstalled
	// isn't restarted, which left launchd spawning a missing program in a
	// loop, exit 78 (the rc6 full stage, A8).
	KeepAliveWhile string
	// ProcessType is launchd's, such as Background; none where empty.
	ProcessType string
}

// LaunchdPlist describes a one-shot service with a shared stdout/stderr log.
func LaunchdPlist(label string, args []string, log string, env map[string]string) []byte {
	return LaunchdJob{Label: label, Arguments: args, Log: log, Environment: env}.Plist()
}

// Plist is the job's property list, the one way dockhand writes one.
func (j LaunchdJob) Plist() []byte {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + xmlString(j.Label) + `</string><key>ProgramArguments</key><array>`)
	for _, arg := range j.Arguments {
		out.WriteString("<string>" + xmlString(arg) + "</string>")
	}
	keepAlive := "<false/>"
	switch {
	case j.KeepAlive && j.KeepAliveWhile != "":
		keepAlive = "<dict><key>PathState</key><dict><key>" + xmlString(j.KeepAliveWhile) + "</key><true/></dict></dict>"
	case j.KeepAlive:
		keepAlive = "<true/>"
	}
	out.WriteString(`</array><key>RunAtLoad</key><true/><key>KeepAlive</key>` + keepAlive + `<key>AbandonProcessGroup</key><false/>`)
	if j.ProcessType != "" {
		out.WriteString(`<key>ProcessType</key><string>` + xmlString(j.ProcessType) + `</string>`)
	}
	out.WriteString(`<key>StandardOutPath</key><string>` + xmlString(j.Log) + `</string><key>StandardErrorPath</key><string>` + xmlString(j.Log) + `</string><key>EnvironmentVariables</key><dict>`)
	keys := make([]string, 0, len(j.Environment))
	for key := range j.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.WriteString("<key>" + xmlString(key) + "</key><string>" + xmlString(j.Environment[key]) + "</string>")
	}
	out.WriteString("</dict></dict></plist>")
	return []byte(out.String())
}
