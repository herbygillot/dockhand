package portfile

import (
	"regexp"
	"strings"
)

// Modeline is the first line of every Portfile in MacPorts' tree, as `port
// lint --nitpick` expects it.
const Modeline = "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4"

// WithModeline is a Portfile's text with MacPorts' modeline as its first
// line: added where line 1 is no modeline, and replacing one whose
// settings are the same spelled otherwise, as `vim:fenc=utf-8:filetype=tcl`
// or one missing `mode: tcl`. A modeline that sets others, tabs or another
// width, is the maintainer's, since the file's indentation follows it, and
// stays (the person's ask, 2026-10-24). changed is false where the text
// was left as it was.
func WithModeline(src []byte) (_ []byte, changed bool) {
	text := string(src)
	first, rest, _ := strings.Cut(text, "\n")
	switch {
	case isModeline(first) && settingsMatch(first):
		return src, false
	case isModeline(first) && conflicts(first):
		return src, false
	case isModeline(first):
		return []byte(Modeline + "\n" + rest), true
	case strings.TrimSpace(first) == "":
		return []byte(Modeline + "\n" + text), true
	}
	return []byte(Modeline + "\n\n" + text), true
}

var (
	emacsLine = regexp.MustCompile(`-\*-(.*?)-\*-`)
	vimLine   = regexp.MustCompile(`(?:^|\s)(?:vim?|ex):\s*(?:set?\s+)?(.*)$`)
)

// isModeline reports whether a line is a comment that sets an editor's
// modes, Emacs's or Vim's.
func isModeline(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#") && (emacsLine.MatchString(line) || vimLine.MatchString(line))
}

// conflicts reports whether a modeline sets any MacPorts option to a
// different value. One it doesn't set is no conflict.
func conflicts(line string) bool {
	settings := readSettings(line)
	for key, expected := range expectedSettings {
		if value, exists := settings[key]; exists && value != expected {
			return true
		}
	}
	return false
}

var expectedSettings = map[string]string{
	"coding": "utf-8", "mode": "tcl", "tab-width": "4", "indent-tabs-mode": "nil", "c-basic-offset": "4",
	"vim.fileencoding": "utf-8", "vim.filetype": "tcl", "vim.expandtab": "true", "vim.shiftwidth": "4", "vim.tabstop": "4", "vim.softtabstop": "4",
}

// settingsMatch reports whether both halves of the modeline set exactly the
// options MacPorts asks for, independent of spelling and order.
func settingsMatch(line string) bool {
	settings := readSettings(line)
	if len(settings) != len(expectedSettings) {
		return false
	}
	for key, want := range expectedSettings {
		if settings[key] != want {
			return false
		}
	}
	return true
}

// readSettings reads the key/value pairs in an Emacs modeline and the options
// in either Vim modeline form into canonical option names and values.
func readSettings(line string) map[string]string {
	settings := map[string]string{}
	if m := emacsLine.FindStringSubmatch(line); m != nil {
		for _, part := range strings.Split(m[1], ";") {
			key, value, ok := strings.Cut(part, ":")
			if ok {
				settings[strings.ToLower(strings.TrimSpace(key))] = strings.ToLower(strings.TrimSpace(value))
			}
		}
	}
	if m := vimLine.FindStringSubmatch(line); m != nil {
		for _, part := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ':' || r == ' ' || r == '\t' }) {
			key, value, hasValue := strings.Cut(part, "=")
			key = strings.ToLower(key)
			canonical, known := vimOptionNames[key]
			if !known {
				canonical = key
			}
			if !hasValue {
				switch key {
				case "et", "expandtab":
					value = "true"
				case "noet", "noexpandtab":
					canonical, value = "expandtab", "false"
				default:
					value = "true"
				}
			}
			settings["vim."+canonical] = strings.ToLower(value)
		}
	}
	return settings
}

var vimOptionNames = map[string]string{
	"fenc": "fileencoding", "fileencoding": "fileencoding",
	"ft": "filetype", "filetype": "filetype",
	"et": "expandtab", "expandtab": "expandtab",
	"sw": "shiftwidth", "shiftwidth": "shiftwidth",
	"ts": "tabstop", "tabstop": "tabstop",
	"sts": "softtabstop", "softtabstop": "softtabstop",
}
