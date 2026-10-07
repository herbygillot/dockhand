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
}

// TargetInputs are what a target's build read, as far as dockhand can name
// them (decision 28): the environment by its identity; the target's own
// directory and _resources, each by its tree; the variants it was asked
// for; the ports active as it built; and for a port fetched with Git, the
// commit it fetched. Until evaluation records what it sources, all of
// _resources counts as read.
type TargetInputs struct {
	Environment string
	Directory   string
	Tree        ObjectID
	Resources   ObjectID
	Variants    map[string]bool `json:",omitempty"`
	// Active are in name order: empty for a build that read no other
	// port, and nil where its provider couldn't say, as one that reports
	// only what a Git fetch checked out can't.
	Active []ActivePort
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
	if i.Environment == "" || i.Tree == "" || i.Resources == "" || i.Active == nil {
		return false
	}
	for _, port := range i.Active {
		if port.Tree == "" || port.Archive == "" {
			return false
		}
	}
	return true
}
