package portedit

import (
	"context"
	"errors"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/model"
)

// sharedArchiveOwners are the members of a shared release, other than the
// target, that fetch archives of their own: none of the target's checksum
// declarations theirs, as xapian-core's bindings each declare their own,
// which the update plans as it plans the target's. A member that shares
// the target's archives must share all of them, as its declarations are
// then the target's; one that shares some but not all is refused, as
// which of its archives the target's edit moves can't be shown.
func (s *Service) sharedArchiveOwners(ctx context.Context, input *sourceInput, contents []byte, observed macports.Observation, selected distfiles.Binding) ([]model.Target, error) {
	if input.scope == nil {
		return nil, nil
	}
	ids := map[string]bool{}
	for _, item := range selected.Artifacts {
		ids[item.Group.ID()] = true
	}
	var owners []model.Target
	for _, member := range input.scope.Affected {
		if member.MetadataOnly || member.Target.Name == input.target.Name {
			continue
		}
		if _, present := observed.Snapshot.Ports[member.Target.Name]; !present {
			continue
		}
		binding, err := s.bindArchives(ctx, input.forMember(member.Target), contents, observed)
		switch {
		case errors.Is(err, errFetchesNothing):
			// A member with no archive of its own, as libgcc beside gcc,
			// owns none of the selected port's.
			continue
		case err != nil:
			return nil, err
		}
		shared := 0
		for _, item := range binding.Artifacts {
			if ids[item.Group.ID()] {
				shared++
			}
		}
		if shared == 0 {
			owners = append(owners, member.Target)
			continue
		}
		if len(binding.Artifacts) != len(selected.Artifacts) {
			return nil, ErrFidelity.Because("%s has an independent archive plan", member.Target.Name)
		}
		for i, item := range binding.Artifacts {
			other := selected.Artifacts[i]
			if item.Group.ID() != other.Group.ID() || item.Name != other.Name || !slices.Equal(item.URLs, other.URLs) {
				return nil, ErrFidelity.Because("%s has independently owned source/checksum declarations", member.Target.Name)
			}
		}
	}
	return owners, nil
}
