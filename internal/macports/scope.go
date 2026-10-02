package macports

import (
	"fmt"
	"slices"
)

// RebindReleaseScope preserves the accepted target set after corrective edits.
// Corrections may change affected source metadata, but may not silently enlarge
// the release or alter a protected sibling's source identity.
func RebindReleaseScope(scope *ReleaseScope, snapshot Snapshot) (*ReleaseScope, error) {
	if scope == nil {
		return nil, nil
	}
	updated := *scope
	updated.Affected = slices.Clone(scope.Affected)
	updated.Protected = slices.Clone(scope.Protected)
	if len(snapshot.Ports) != len(scope.Affected)+len(scope.Protected) {
		return nil, fmt.Errorf("macports: shared-release target set changed; reassess the contribution")
	}
	for i, member := range updated.Affected {
		info, ok := snapshot.Ports[member.Target.Name]
		if only, err := info.MetadataOnly(); !ok || err != nil || only != member.MetadataOnly {
			return nil, fmt.Errorf("macports: shared-release target %s changed identity", member.Target.Name)
		}
		// Reverification can cover implementation fixes, not a new release hidden
		// inside an amend/reassociate operation.
		if info.Version != member.After.Version || info.Epoch != member.After.Epoch {
			return nil, fmt.Errorf("macports: shared-release version changed for %s; start a new bump", member.Target.Name)
		}
		updated.Affected[i].After = ReleaseStateOf(info)
		needsXcode, err := info.Bool("use_xcode")
		if err != nil {
			return nil, err
		}
		updated.Affected[i].NeedsXcode = needsXcode
	}
	for _, member := range updated.Protected {
		info, ok := snapshot.Ports[member.Target.Name]
		if !ok || ReleaseStateOf(info) != member.After {
			return nil, fmt.Errorf("macports: protected sibling %s changed source identity", member.Target.Name)
		}
	}
	return &updated, nil
}

// ReleaseStateOf records the evaluated version and archive identity for a
// scope member.
func ReleaseStateOf(p PortInfo) ReleaseState {
	return ReleaseState{MasterSites: p.Options["master_sites"], Worksrcdir: p.Options["worksrcdir"], Epoch: p.Epoch, Version: p.Version, Revision: p.Revision, Tag: p.Options["git.branch"], Distfiles: p.Options["distfiles"], Checksums: p.Options["checksums"]}
}

// SameSource reports whether a port fetches and patches the same source
// before and after a change: its version, epoch, sites, distfiles,
// checksums, Git tag, source directory, and patch files, whatever its
// revision. Upstream changed nothing of such a port, as nothing changed
// of terraform-1.15 where terraform-1.16 was updated beside it (field
// testing, 2026-10-02).
func SameSource(before, after PortInfo) bool {
	a, b := ReleaseStateOf(before), ReleaseStateOf(after)
	a.Revision, b.Revision = 0, 0
	return a == b && before.Options["patchfiles"] == after.Options["patchfiles"]
}
