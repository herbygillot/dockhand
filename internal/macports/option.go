package macports

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Checked readings of an evaluated option, which the typed accessors
// share (the helper-ownership review's finding 4): a value, an option the
// evaluation didn't set, or one it couldn't settle, which is an error and
// never a value; and a list or dictionary that doesn't parse is an error
// too, not an empty one. What an absent option means is each accessor's
// to say.

// option is an evaluated option's value, and whether it was set; an error
// where the evaluation couldn't settle it.
func (p PortInfo) option(name string) (value string, set bool, err error) {
	if failure, failed := p.OptionErrors[name]; failed {
		return "", false, fmt.Errorf("macports: evaluating %s: %s", name, failure)
	}
	value, set = p.Options[name]
	return value, set, nil
}

// optionList is an evaluated option read as a Tcl list.
func (p PortInfo) optionList(name string) ([]string, bool, error) {
	value, set, err := p.option(name)
	if err != nil || !set {
		return nil, set, err
	}
	list, err := readList(name, value)
	return list, true, err
}

// optionDict is an evaluated option read as a Tcl dictionary.
func (p PortInfo) optionDict(name string) (map[string]string, bool, error) {
	value, set, err := p.option(name)
	if err != nil || !set {
		return nil, set, err
	}
	dict, errs := syntax.DictValues(value)
	if len(errs) > 0 {
		return nil, true, fmt.Errorf("macports: %s %q isn't a Tcl dictionary: %v", name, value, errs[0])
	}
	return dict, true, nil
}

// readList reads a value as a Tcl list, naming what it was on failure.
func readList(what, value string) ([]string, error) {
	list, errs := syntax.ListValues(value)
	if len(errs) > 0 {
		return nil, fmt.Errorf("macports: %s %q isn't a Tcl list: %v", what, value, errs[0])
	}
	return list, nil
}
