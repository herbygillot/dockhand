package model

import (
	"strings"
	"time"
)

// Archive is a port's archive, as a check's build made it, that dockhand
// keeps for a later build to install rather than build the port again
// (decision 28). It is kept once it is whole and matches the digest its
// build reported, and only then is it ready for dependents (decision 44).
type Archive struct {
	// Digest is the archive's sha256, sha256:<hex>, which names it.
	Digest string
	// Name is MacPorts' file name for it, which a guest's MacPorts looks
	// for: jq-1.8.1_0.darwin_25.arm64.tbz2.
	Name   string
	Size   int64
	KeptAt time.Time
}

// DependencyArchive is a kept archive (Archive) a guest installed a port
// it didn't build as a target from, for a later guest in the same
// environment to install rather than build: rust and cargo, built from
// source where MacPorts had no archive for the release yet (batch 90).
// The port is one the check's revision had as its base did; one the
// branch changed is a target, whose archive is its result's.
type DependencyArchive struct {
	Archive
	Port        string
	Environment Environment
}

// ValidArchiveName reports whether a name is one file's, as MacPorts names
// an archive: no directory, and nothing a path could make more of.
func ValidArchiveName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00\n")
}
