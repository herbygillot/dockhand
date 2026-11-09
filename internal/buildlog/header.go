package buildlog

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Header is the header MacPorts Base writes at the top of a port's log
// when it runs with -d, as mpbb's install-port does on the buildbot and in
// MacPorts' CI workflow:
//
//	DEBUG: macOS 26.7 (darwin/25.6.0) arch i386
//	DEBUG: MacPorts 2.12.6
//	DEBUG: Xcode 26.6, CLT 26.6.0.0.1781586589
//	DEBUG: SDK 26
//
// A field is empty where its line wasn't there. Xcode and Tools are
// "none" where Base found none.
type Header struct {
	MacOS string `json:"macos"`
	// Darwin is the kernel's major version, 25.
	Darwin int `json:"darwin"`
	// Arch is Base's os.arch, "arm" or "i386" (Architecture).
	Arch     string `json:"arch,omitempty"`
	MacPorts string `json:"macports"`
	Xcode    string `json:"xcode"`
	Tools    string `json:"tools"`
	SDK      string `json:"sdk"`
}

var (
	// Base names the release as Apple did when it shipped: Mac OS X 10.6,
	// OS X 10.11, macOS 10.12 and on.
	headerMacOS    = regexp.MustCompile(`^DEBUG: (?:macOS|OS X|Mac OS X) (\S+) \(darwin/(\d+)\.\S*\)(?: arch (\S+))?`)
	headerMacPorts = regexp.MustCompile(`^DEBUG: MacPorts (\S+)$`)
	headerXcode    = regexp.MustCompile(`^DEBUG: Xcode (\S+), CLT (\S+)$`)
	headerSDK      = regexp.MustCompile(`^DEBUG: SDK (\S+)$`)
)

// ReadHeader reads a log up to its first complete header, and reports
// whether it found one; where it found none, it returns what lines of one
// it read. A GitHub job's log is read untimed (cilog.Untimed).
func ReadHeader(log io.Reader) (Header, bool) {
	var h Header
	scanner := bufio.NewScanner(log)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if m := headerMacOS.FindStringSubmatch(line); m != nil {
			h.MacOS, h.Arch = m[1], m[3]
			h.Darwin, _ = strconv.Atoi(m[2])
		} else if m := headerMacPorts.FindStringSubmatch(line); m != nil {
			h.MacPorts = m[1]
		} else if m := headerXcode.FindStringSubmatch(line); m != nil {
			h.Xcode, h.Tools = m[1], m[2]
		} else if m := headerSDK.FindStringSubmatch(line); m != nil {
			h.SDK = m[1]
		}
		if h.Darwin != 0 && h.MacPorts != "" && h.Xcode != "" && h.SDK != "" {
			return h, true
		}
	}
	return h, false
}

// Architecture is the machine's architecture as uname -m names it, from
// Base's os.arch, which names the processor family: arm on Apple silicon
// is arm64, and i386 on Intel is x86_64, as every Intel Mac since OS X
// 10.7 runs; any other is as Base said it. Empty where the header didn't
// say.
func (h Header) Architecture() string {
	switch h.Arch {
	case "arm":
		return "arm64"
	case "i386":
		return "x86_64"
	}
	return h.Arch
}

// DeveloperTools are Xcode's version and the Command Line Tools', each
// empty where Base found none or the header didn't say.
func (h Header) DeveloperTools() (xcode, tools string) {
	stated := func(version string) string {
		if version == "none" {
			return ""
		}
		return version
	}
	return stated(h.Xcode), stated(h.Tools)
}
