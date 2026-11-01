package macports

import "strings"

// Extraction is how MacPorts extracts an archive by its suffix: the use_
// option that picks the compressor, none for gzip, the default, and the
// suffix that option implies (portextract::get_extract_suffix), which a
// Portfile names in extract.suffix only where the archive's differs.
type Extraction struct {
	Suffix string
	// UseOption is use_bzip2, use_xz, use_lzip, or use_zip; empty for
	// gzip.
	UseOption string
	// Implied is the suffix the use_ option, or gzip, gives by default.
	Implied string
}

// Extractions are the archive suffixes Base 2.12 extracts with a use_
// option or its default, the longest first, so .tar.gz is read before
// .gz would be. A .tar.zst isn't one: Base has no use_zstd, and a port
// fetching one sets its own extract.cmd.
var Extractions = []Extraction{
	{Suffix: ".tar.bz2", UseOption: "use_bzip2", Implied: ".tar.bz2"},
	{Suffix: ".tar.gz", Implied: ".tar.gz"},
	{Suffix: ".tar.xz", UseOption: "use_xz", Implied: ".tar.xz"},
	{Suffix: ".tar.lz", UseOption: "use_lzip", Implied: ".tar.lz"},
	{Suffix: ".tbz2", UseOption: "use_bzip2", Implied: ".tar.bz2"},
	{Suffix: ".tbz", UseOption: "use_bzip2", Implied: ".tar.bz2"},
	{Suffix: ".tgz", Implied: ".tar.gz"},
	{Suffix: ".txz", UseOption: "use_xz", Implied: ".tar.xz"},
	{Suffix: ".zip", UseOption: "use_zip", Implied: ".zip"},
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
