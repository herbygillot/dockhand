package macports

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/record"
)

// RebindReleaseScope preserves the accepted target set after corrective edits.
// Corrections may change affected source metadata, but may not silently enlarge
// the release or alter a protected sibling's source identity.
func RebindReleaseScope(scope *record.ReleaseScope, snapshot Snapshot) (*record.ReleaseScope, error) {
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
		if !ok || (info.Options["dockhand.metadata_only"] == "1") != member.MetadataOnly {
			return nil, fmt.Errorf("macports: shared-release target %s changed identity", member.Target.Name)
		}
		// Reverification can cover implementation fixes, not a new release hidden
		// inside an amend/reassociate operation.
		if info.Version != member.After.Version || info.Epoch != member.After.Epoch {
			return nil, fmt.Errorf("macports: shared-release version changed for %s; start a new bump", member.Target.Name)
		}
		updated.Affected[i].After = ReleaseState(info)
		updated.Affected[i].NeedsXcode = info.Options["use_xcode"] == "yes" || info.Options["use_xcode"] == "true" || info.Options["use_xcode"] == "1"
	}
	for _, member := range updated.Protected {
		info, ok := snapshot.Ports[member.Target.Name]
		if !ok || ReleaseState(info) != member.After {
			return nil, fmt.Errorf("macports: protected sibling %s changed source identity", member.Target.Name)
		}
	}
	return &updated, nil
}

// ReleaseState records the evaluated version and archive identity for a scope member.
func ReleaseState(p PortInfo) record.ReleaseState {
	return record.ReleaseState{MasterSites: p.Options["master_sites"], Worksrcdir: p.Options["worksrcdir"], Epoch: p.Epoch, Version: p.Version, Revision: p.Revision, Tag: p.Options["git.branch"], Distfiles: p.Options["distfiles"], Checksums: p.Options["checksums"]}
}
