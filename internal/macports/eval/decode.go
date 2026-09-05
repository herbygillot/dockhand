package eval

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// decodeSnapshot turns one shim reply into Values plus the subport list the
// reply carried. Subports are snapshot structure, consumed by enumeration,
// never stored in Values. Replies come from Tcl's own dict formatter, so a
// malformed one is protocol corruption and propagates as an error rather
// than being repaired.
func decodeSnapshot(reply string) (info.Values, []string, error) {
	fields, errs := syntax.DictValues(reply)
	if len(errs) != 0 {
		return info.Values{}, nil, fmt.Errorf("malformed dict reply %q: %w", reply, errs[0])
	}
	v := info.Values{
		Name:     fields["name"],
		Version:  fields["version"],
		Revision: fields["revision"],
		Epoch:    fields["epoch"],
		// Prose and configuration arrive as single list elements, so a
		// value with spaces comes braced; ListValue is identity for the
		// rest.
		Description:     syntax.ListValue(fields["description"]),
		Homepage:        syntax.ListValue(fields["homepage"]),
		LongDescription: syntax.ListValue(fields["long_description"]),
		Livecheck: info.Livecheck{
			Type:    syntax.ListValue(fields["livecheck.type"]),
			URL:     syntax.ListValue(fields["livecheck.url"]),
			Regex:   syntax.ListValue(fields["livecheck.regex"]),
			Version: syntax.ListValue(fields["livecheck.version"]),
		},
		Vendored: info.Vendored{
			GoVendors:         fields["go.vendors"],
			CargoCrates:       fields["cargo.crates"],
			CargoCratesGithub: fields["cargo.crates_github"],
		},
		Worksrcdir: syntax.ListValue(fields["worksrcdir"]),
		Filespath:  syntax.ListValue(fields["filespath"]),
		// The patch phase's own default, written in when the reply
		// carries nothing: a shim that did not report it — or a fake
		// that never speaks of patches — describes a port that patches
		// at -p0, not one that patches at nothing.
		PatchPreArgs: DefaultPatchPreArgs,
	}
	if pre, ok := fields["patch.pre_args"]; ok {
		v.PatchPreArgs = syntax.ListValue(pre)
	}
	var err error
	for _, f := range []struct {
		key string
		dst *[]string
	}{
		{"categories", &v.Categories},
		{"license", &v.License},
		{"maintainers", &v.Maintainers},
		{"platforms", &v.Platforms},
		{"distfiles", &v.Distfiles},
		{"checksums", &v.Checksums},
		{"patchfiles", &v.Patchfiles},
		{"depends_fetch", &v.Depends.Fetch},
		{"depends_extract", &v.Depends.Extract},
		{"depends_patch", &v.Depends.Patch},
		{"depends_build", &v.Depends.Build},
		{"depends_lib", &v.Depends.Lib},
		{"depends_run", &v.Depends.Run},
		{"depends_test", &v.Depends.Test},
	} {
		if *f.dst, err = listField(fields, f.key); err != nil {
			return info.Values{}, nil, err
		}
	}
	subs, err := listField(fields, "subports")
	if err != nil {
		return info.Values{}, nil, err
	}
	return v, subs, nil
}

// DefaultPatchPreArgs is what patch.pre_args reads when a port has not
// set it: base's own default from portpatch.tcl, verbatim — the strip
// level is the 0 the ruling names, and the two flags before it are
// what base passes with it — copied here because a reply without the
// key has to say what the patch phase would do rather than nothing.
// The shim reports the option whenever a port is open, so this is
// reached only by a fake that never speaks of patches.
const DefaultPatchPreArgs = "-t -N -p0"

// StripLevel reads the -pN out of a patch.pre_args value: the number of
// leading path components the patch phase tells patch(1) to discard
// from every file name in a hunk header.
//
// It answers 0 for everything it cannot read — an empty value, a value
// with no -p in it, a -p followed by something that is not a number —
// because 0 is base's default and the answer a port that said nothing
// gets. That is a policy and not a parse: a caller that needs to know
// whether the port SAID -p0 has the option's own text in
// Values.PatchPreArgs. Both spellings patch(1) takes are read, "-p1"
// and "-p 1", and when the option names more than one the last wins,
// which is what patch(1) does with its own arguments.
func StripLevel(pre string) int {
	level := 0
	args := strings.Fields(pre)
	for i := 0; i < len(args); i++ {
		digits, ok := strings.CutPrefix(args[i], "-p")
		if !ok {
			continue
		}
		if digits == "" && i+1 < len(args) {
			i++
			digits = args[i]
		}
		if n, err := strconv.Atoi(digits); err == nil && n >= 0 {
			level = n
		}
	}
	return level
}

// listField decodes a list-valued dict field, absent fields yielding nil.
// Absence tolerance is this decoder's policy, not Tcl's — dict get errors
// on a missing key — which is why this helper lives here rather than in
// syntax: that package holds what Tcl strings mean, this one holds what the
// shim protocol has decided about them.
func listField(fields map[string]string, key string) ([]string, error) {
	s, ok := fields[key]
	if !ok {
		return nil, nil
	}
	vals, errs := syntax.ListValues(s)
	if len(errs) != 0 {
		return nil, fmt.Errorf("malformed %s list %q: %w", key, s, errs[0])
	}
	return vals, nil
}
