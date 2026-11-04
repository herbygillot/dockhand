package macports

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every suffix Base extracts is known: Base 2.12's eight use_ options and
// gzip's default, and zstd's, which takes an extract.cmd (devel/libhx).
func TestEveryArchiveBaseExtractsIsKnown(t *testing.T) {
	for name, want := range map[string]Extraction{
		"tool-1.0.tar.lzma": {Suffix: ".tar.lzma", UseOption: "use_lzma", Implied: ".tar.lzma"},
		"tool-1.0.tlz":      {Suffix: ".tlz", UseOption: "use_lzma", Implied: ".tar.lzma"},
		"tool-1.0.tar.lz":   {Suffix: ".tar.lz", UseOption: "use_lzip", Implied: ".tar.lz"},
		"tool-1.0.tar":      {Suffix: ".tar", UseOption: "use_tar", Implied: ".tar"},
		"tool-1.0.7z":       {Suffix: ".7z", UseOption: "use_7z", Implied: ".7z"},
		"tool-1.0.dmg":      {Suffix: ".dmg", UseOption: "use_dmg", Implied: ".dmg"},
		"tool-1.0.tar.zst":  {Suffix: ".tar.zst", Implied: ".tar.zst", Command: "zstd", Depends: "bin:zstd:zstd"},
		"tool-1.0.tzst":     {Suffix: ".tzst", Implied: ".tar.zst", Command: "zstd", Depends: "bin:zstd:zstd"},
		"tool-1.0.tar.gz":   {Suffix: ".tar.gz", Implied: ".tar.gz"},
	} {
		got, ok := ExtractionOf(name)
		require.True(t, ok, name)
		require.Equal(t, want, got, name)
	}
	_, ok := ExtractionOf("tool-1.0.gz")
	require.False(t, ok, "a bare .gz isn't one Base extracts")
	for _, extraction := range Extractions {
		require.True(t, extraction.UseOption == "" || slices.Contains(ExtractOptions, extraction.UseOption), extraction.Suffix)
	}
	require.Len(t, ExtractOptions, 8)
}
