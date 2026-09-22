package portedit

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
)

// archiveCoverage is the bookkeeping the archive passes share over the
// observed contexts: which checksum groups the contexts declare, which an
// archive covers, which a patch file present beside the Portfile makes
// inert, and the archives to download, each once by group, name, and
// locations. What each pass accepts stays with the pass: an assessment
// only reads, a checksum refresh downloads every archive, and a version
// edit downloads the changed ones and protects the rest.
type archiveCoverage struct {
	declared, covered, inert, unique map[string]bool
	downloads                        []plannedArchive
}

func newArchiveCoverage() *archiveCoverage {
	return &archiveCoverage{declared: map[string]bool{}, covered: map[string]bool{}, inert: map[string]bool{}, unique: map[string]bool{}}
}

// declare records the checksum groups one context's binding declares, and
// among them the ones a present patch file makes inert.
func (c *archiveCoverage) declare(info macports.PortInfo, groups []distfiles.Group) {
	for _, group := range groups {
		c.declared[group.ID()] = true
	}
	for id := range inertChecksumGroups(info, groups) {
		c.inert[id] = true
	}
}

// cover records that an artifact covers its checksum group.
func (c *archiveCoverage) cover(artifact distfiles.Artifact) {
	c.covered[artifact.Group.ID()] = true
}

// download plans an artifact's download once, however many contexts fetch
// the same file from the same locations.
func (c *archiveCoverage) download(artifact distfiles.Artifact, info macports.PortInfo) {
	key := artifact.Group.ID() + "\x00" + artifact.Name + "\x00" + strings.Join(artifact.URLs, "\x00")
	if c.unique[key] {
		return
	}
	c.unique[key] = true
	c.downloads = append(c.downloads, plannedArchive{artifact: artifact, info: info})
}

// uncovered names the declared checksum groups no archive covers and no
// patch file makes inert, in order.
func (c *archiveCoverage) uncovered() []string {
	var ids []string
	for id := range c.declared {
		if !c.covered[id] && !c.inert[id] {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
