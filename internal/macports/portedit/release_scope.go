package portedit

import (
	"context"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
)

func (s *Service) checkSharedArchiveOwners(ctx context.Context, input *sourceInput, contents []byte, observed macports.Observation, selected distfiles.Binding) error {
	if input.scope == nil {
		return nil
	}
	for _, member := range input.scope.Affected {
		if member.MetadataOnly || member.Target.Name == input.target.Name {
			continue
		}
		next := *input
		next.target = member.Target
		binding, err := s.bindArchives(ctx, &next, contents, observed)
		if err != nil {
			return err
		}
		if len(binding.Artifacts) != len(selected.Artifacts) {
			return fmt.Errorf("%w: %s has an independent archive plan", ErrFidelity, member.Target.Name)
		}
		for i, item := range binding.Artifacts {
			other := selected.Artifacts[i]
			if item.Group.ID() != other.Group.ID() || item.Name != other.Name || !slices.Equal(item.URLs, other.URLs) {
				return fmt.Errorf("%w: %s has independently owned source/checksum declarations", ErrFidelity, member.Target.Name)
			}
		}
	}
	return nil
}
