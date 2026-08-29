package intent

import (
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/text"
)

// Shadow applies a planner's edits to the Portfile source and
// materializes the result as a shadow of the handle's port: applying
// the edits is the planner's half, copying the portdir is
// port.Handle.Shadow's. The returned function removes the shadow.
func Shadow(h port.Handle, src []byte, edits []plan.Edit) (port.Handle, func(), error) {
	edited, err := text.Apply(src, textEdits(edits))
	if err != nil {
		return port.Handle{}, nil, err
	}
	return h.Shadow(edited)
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
