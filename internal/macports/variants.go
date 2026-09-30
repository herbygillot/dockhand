package macports

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Variant is one a port declares, as Base records it (PortInfo(variants)
// and PortInfo(vinfo)): whether it's among the defaults, the variants it
// requires and conflicts with, and its description.
type Variant struct {
	Name        string
	Default     bool
	Requires    []string
	Conflicts   []string
	Description string
}

// Variants are the variants a port declares, in the order it declares
// them, as its evaluation reported them. A port that declares none has
// none; what can't be read is an error.
func (p PortInfo) Variants() ([]Variant, error) {
	names, _, err := p.optionList("variants")
	if err != nil {
		return nil, fmt.Errorf("%s's variants: %w", p.Name, err)
	}
	info, _, err := p.optionDict("vinfo")
	if err != nil {
		return nil, fmt.Errorf("%s's variants: %w", p.Name, err)
	}
	var variants []Variant
	for _, name := range names {
		if !ValidName(name) {
			return nil, fmt.Errorf("macports: %s declares a variant named %q", p.Name, name)
		}
		fields, errs := syntax.DictValues(info[name])
		if len(errs) > 0 {
			return nil, fmt.Errorf("macports: %s's +%s can't be read: %v", p.Name, name, errs)
		}
		variant := Variant{Name: name, Description: fields["description"]}
		// is_default exists only for a default variant, "+" where on.
		variant.Default = fields["is_default"] == "+"
		for key, into := range map[string]*[]string{"requires": &variant.Requires, "conflicts": &variant.Conflicts} {
			list, err := readList(p.Name+" +"+name+" "+key, fields[key])
			if err != nil {
				return nil, err
			}
			if len(list) > 0 {
				*into = list
			}
		}
		variants = append(variants, variant)
	}
	return variants, nil
}

// ParseVariants reads variant choices as MacPorts' command line takes
// them: "+tests", "+tests -docs", or "+tests-docs", each name after its
// sign. A name a port couldn't declare, or one chosen twice, is refused.
func ParseVariants(spec string) (map[string]bool, error) {
	variants := map[string]bool{}
	for _, word := range strings.Fields(spec) {
		for len(word) > 0 {
			sign := word[0]
			if sign != '+' && sign != '-' {
				return nil, fmt.Errorf("macports: %q: a variant is +name or -name", word)
			}
			word = word[1:]
			end := strings.IndexAny(word, "+-")
			if end < 0 {
				end = len(word)
			}
			name := word[:end]
			word = word[end:]
			if !ValidName(name) {
				return nil, fmt.Errorf("macports: %q is not a variant name", name)
			}
			if _, twice := variants[name]; twice {
				return nil, fmt.Errorf("macports: %s is chosen twice", name)
			}
			variants[name] = sign == '+'
		}
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("macports: no variants in %q", spec)
	}
	return variants, nil
}

// Undeclared are the variants chosen that a port doesn't declare, sorted.
func Undeclared(chosen map[string]bool, declared []Variant) []string {
	var missing []string
	for name := range chosen {
		if !slices.ContainsFunc(declared, func(v Variant) bool { return v.Name == name }) {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}
