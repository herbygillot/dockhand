package cmd

import (
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/intent/refresh"
)

// refreshCaution is printed with every refresh summary. The intent
// applies like any other — the user asking is the human in the loop —
// but a checksum that moves at an unchanged version means upstream
// re-rolled the artifact: possibly a benign re-tar, possibly a
// supply-chain event, and the edit cannot tell you which.
const refreshCaution = "note: these checksums changed at an UNCHANGED version — upstream re-rolled\n" +
	"the artifact. Establish why before this change goes anywhere public: it may\n" +
	"be a benign re-tar, or it may be a supply-chain event.\n"

// refreshVerb is the catalogue's entry for making a port's recorded
// checksums true again at its unchanged version. It is the one verb the
// registration shape reproduces with nothing left over: no flags of its
// own, and nothing to resolve.
var refreshVerb = intentVerb{
	Definition: intent.Definition{
		Name:    "refresh-checksums",
		Aliases: []string{"refresh"},
		Fetches: true,
		Caution: refreshCaution,
		New: func(p intent.Params) (intent.Planner, error) {
			return refresh.Refresh{ClosesTicket: p.ClosesTicket, Riders: p.Riders}, nil
		},
	},
	Short: "Re-fetch a port's distfiles and repair its recorded checksums",
}
