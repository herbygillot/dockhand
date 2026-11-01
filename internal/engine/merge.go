package engine

import (
	"context"
	"path"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/model"
)

// readMerge reads an open pull request's fitness to be merged, as of now:
// the maintainers of the ports it changes, and its opening, author,
// mentions, and approvals, as its last reading has them
// (macports.MergeFitness). Status reads nothing of GitHub for it: the
// pull request's reading is the one status already makes. A port's
// maintainers are what its change record kept of the base's evaluation,
// and where no record keeps them, as master's cached port index has them
// (indexedMaintainers), so a port master has, an adopted pull request's
// included, shows them before any record is made; a port neither has, a
// new port until its record is made, is Unread, and said so rather than
// guessed.
func (e *Engine) readMerge(ctx context.Context, status *BranchStatus, reads *statusReads) {
	pr := status.Branch.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.State != model.PullRequestOpen || status.Branch.State != model.BranchOpen || status.Missing {
		return
	}
	// Each port changed, in the order said, with its recorded maintainers
	// where its record keeps them.
	var wanted []indexedPort
	recorded := map[indexedPort]string{}
	for _, directory := range status.Scope.Ports {
		// The subports its record says it changes (model.ChangeRecord.Scope),
		// or, where it says none it sees, every one, as its text scope is;
		// with no record to say, the port the directory is named for.
		record, hasRecord := status.Changes[directory]
		changed, ok := changedSubports(status.Changes, directory)
		if !ok && hasRecord && record.Problem == "" {
			for _, port := range record.Ports {
				changed = append(changed, port.Port)
			}
			ok = len(changed) > 0
		}
		if !ok {
			wanted = append(wanted, indexedPort{Port: path.Base(directory), Directory: directory})
			continue
		}
		for _, port := range changed {
			want := indexedPort{Port: port, Directory: directory}
			wanted = append(wanted, want)
			if i := slices.IndexFunc(record.Ports, func(c model.SubportChange) bool { return c.Port == port }); i >= 0 && record.Ports[i].Maintainers != "" {
				recorded[want] = record.Ports[i].Maintainers
			}
		}
	}
	var missing []indexedPort
	for _, want := range wanted {
		if _, ok := recorded[want]; !ok {
			missing = append(missing, want)
		}
	}
	indexed := e.indexedMaintainers(ctx, reads, missing)
	var ports []macports.PortMaintainers
	for _, want := range wanted {
		words, ok := recorded[want]
		if !ok {
			words, ok = indexed[want]
		}
		maintainers, err := macports.ReadMaintainers(words)
		if !ok || err != nil {
			status.MaintainersUnread = append(status.MaintainersUnread, want.Port)
			continue
		}
		ports = append(ports, macports.PortMaintainers{Port: want.Port, Maintainers: maintainers})
	}
	// A draft is one as dockhand made it or last read it (SubmitPlan.WasDraft).
	observed := pr.Observed
	merge := macports.MergeFitness(ports, observed.Author, observed.Mentioned, observed.ApprovedBy, observed.Opened, pr.Draft || observed.Draft, e.now())
	status.Merge = &merge
}

// indexedPort is a port as master's port index is asked for it: its name,
// and the directory it must be indexed from, so a port of the same name
// elsewhere isn't taken for it.
type indexedPort struct{ Port, Directory string }

// masterIndex is what a status read of master's cached port index: the
// tree of the generation it reads, empty where none is cached, and each
// port asked for, with its maintainers where the index has it.
type masterIndex struct {
	tree        string
	asked       map[indexedPort]bool
	maintainers map[indexedPort]string
}

// masterIndexDepth is how far back along master's first parents status
// looks for a cached generation: macports-ports' master took 5,818
// first-parent commits from August to October 2026, so some eleven weeks
// of it, past which a cached generation says less of the maintainers
// MacPorts has now than a change record would.
const masterIndexDepth = 5000

// indexedMaintainers are ports' maintainers as master's port index has
// them: the newest generation cached for master's tree or one of its
// first-parent ancestors', in any indexing environment, since a port's
// maintainers don't differ by platform. Status only looks at the cache:
// it builds no generation and fetches none, so where none is cached, or
// the index doesn't have a port from its directory, the port has none
// here. Read once a status, each port asked for once.
func (e *Engine) indexedMaintainers(ctx context.Context, reads *statusReads, ports []indexedPort) map[indexedPort]string {
	if len(ports) == 0 {
		return nil
	}
	if reads.indexed == nil {
		reads.indexed = &masterIndex{asked: map[indexedPort]bool{}, maintainers: map[indexedPort]string{}}
		reads.indexed.tree = e.cachedMasterTree(ctx, reads.master)
	}
	known := reads.indexed
	var ask []indexedPort
	for _, port := range ports {
		if !known.asked[port] {
			known.asked[port] = true
			ask = append(ask, port)
		}
	}
	if known.tree != "" && len(ask) > 0 {
		cache, err := e.indexCache()
		if err == nil {
			// An index that can't be read leaves its ports unread, which
			// status says, rather than failing the status.
			_, _ = portindex.ReadCached(ctx, cache, known.tree, func(index *portindex.Index) error {
				for _, port := range ask {
					entry, err := index.Lookup(port.Port)
					if err == nil && entry.Portdir == port.Directory {
						known.maintainers[port] = entry.Fields[macports.MaintainersOption]
					}
				}
				return nil
			})
		}
	}
	found := map[indexedPort]string{}
	for _, port := range ports {
		if words, ok := known.maintainers[port]; ok {
			found[port] = words
		}
	}
	return found
}

// cachedMasterTree is the tree of the newest generation cached for master
// or a first-parent ancestor of it, as far back as masterIndexDepth; empty
// where none is, or there's no master.
func (e *Engine) cachedMasterTree(ctx context.Context, master model.ObjectID) string {
	cache, err := e.indexCache()
	if err != nil || master == "" {
		return ""
	}
	cached, err := portindex.CachedTrees(cache)
	if err != nil || len(cached) == 0 {
		return ""
	}
	trees, err := e.Repo.FirstParentTrees(ctx, string(master), masterIndexDepth)
	if err != nil {
		return ""
	}
	for _, tree := range trees {
		if cached[tree] {
			return tree
		}
	}
	return ""
}
