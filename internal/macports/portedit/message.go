package portedit

import (
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
)

// Message renders a completely generated contribution, including its attribution.
func (c CommitIntent) Message() string {
	return commitmsg.Compose(c.Subject, c.Body, c.References)
}
