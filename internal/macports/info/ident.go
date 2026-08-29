package info

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// VariantSet is a canonical, order-independent representation of variant
// selections. The zero value means the default variants. Canonical form —
// selections sorted by variant name, a later selection of a name replacing
// an earlier one — makes VariantSet usable directly as a map key.
type VariantSet string

// ErrMalformedSelection reports a variant selection that is not "+name" or
// "-name".
var ErrMalformedSelection = errors.New("info: malformed variant selection")

// Variants canonicalizes a list of selections. Each must be "+name" or
// "-name".
func Variants(selections ...string) (VariantSet, error) {
	byName := make(map[string]string, len(selections))
	for _, sel := range selections {
		if len(sel) < 2 || (sel[0] != '+' && sel[0] != '-') {
			return "", fmt.Errorf("%w: %q (want +name or -name)", ErrMalformedSelection, sel)
		}
		byName[sel[1:]] = sel
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(byName[n])
	}
	return VariantSet(b.String()), nil
}

// List returns the canonical selections, nil for the default set.
func (v VariantSet) List() []string {
	if v == "" {
		return nil
	}
	var out []string
	s := string(v)
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] == '+' || s[i] == '-' {
			out = append(out, s[start:i])
			start = i
		}
	}
	return append(out, s[start:])
}

// SubportKey names one evaluation context: a subport under a variant set.
// It is the key type of an evaluation snapshot.
type SubportKey struct {
	Subport  string
	Variants VariantSet
}
