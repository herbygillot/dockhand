// Package usage is the guide's pages, embedded, so the binary prints the
// text the repository keeps here (dockhand usage). It holds the pages
// alone: which topic is which page, and how one prints, are the usage
// command's.
package usage

import "embed"

// Pages are this directory's Markdown pages, by file name.
//
//go:embed *.md
var Pages embed.FS
