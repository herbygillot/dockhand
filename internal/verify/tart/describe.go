package tart

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/tool"
)

// describe asks a booted guest what it is: the full macOS product and
// build version, and the Xcode that would compile a port there.
//
// IT IS ASKED OF THE GUEST AND NEVER INFERRED FROM THE BASE. A base is
// provisioned for a release and reports a POINT version, and the two are
// different facts — the image is a `:latest` tag that moved, and the
// system that booted off it is whatever Apple shipped that week. A
// reviewer told "built in a pristine VM" is owed the machine it was
// pristine on, and only the machine can say.
//
// EVERY ANSWER IS OPTIONAL AND SILENCE IS AN ANSWER. A guest that will
// not run sw_vers, or that has no Xcode, returns empty — and empty
// travels to the record as empty, so a pull request body reports what
// the provider provided and nothing else. Rule 7: an unasked question
// must not be answered with a plausible name.
//
// It is two `tart exec` calls on a guest that is already awake for the
// upload that follows, so it costs a round trip and not a boot.
func describe(ctx context.Context, tools *tool.Finder, vm string) (osVersion, xcode string) {
	product := strings.TrimSpace(one(ctx, tools, vm, "/usr/bin/sw_vers", "-productVersion"))
	build := strings.TrimSpace(one(ctx, tools, vm, "/usr/bin/sw_vers", "-buildVersion"))
	switch {
	case product != "" && build != "":
		osVersion = product + " (" + build + ")"
	case product != "":
		osVersion = product
	}
	// `xcodebuild -version` answers over two lines — "Xcode 26.6" then
	// "Build version 17A400" — and the first is the one a reviewer reads.
	// A guest with only the command line tools answers an error, which is
	// silence here rather than a sentence about the guest's plumbing.
	if out := one(ctx, tools, vm, "/usr/bin/xcodebuild", "-version"); out != "" {
		if first, _, _ := strings.Cut(strings.TrimSpace(out), "\n"); strings.HasPrefix(first, "Xcode") {
			xcode = strings.TrimSpace(first)
		}
	}
	return osVersion, xcode
}

// one runs a command in the guest and answers with its output, or with
// nothing at all. A failure is not reported: describe's whole contract
// is that what it cannot learn it does not claim.
func one(ctx context.Context, tools *tool.Finder, vm string, argv ...string) string {
	out, err := Exec(ctx, tools, vm, argv...)
	if err != nil {
		return ""
	}
	return out
}
