package engine

import (
	"path"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// readMerge reads an open pull request's fitness to be merged, as of now:
// the maintainers of the ports it changes, from their change records, and
// its opening, author, mentions, and approvals, as its last reading has
// them (macports.MergeFitness). Status reads nothing for it: the pull
// request's reading is the one status already makes, and the maintainers
// are what its change records kept of the ports' evaluation. A port whose
// record keeps no maintainers, made before they were kept or not made, is
// Unread, and said so rather than guessed.
func (e *Engine) readMerge(status *BranchStatus) {
	pr := status.Branch.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.State != model.PullRequestOpen || status.Branch.State != model.BranchOpen || status.Missing {
		return
	}
	var ports []macports.PortMaintainers
	for _, directory := range status.Scope.Ports {
		// The subports its record says it changes (model.ChangeRecord.Scope),
		// or, where it says none it sees, every one, as its text scope is.
		record, recorded := status.Changes[directory]
		changed, ok := changedSubports(status.Changes, directory)
		if !ok && recorded && record.Problem == "" {
			for _, port := range record.Ports {
				changed = append(changed, port.Port)
			}
			ok = len(changed) > 0
		}
		if !ok {
			status.MaintainersUnread = append(status.MaintainersUnread, path.Base(directory))
			continue
		}
		for _, port := range changed {
			i := slices.IndexFunc(record.Ports, func(c model.SubportChange) bool { return c.Port == port })
			if i < 0 || record.Ports[i].Maintainers == "" {
				status.MaintainersUnread = append(status.MaintainersUnread, port)
				continue
			}
			maintainers, err := macports.ReadMaintainers(record.Ports[i].Maintainers)
			if err != nil {
				status.MaintainersUnread = append(status.MaintainersUnread, port)
				continue
			}
			ports = append(ports, macports.PortMaintainers{Port: port, Maintainers: maintainers})
		}
	}
	observed := pr.Observed
	merge := macports.MergeFitness(ports, observed.Author, observed.Mentioned, observed.ApprovedBy, observed.Opened, e.now())
	status.Merge = &merge
}
