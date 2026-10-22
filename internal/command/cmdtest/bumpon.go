package cmdtest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/term"
)

// BumpOn runs bump where a person could answer, to show it asks nothing.
func BumpOn(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := RunFor(t, t.Context(), append([]string{"bump"}, args...), term.Streams{In: strings.NewReader("y\ny\ny\n"), Out: &out, Err: &errs}.AsTerminal())
	return out.String(), errs.String(), err
}
