package eval

import (
	"fmt"

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
