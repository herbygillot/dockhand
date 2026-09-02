package bump

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"github.com/herbygillot/dockhand/internal/tool"
)

// goDirectiveRE matches go.mod's `go` directive — the module's declared
// minimum toolchain, which module-mode builds enforce exactly.
var goDirectiveRE = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)

// toolchainMinEdit updates a declared go.toolchain_min to what the new
// version's go.mod requires. Update-only, by the golang PortGroup's own
// rules: a port that does not declare a minimum is never gated, and in
// GOPATH mode the go.mod directive is only an upper bound — so dockhand
// rewrites a declaration the maintainer already made and never invents
// one. The rewrite happens only when the SERIES moved: the PortGroup
// compares series alone, and rewriting 1.22 to 1.22.0 would be churn.
//
// ok is false when there is nothing to do — no declaration, no go.mod,
// no directive, or the series is unchanged.
func toolchainMinEdit(ctx context.Context, tools *tool.Finder, src []byte, cst *syntax.Script, contextName string, fetched []string, worksrcdir string) (edit.Edit, bool, error) {
	span, declared, found := locateToolchainMin(src, cst, contextName)
	if !found {
		return edit.Edit{}, false, nil
	}
	mod, from, err := distfile.Extract(ctx, tools, fetched, worksrcdir, "go.mod")
	if err != nil {
		// A project with no go.mod cannot say what it needs; the
		// declaration the maintainer made stands.
		slog.Debug("no go.mod in the distfiles; go.toolchain_min stands", "err", err)
		return edit.Edit{}, false, nil
	}
	m := goDirectiveRE.FindSubmatch(mod)
	if m == nil {
		slog.Debug("go.mod carries no go directive; go.toolchain_min stands", "from", from)
		return edit.Edit{}, false, nil
	}
	required := string(m[1])
	if goSeries(required) == goSeries(declared) {
		return edit.Edit{}, false, nil
	}
	slog.Debug("go.toolchain_min moves", "declared", declared, "required", required)
	return edit.Edit{
		Start: span.Start, End: span.End,
		Old: declared, New: required, Reason: "go.toolchain_min",
	}, true, nil
}

// locateToolchainMin finds the literal value of a go.toolchain_min
// declaration in the context's scope, the last one winning as Tcl
// would have it. A non-literal value is left alone: update-only means
// never guessing at what a computed declaration meant.
func locateToolchainMin(src []byte, cst *syntax.Script, contextName string) (text.Span, string, bool) {
	var span text.Span
	var value string
	found := false
	for cmd := range cst.Commands(src, portstyle.ScopeOf(src, contextName)) {
		name, ok := cmd.Name(src)
		if !ok || name != "go.toolchain_min" || len(cmd.Words) < 2 {
			continue
		}
		w := cmd.Words[1]
		if _, lit := w.Literal(src); !lit {
			continue
		}
		span, value, found = w.Span, w.Span.Text(src), true
	}
	return span, value, found
}

// goSeries reduces a toolchain version to the series the PortGroup
// compares: major.minor.
func goSeries(v string) string {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	if len(parts) < 2 {
		return strings.TrimSpace(v)
	}
	return parts[0] + "." + parts[1]
}
