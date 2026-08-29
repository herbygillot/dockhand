package intent

import (
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/text"
)

// Shadow materializes a copy of portdir with the edits applied to its
// Portfile: the edit application is the planner's half, the portdir
// copy is tree.Shadow's. The caller removes the returned
// directory.
func Shadow(portdir string, src []byte, edits []plan.Edit) (string, error) {
	edited, err := text.Apply(src, textEdits(edits))
	if err != nil {
		return "", err
	}
	return tree.Shadow(portdir, edited)
}

// textEdits converts plan edits to text edits.
func textEdits(edits []plan.Edit) []text.Edit {
	out := make([]text.Edit, 0, len(edits))
	for _, e := range edits {
		out = append(out, text.Edit{
			Span: text.Span{Start: e.Start, End: e.End},
			New:  []byte(e.New),
		})
	}
	return out
}
