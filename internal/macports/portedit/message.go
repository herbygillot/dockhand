package portedit

import (
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
)

// generatedByPrefix opens the trailer every generated contribution commit
// ends with. The legacy prefixes are what earlier builds wrote; they are
// still recognized so a rewritten message carries one trailer, not two.

// Message renders a completely generated contribution, including its attribution.
func (c CommitIntent) Message() string {
	return commitmsg.Compose(c.Subject, c.Body, c.References)
}
