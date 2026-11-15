package model

import (
	"encoding/json"
	"slices"
	"strings"
)

// ActivePort is a port that was active as a target built, other than the
// target itself: an input of its build (decision 28).
type ActivePort struct {
	Name string
	// Spec is the port's version, revision, and variants, as MacPorts
	// names an installed port: @1.88.0_3+no_single.
	Spec string
	// Directory is where the port's name resolved in the ports tree the
	// build read, relative to its root; empty where MacPorts couldn't
	// say.
	Directory string `json:",omitempty"`
	// Tree is that directory's tree in the revision the build read.
	Tree ObjectID `json:",omitempty"`
	// Archive is the digest of the archive the port was activated from,
	// sha256:<hex>; empty where the environment kept none.
	Archive string `json:",omitempty"`
	// PortGroups are the PortGroup files the port's build loaded, each by
	// its blob ID, by its path in the ports tree, where the environment
	// built it from source for the check, an earlier target or a
	// dependency it had no archive of: the target read what that build
	// made, so a change to one of them is a change to what it read
	// (prioritization's ruling, 2026-11-01). A port installed from an
	// archive dockhand kept of such a build carries that build's, as
	// recorded with the archive or its result's inputs (Codex's review of
	// 03b380f1, F1). Nil for a port installed from MacPorts' archive, or
	// from a kept one whose build's are unknown, which its archive names,
	// and in every record before, so each keeps its key.
	PortGroups map[string]ObjectID `json:",omitempty"`
}

// TargetInputs are what a target's build read, as far as dockhand can name
// them (decision 28): the environment by its identity; the target's own
// directory by its tree; _resources, by its tree or by what of it the
// build read (ResourcesRead); the variants it was asked for; the ports
// active as it built; and for a port fetched with Git, the commit it
// fetched.
type TargetInputs struct {
	Environment string
	Directory   string
	Tree        ObjectID
	// Resources is _resources' tree, where the build is taken to have read
	// all of it: its provider didn't say which PortGroups it loaded, or it
	// was recorded before any provider said. Empty where ResourcesRead
	// names what it read instead.
	Resources ObjectID
	// ResourcesRead is what of _resources the build read, where its
	// provider said which PortGroups it loaded: a result then lasts until
	// one of those or a file every port reads changes, where a change to
	// any PortGroup voided it (the oracle survey's Q2). Left out when nil,
	// so a key recorded before it is the same key still, and holds as a
	// stricter one.
	ResourcesRead *ResourcesRead  `json:",omitempty"`
	Variants      map[string]bool `json:",omitempty"`
	// Active are in name order: empty for a build that read no other
	// port, and nil where its provider couldn't say, as one that reports
	// only what a Git fetch checked out can't.
	Active []ActivePort
	// Targets names the build ID of each in-check port consumed, by its
	// lower-case port name. Older inputs omit it; builds with in-check
	// dependencies gain a new key once their consumption is recorded (#236).
	Targets map[string]TargetID `json:",omitempty"`
	// Fetched is the commit a Git-fetched target's build checked out, as
	// its provider reported it (batch 20): the source it built, which its
	// Portfile's tag doesn't bind. It is empty for a port fetched
	// otherwise, and where the provider couldn't say, which the plan the
	// build was checked by tells apart (EnvironmentPlan.Git). Left out when
	// empty, so a key recorded before it is the same key still.
	Fetched ObjectID `json:",omitempty"`
	// NotReusable is why no later build can reuse this one, where what it
	// read couldn't be recorded, kept so a later check that builds it
	// again can say why, where it rebuilt for an hour in silence (the
	// architecture review's L3a). Left out when empty.
	NotReusable string `json:",omitempty"`
}

// ResourcesRead is what of _resources a build read: what every port's
// build may read there, and the PortGroups it loaded, each by its object
// in the revision it read, by its path in the ports tree.
type ResourcesRead struct {
	// Shared are the entries of _resources beside the PortGroups, each a
	// tree or a file: the compiler lists, the fetch and livecheck
	// definitions, the SDK lists, the variant descriptions, which MacPorts
	// itself reads for any port. An entry added or gone since is a change.
	Shared map[string]ObjectID
	// PortGroups are the PortGroup files the build loaded, as the guest
	// that built it recorded them, not as the host's evaluation did, each
	// by its blob; empty where it loaded none.
	PortGroups map[string]ObjectID
}

// NewTargetInputs gathers a build's inputs, the active ports in name
// order, so the same inputs have one form.
func NewTargetInputs(environment, directory string, tree, resources ObjectID, variants map[string]bool, active []ActivePort) TargetInputs {
	active = slices.Clone(active)
	slices.SortFunc(active, func(a, b ActivePort) int { return strings.Compare(a.Name, b.Name) })
	return TargetInputs{Environment: environment, Directory: directory, Tree: tree, Resources: resources, Variants: variants, Active: active}
}

// Key identifies inputs by their content.
func (i TargetInputs) Key() string {
	data, err := json.Marshal(i)
	if err != nil {
		panic(err) // plain strings and maps always marshal
	}
	return "sha256:" + Digest(data)
}

// Complete reports whether every input is identified by content: the
// environment, the target's directory and _resources, and each active
// port's directory and archive, which are known. Only complete inputs can
// stand for another build's.
func (i TargetInputs) Complete() bool {
	if i.Environment == "" || i.Tree == "" || i.Active == nil {
		return false
	}
	if i.ResourcesRead == nil && i.Resources == "" || i.ResourcesRead != nil && (i.ResourcesRead.Shared == nil || i.ResourcesRead.PortGroups == nil) {
		return false
	}
	for _, port := range i.Active {
		if port.Tree == "" || port.Archive == "" {
			return false
		}
	}
	return true
}
