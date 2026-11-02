package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/prose"
)

// mergeRowWords are what a branch's row says of its pull request's merge,
// after its review and CI: that it's a draft, which no one merges; else
// when MacPorts' maintainer timeout ends, or that it has, where a port
// waits on it; nothing otherwise.
func mergeRowWords(s engine.BranchStatus) string {
	merge := s.Merge
	switch {
	case merge == nil:
		return ""
	case merge.Draft:
		return ", draft"
	case merge.Until.IsZero():
		return ""
	case merge.Passed():
		return ", maintainer timeout passed"
	}
	return ", maintainer timeout ends in " + leftWords(merge.Left)
}

// leftWords are how long until a time, in whole hours, or minutes within
// the hour: "41h", "25m".
func leftWords(left time.Duration) string {
	if left >= time.Hour {
		return fmt.Sprintf("%dh", int(left.Hours()))
	}
	return fmt.Sprintf("%dm", max(int(left.Minutes()), 1))
}

// MergeLines are what status details of an open pull request's merge,
// a line each: each port's maintainers and how it stands, whom the
// description mentions, who approved it, and when MacPorts' 72-hour
// maintainer timeout ends, or that it's a draft; none where there's no
// open pull request. Who was notified is said as what was read, the
// description's mentions, never that a maintainer wasn't: MacPorts' bot
// notifies them as it opens, in a comment that isn't read.
func MergeLines(s engine.BranchStatus) []string {
	merge := s.Merge
	if merge == nil {
		return nil
	}
	var lines []string
	for _, port := range merge.Ports {
		line := port.Port + ": " + portMergeWords(port)
		if basis := basisWords(port); basis != "" {
			line += "; " + basis
		}
		lines = append(lines, line)
	}
	if len(s.MaintainersUnread) > 0 {
		lines = append(lines, unreadWords(s))
	}
	if len(merge.Mentioned) > 0 {
		lines = append(lines, "Mentioned in the description: "+prose.And(handles(merge.Mentioned)))
	} else {
		lines = append(lines, "No one is mentioned in the description")
	}
	if len(merge.Approvers) > 0 {
		lines = append(lines, "Approved by "+prose.And(handles(merge.Approvers)))
	}
	switch {
	case merge.Draft && len(merge.Waiting()) > 0:
		lines = append(lines, fmt.Sprintf("A draft, so no one merges it yet; once it's ready, MacPorts' %d-hour maintainer timeout is counted from its opening", hours()))
	case merge.Draft:
		lines = append(lines, "A draft, so no one merges it yet")
	case !merge.Until.IsZero() && merge.Passed():
		lines = append(lines, fmt.Sprintf("MacPorts' %d-hour maintainer timeout ended %s, so another committer may merge it", hours(), timeWords(merge.Until)))
	case !merge.Until.IsZero():
		lines = append(lines, fmt.Sprintf("MacPorts' %d-hour maintainer timeout ends %s, in %s; until then only its maintainers or its author should merge it", hours(), timeWords(merge.Until), leftWords(merge.Left)))
	case len(merge.Waiting()) > 0:
		lines = append(lines, fmt.Sprintf("MacPorts' %d-hour maintainer timeout counts from its opening, not read yet (dockhand status --refresh)", hours()))
	case len(s.MaintainersUnread) == 0:
		lines = append(lines, "No port waits on MacPorts' maintainer timeout")
	}
	return lines
}

// unreadWords say the ports whose maintainers aren't read: neither a
// change record nor master's cached port index has them, as a new port's
// aren't until its record is made. For the person's own branch the line
// names what reads them; an adopted branch's Portfiles are another's,
// which nothing evaluates unattended (engine.OthersPortfiles), so there
// it names nothing.
func unreadWords(s engine.BranchStatus) string {
	words := prose.And(s.MaintainersUnread) + ": maintainers not read yet"
	if _, others := engine.OthersPortfiles(s.Branch); others {
		return words
	}
	return words + "; dockhand check --plan reads them"
}

// portMergeWords are a port's maintainers and how it stands for the merge:
// "@ryandesign, openmaintainer; waits on the maintainer timeout".
func portMergeWords(port macports.PortMerge) string {
	named := append([]string{}, port.People...)
	if port.OpenMaintainer {
		named = append(named, macports.OpenMaintainer)
	}
	if port.NoMaintainer {
		named = append(named, macports.NoMaintainer)
	}
	words := strings.Join(named, ", ")
	switch {
	case port.Waits():
		return words + "; waits on the maintainer timeout"
	case port.ByMaintainer:
		return words + "; opened by its maintainer"
	case len(port.ApprovedBy) > 0:
		return words + "; approved by " + prose.And(port.ApprovedBy)
	case port.NoMaintainer:
		return words + "; anyone may update it"
	}
	return words + "; no one named to wait for"
}

// basisWords name the reading a port's maintainers were taken from where
// it isn't master's newest cached port index: "maintainers as of master,
// 2026-10-28" for the branch's base, newer than any cached index or with
// none cached, and "maintainers as the branch adds it" for a new port.
func basisWords(port macports.PortMerge) string {
	switch {
	case port.Basis == macports.MaintainersOfBase && !port.As.IsZero():
		return "maintainers as of master, " + port.As.Local().Format(time.DateOnly)
	case port.Basis == macports.MaintainersOfBase:
		return "maintainers as of the branch's base"
	case port.Basis == macports.MaintainersOfNewPort:
		return "maintainers as the branch adds it"
	}
	return ""
}

// handles are logins as a pull request mentions them, @ada.
func handles(logins []string) []string {
	var named []string
	for _, login := range logins {
		named = append(named, "@"+login)
	}
	return named
}

// hours is the maintainer timeout in hours, 72.
func hours() int { return int(macports.MaintainerTimeout.Hours()) }

// timeWords are a time as status says one ahead or just past: its day and
// time in the person's zone.
func timeWords(at time.Time) string { return at.Local().Format("Mon 2 Jan 15:04 MST") }
