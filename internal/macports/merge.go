package macports

import (
	"slices"
	"strings"
	"time"
)

// MaintainersOption is the option a port names its maintainers in, as
// MacPorts' port information carries it.
const MaintainersOption = "maintainers"

// MaintainerTimeout is how long a pull request for a maintained port waits
// for its maintainer before others may merge it. The MacPorts Guide's
// "Non-Maintainer Port Updates" (section 7.4.1, guide.macports.org) says
// pull requests for maintained ports should not be merged by anyone other
// than their creator or the port maintainer until the 72-hour timeout
// period has passed, even if the port is openmaintainer; and that where
// the maintainer does not respond within 72 hours, another committer may
// review the change and update the port. A nomaintainer port may be
// updated by anyone, the same section says, so it doesn't wait; and a
// maintainer's approval is their response, which ends the wait, as one of
// them opening it does. What the timeout holds is a merge by others: the
// creator's own merge, where they may merge at all, is theirs to judge.
const MaintainerTimeout = 72 * time.Hour

// PortMaintainers are a port's maintainers, as its evaluated maintainers
// option lists them (ReadMaintainers).
type PortMaintainers struct {
	Port        string
	Maintainers []Maintainer
}

// PortMerge is how one port of a pull request stands for its merge:
// the people who maintain it, keywords apart; whether it's openmaintainer
// or nomaintainer; which of its maintainers approved the pull request; and
// whether one of them opened it.
type PortMerge struct {
	Port string
	// People are its maintainers who name someone, each as a pull
	// request names them, their GitHub handle where one is given.
	People                       []string
	OpenMaintainer, NoMaintainer bool
	ApprovedBy                   []string
	ByMaintainer                 bool
}

// Waits reports whether the port waits on the maintainer timeout: someone
// maintains it, and none of them opened the pull request or approved it.
func (p PortMerge) Waits() bool {
	return len(p.People) > 0 && !p.ByMaintainer && len(p.ApprovedBy) == 0
}

// Merge is a pull request's fitness to be merged by MacPorts' maintainer
// policy: each of its ports, who its description notified, who approved
// it, and when the maintainer timeout passes, from its opening.
type Merge struct {
	Ports []PortMerge
	// Opened is when the pull request was opened, which the timeout
	// counts from: the maintainers are notified as it opens, by its
	// description's mentions or MacPorts' bot's, and a mention's own time
	// would take its description's history, requests status doesn't make.
	Opened time.Time
	// Notified are the logins its description mentions, and Approvers
	// every reviewer whose latest review approves it.
	Notified, Approvers []string
	// Until is when the maintainer timeout passes, zero where no port
	// waits on it; Left is how long until then, as of the reading, zero
	// once it's passed.
	Until time.Time
	Left  time.Duration
}

// Waiting are the ports that wait on the maintainer timeout.
func (m Merge) Waiting() []string {
	var ports []string
	for _, port := range m.Ports {
		if port.Waits() {
			ports = append(ports, port.Port)
		}
	}
	return ports
}

// Passed reports whether a port waits on the timeout and it has passed.
func (m Merge) Passed() bool { return !m.Until.IsZero() && m.Left == 0 }

// MergeFitness is a pull request's Merge, as of now: its ports'
// maintainers, who opened it, who its description mentions, who approved
// it, and when it was opened (MaintainerTimeout). A maintainer is matched
// to a login by their GitHub handle (Maintainer.Names); one named by an
// address alone can't be, and their approval isn't seen. Where no port
// waits, or it's unknown when the pull request was opened, there's no
// time to give.
func MergeFitness(ports []PortMaintainers, author string, notified, approvers []string, opened, now time.Time) Merge {
	merge := Merge{Opened: opened, Notified: notified, Approvers: approvers}
	names := func(m Maintainer, login string) bool { return login != "" && m.Names("@"+login) }
	for _, port := range ports {
		standing := PortMerge{Port: port.Port}
		for _, maintainer := range port.Maintainers {
			switch {
			case len(maintainer) == 1 && maintainer[0] == OpenMaintainer:
				standing.OpenMaintainer = true
			case len(maintainer) == 1 && maintainer[0] == NoMaintainer:
				standing.NoMaintainer = true
			default:
				standing.People = append(standing.People, maintainer.Handle())
				standing.ByMaintainer = standing.ByMaintainer || names(maintainer, author)
				for _, login := range approvers {
					if names(maintainer, login) && !slices.Contains(standing.ApprovedBy, "@"+login) {
						standing.ApprovedBy = append(standing.ApprovedBy, "@"+login)
					}
				}
			}
		}
		merge.Ports = append(merge.Ports, standing)
	}
	if len(merge.Waiting()) > 0 && !opened.IsZero() {
		merge.Until = opened.Add(MaintainerTimeout)
		merge.Left = max(merge.Until.Sub(now), 0)
	}
	return merge
}

// Handle is how a pull request names the maintainer: their GitHub handle,
// @ada, where one of their spellings is one, else their first spelling.
func (m Maintainer) Handle() string {
	for _, spelling := range m {
		if identity := MaintainerIdentity(spelling); strings.HasPrefix(identity, "@") {
			return "@" + strings.TrimPrefix(strings.TrimSuffix(spelling, "@github"), "@")
		}
	}
	return m[0]
}
