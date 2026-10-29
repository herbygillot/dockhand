package portfile

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// SwitchHTTPS writes https for each plain-HTTP link whose option, homepage,
// master_sites, or livecheck.url, names its URL as a literal word, anywhere
// in the Portfile: the URL itself, or a site followed by its tags, which
// are kept, as is each site's place in its list, an -append's or a
// -prepend's among them. A link is switched in its own option alone, so a
// site is switched only where its own distfile answered, never because a
// livecheck.url of the same URL did. It reports the links it switched; one
// written any other way, through a variable or in quotes, is left as it is
// (the person's "Let's try to get HTTP to HTTPS into 0.7.0", 2026-10-29).
func SwitchHTTPS(src []byte, links []macports.PlainURL) ([]byte, []macports.PlainURL) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 || len(links) == 0 {
		return src, nil
	}
	var edits []textedit.Edit
	var switched []macports.PlainURL
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		name, _ := cmd.Name(src)
		option := strings.TrimSuffix(strings.TrimSuffix(name, "-append"), "-prepend")
		switch option {
		case "homepage", "master_sites", "livecheck.url":
		default:
			return
		}
		for _, word := range cmd.Words[1:] {
			value, literal := word.Literal(src)
			if !literal || !strings.HasPrefix(value, "http://") {
				continue
			}
			i := slices.IndexFunc(links, func(link macports.PlainURL) bool {
				return link.Option == option && (value == link.URL || strings.HasPrefix(value, link.URL+":"))
			})
			if i < 0 {
				continue
			}
			// A literal's text is its value, the scheme first.
			span := textedit.Span{Start: word.Span.Start, End: word.Span.Start + len("http://")}
			edits = append(edits, textedit.Edit{Span: span, New: []byte("https://")})
			if !slices.Contains(switched, links[i]) {
				switched = append(switched, links[i])
			}
		}
	})
	if len(edits) == 0 {
		return src, nil
	}
	out, err := textedit.Apply(src, edits)
	if err != nil {
		return src, nil
	}
	return out, switched
}
