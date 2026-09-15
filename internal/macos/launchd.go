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

// LaunchdPlist describes a one-shot service with a shared stdout/stderr log.
func LaunchdPlist(label string, args []string, log string, env map[string]string) []byte {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + xmlString(label) + `</string><key>ProgramArguments</key><array>`)
	for _, arg := range args {
		out.WriteString("<string>" + xmlString(arg) + "</string>")
	}
	out.WriteString(`</array><key>RunAtLoad</key><true/><key>KeepAlive</key><false/><key>AbandonProcessGroup</key><false/><key>StandardOutPath</key><string>` + xmlString(log) + `</string><key>StandardErrorPath</key><string>` + xmlString(log) + `</string><key>EnvironmentVariables</key><dict>`)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := env[key]
		out.WriteString("<key>" + xmlString(key) + "</key><string>" + xmlString(value) + "</string>")
	}
	out.WriteString("</dict></dict></plist>")
	return []byte(out.String())
}
