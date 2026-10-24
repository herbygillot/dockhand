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
	case first == Modeline:
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

// conflicts reports whether a modeline sets anything other than MacPorts'
// settings: another mode or encoding, tabs, or a width other than 4. One
// it doesn't set is no conflict.
func conflicts(line string) bool {
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
			key, value, _ := strings.Cut(part, "=")
			settings["vim."+strings.ToLower(key)] = strings.ToLower(value)
		}
	}
	want := map[string]string{
		"coding": "utf-8", "mode": "tcl", "tab-width": "4", "indent-tabs-mode": "nil", "c-basic-offset": "4",
		"vim.fenc": "utf-8", "vim.fileencoding": "utf-8", "vim.ft": "tcl", "vim.filetype": "tcl",
		"vim.sw": "4", "vim.shiftwidth": "4", "vim.ts": "4", "vim.tabstop": "4", "vim.sts": "4", "vim.softtabstop": "4",
	}
	for key, value := range settings {
		if expected, known := want[key]; known && value != expected {
			return true
		}
	}
	_, noet := settings["vim.noet"]
	_, noexpand := settings["vim.noexpandtab"]
	return noet || noexpand
}
