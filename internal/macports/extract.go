package macports

import "strings"

// Extraction is how MacPorts extracts an archive by its suffix: the use_
// option that picks the compressor, none for gzip, the default, and the
// suffix that option implies (portextract::get_extract_suffix), which a
// Portfile names in extract.suffix only where the archive's differs.
type Extraction struct {
	Suffix string
	// UseOption is one of ExtractOptions; empty for gzip, and for one
	// Base 2.12 has no option for, which Command extracts.
	UseOption string
	// Implied is the suffix the use_ option, or gzip, gives by default.
	Implied string
	// Command is the extract.cmd an archive no use_ option extracts takes,
	// zstd, with Depends its extract dependency, bin:zstd:zstd, both
	// written with its extract.suffix, as devel/libhx writes them; empty
	// for the rest.
	Command, Depends string
}

// ExtractOptions are Base 2.12's use_ options, each a compressor or a
// container its extract phase picks (portextract.tcl): gzip, the default,
// has none.
var ExtractOptions = []string{"use_bzip2", "use_lzma", "use_tar", "use_xz", "use_zip", "use_7z", "use_lzip", "use_dmg"}

// Extractions are the archive suffixes Base extracts, the longest first,
// so .tar.gz is read before .gz would be: Base 2.12's, by a use_ option or
// its default, and .tar.zst and .tzst, which Base's master extracts by
// suffix and 2.12 by an explicit extract.cmd (Command).
var Extractions = []Extraction{
	{Suffix: ".tar.bz2", UseOption: "use_bzip2", Implied: ".tar.bz2"},
	{Suffix: ".tar.gz", Implied: ".tar.gz"},
	{Suffix: ".tar.xz", UseOption: "use_xz", Implied: ".tar.xz"},
	{Suffix: ".tar.lzma", UseOption: "use_lzma", Implied: ".tar.lzma"},
	{Suffix: ".tar.lz", UseOption: "use_lzip", Implied: ".tar.lz"},
	{Suffix: ".tar.zst", Implied: ".tar.zst", Command: "zstd", Depends: "bin:zstd:zstd"},
	{Suffix: ".tbz2", UseOption: "use_bzip2", Implied: ".tar.bz2"},
	{Suffix: ".tbz", UseOption: "use_bzip2", Implied: ".tar.bz2"},
	{Suffix: ".tgz", Implied: ".tar.gz"},
	{Suffix: ".txz", UseOption: "use_xz", Implied: ".tar.xz"},
	{Suffix: ".tlz", UseOption: "use_lzma", Implied: ".tar.lzma"},
	{Suffix: ".tzst", Implied: ".tar.zst", Command: "zstd", Depends: "bin:zstd:zstd"},
	{Suffix: ".tar", UseOption: "use_tar", Implied: ".tar"},
	{Suffix: ".zip", UseOption: "use_zip", Implied: ".zip"},
	{Suffix: ".7z", UseOption: "use_7z", Implied: ".7z"},
	{Suffix: ".dmg", UseOption: "use_dmg", Implied: ".dmg"},
}

// ExtractionOf is how MacPorts extracts a file by its name; false for a
// name with no suffix it extracts.
func ExtractionOf(name string) (Extraction, bool) {
	for _, extraction := range Extractions {
		if strings.HasSuffix(name, extraction.Suffix) && len(name) > len(extraction.Suffix) {
			return extraction, true
		}
	}
	return Extraction{}, false
}
