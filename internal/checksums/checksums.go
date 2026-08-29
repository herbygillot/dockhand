// Package checksums holds MacPorts' checksum vocabulary and mechanics:
// the triple a Portfile's checksums option records, the option's spec
// grammar, generation (hashing fetched bytes), and validation of
// computed sums against recorded ones. Fetching lives elsewhere
// (distfile, macports/portfetch); what a checksum is lives here.
package checksums

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sums is the checksum triple a Portfile's checksums option records.
type Sums struct {
	Rmd160 string
	Sha256 string
	Size   int64
}

// Value returns the computed sum for a checksum type token.
func (s Sums) Value(typ string) (string, bool) {
	switch typ {
	case "rmd160":
		return s.Rmd160, true
	case "sha256":
		return s.Sha256, true
	case "size":
		return strconv.FormatInt(s.Size, 10), true
	}
	return "", false
}

// IsType reports a checksum type token dockhand can compute.
func IsType(tok string) bool {
	return tok == "rmd160" || tok == "sha256" || tok == "size"
}

// IsLegacyType reports a checksum type token that still appears in
// ancient ports but cannot responsibly be recomputed.
func IsLegacyType(tok string) bool {
	return tok == "md5" || tok == "sha1"
}

// Recorded is one (distfile, type, value) triple parsed from the
// evaluated checksums option.
type Recorded struct {
	File  string // "" in the single-distfile form
	Type  string
	Value string
}

// ErrMalformed reports a checksums spec that does not parse.
var ErrMalformed = errors.New("checksums: malformed checksums spec")

// Parse splits the evaluated checksums tokens into recorded triples:
// repeated [filename] {type value}... groups, filenames optional when
// the port has one distfile.
func Parse(tokens []string) ([]Recorded, error) {
	var out []Recorded
	file := ""
	for i := 0; i < len(tokens); {
		tok := tokens[i]
		switch {
		case IsType(tok) || IsLegacyType(tok):
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("%w: type %q has no value", ErrMalformed, tok)
			}
			out = append(out, Recorded{File: file, Type: tok, Value: tokens[i+1]})
			i += 2
		default:
			file = tok
			i++
		}
	}
	return out, nil
}

// ErrMismatch reports computed sums that disagree with recorded ones.
var ErrMismatch = errors.New("checksums: mismatch")

// Verify checks one file's computed sums against its recorded triples.
// A legacy type cannot be verified and counts as a failure — silence
// must never imply a check that did not happen.
func Verify(s Sums, recorded []Recorded) error {
	var problems []string
	for _, r := range recorded {
		if IsLegacyType(r.Type) {
			problems = append(problems, fmt.Sprintf("%s unverifiable (legacy type)", r.Type))
			continue
		}
		got, ok := s.Value(r.Type)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s unknown type", r.Type))
			continue
		}
		if got != r.Value {
			problems = append(problems, fmt.Sprintf("%s recorded %s, computed %s", r.Type, r.Value, got))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrMismatch, strings.Join(problems, "; "))
	}
	return nil
}
