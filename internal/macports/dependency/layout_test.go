package dependency

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Rows copied byte for byte from the maintained Codex and mdx Portfiles.
const codexBlock = "cargo.crates \\\n" +
	"    dispatch2                        0.3.0  89a09f22a6c6069a18470eb92d2298acf25463f14256d24778e1230d789a2aec \\\n" +
	"    gix-revision                    0.43.0  7c08f1ec5d1e6a524f8ba291c41f0ccaef64e48ed0e8cf790b3461cae45f6d3d \\\n" +
	"    wasi                          0.11.1+wasi-snapshot-preview1  ccf3ec651a847eb01de73ccad15eb7d99f80485de043efb2f370cd654f4ea44b \\\n" +
	"    windows-implement               0.58.0  2bbd5b46c938e506ecbce286b6628a02171d56153ba733b6c741fc627ec9579b \\\n" +
	"    zvariant                         4.2.0  2084290ab9a1c471c38fc524945837734fbf124487e105daec2bb57fd48c81fe"

const mdxBlock = "go.vendors          gopkg.in/yaml.v3 \\\n" +
	"                        lock    9f266ea9e77c \\\n" +
	"                        rmd160  06dca2ede07b2f31c515b4711fbebc1d5359b5e4 \\\n" +
	"                        sha256  e70dd42fb30b7b2d0129c5cdf0e079caaf5602cab24081fdac830ec01204fa59 \\\n" +
	"                        size    86890 \\\n" +
	"                    gopkg.in/check.v1 \\\n" +
	"                        lock    20d25e280405 \\\n" +
	"                        rmd160  412aa0d109919182ff84259e9b5bbc9f24d78117 \\\n" +
	"                        sha256  233f8faf427ce6701ac3427f85c28bc6b6ae7cdc97a303a52873c69999223325 \\\n" +
	"                        size    30360"

func TestChangedCrateBlockKeepsMaintainedColumns(t *testing.T) {
	t.Parallel()
	src := []byte("name fixture\nversion 1\n" + codexBlock + "\nlicense MIT\n")
	values, err := generated(src, Cargo)
	require.NoError(t, err)
	plan, err := Inspect(src, map[string]string{Cargo: strings.Join(values, " ")})
	require.NoError(t, err)
	stripped, err := plan.Strip(src)
	require.NoError(t, err)
	sha := strings.Repeat("b", 64)
	// gix-revision moves, wasi keeps its long version, windows-implement is
	// replaced by a crate with a long name, and a new crate joins.
	next := []string{
		"dispatch2", "0.3.0", "89a09f22a6c6069a18470eb92d2298acf25463f14256d24778e1230d789a2aec",
		"gix-revision", "0.44.0", sha,
		"process_security_environment_spec", "0.8.0", sha,
		"wasi", "0.11.1+wasi-snapshot-preview1", "ccf3ec651a847eb01de73ccad15eb7d99f80485de043efb2f370cd654f4ea44b",
		"zvariant", "4.2.0", "2084290ab9a1c471c38fc524945837734fbf124487e105daec2bb57fd48c81fe",
		"zzz", "10.0.0-rc.1+build", sha,
	}
	out, err := plan.Apply(stripped, map[string][]string{Cargo: next})
	require.NoError(t, err)
	text := string(out)
	require.Contains(t, text, "cargo.crates \\\n    dispatch2                        0.3.0  89a09f22", "unchanged rows are byte-identical")
	require.Contains(t, text, "    wasi                          0.11.1+wasi-snapshot-preview1  ccf3ec65")
	require.Contains(t, text, "    gix-revision                    0.44.0  "+sha+" \\\n", "a changed version keeps the hash column")
	require.Contains(t, text, "    process_security_environment_spec   0.8.0  "+sha, "a long name keeps the version field width, as cargo2port does")
	require.Contains(t, text, "    zzz                           10.0.0-rc.1+build  "+sha+"\nlicense MIT", "a long version starts at the field floor and pushes the hash by the observed gap; the block ends without a continuation")
	require.NotContains(t, text, "windows-implement")
	regenerated, err := generated(out, Cargo)
	require.NoError(t, err)
	require.Equal(t, next, regenerated)
}

func TestChangedGoBlockKeepsModuleAndFieldColumns(t *testing.T) {
	t.Parallel()
	src := []byte("name fixture\n" + mdxBlock + "\n")
	values, err := generated(src, Go)
	require.NoError(t, err)
	plan, err := Inspect(src, map[string]string{Go: strings.Join(values, " ")})
	require.NoError(t, err)
	stripped, err := plan.Strip(src)
	require.NoError(t, err)
	sha := strings.Repeat("c", 64)
	next := append([]string{}, values[:9]...)
	next = append(next, "gopkg.in/check.v1", "lock", "aaaaaaaaaaaa", "sha256", sha, "size", "1")
	out, err := plan.Apply(stripped, map[string][]string{Go: next})
	require.NoError(t, err)
	text := string(out)
	require.Contains(t, text, strings.Join(strings.Split(mdxBlock, "\n")[:5], "\n"), "the unchanged module group is byte-identical, including its place on the command line")
	require.Contains(t, text, "                    gopkg.in/check.v1 \\\n                        lock    aaaaaaaaaaaa \\\n                        sha256  "+sha+" \\\n                        size    1\n")
}

func TestUnusualBlocksFallBackToPlainRows(t *testing.T) {
	t.Parallel()
	require.Nil(t, inferLayout(Cargo, "cargo.crates \\\n    a 1.0 \\\n    "+strings.Repeat("a", 64)), "a wrapped row is not a sample")
	require.Nil(t, inferLayout(Go, "go.vendors \\\n    example.com/x lock v1 sha256 abc"), "a single-line Go module is not go2port's shape")
	require.Nil(t, inferLayout(Cargo, "cargo.crates"))
	layout := inferLayout(CargoGit, "cargo.crates_github \\\n    name owner/repo main "+strings.Repeat("a", 40)+" "+strings.Repeat("b", 64))
	require.NotNil(t, layout)
	require.Equal(t, "cargo.crates_github \\\n    name owner/repo main "+strings.Repeat("a", 40)+" "+strings.Repeat("b", 64), layout.format([][]string{{"name", "owner/repo", "main", strings.Repeat("a", 40), strings.Repeat("b", 64)}}))
}

// Rows copied byte for byte from the maintained xan Portfile, whose long
// versions end on the column like the others instead of starting a field.
const xanBlock = "cargo.crates \\\n" +
	"    adler2                           2.0.1  320119579fcad9c21884f5c4861d16174d0e06250625266f50fe6898340abefa \\\n" +
	"    aho-corasick                     1.1.3  8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \\\n" +
	"    bstr                            1.12.0  234113d19d0d7d613b40e86fb654acf958910802bcceab913a4f9e7cda03b1a4 \\\n" +
	"    encoding-index-simpchinese 1.20141219.5 d87a7194909b9118fc707194baa434a4e3b0fb6a5a757c73c3adb07aa25031f7 \\\n" +
	"    wasi     0.11.1+wasi-snapshot-preview1  ccf3ec651a847eb01de73ccad15eb7d99f80485de043efb2f370cd654f4ea44b"

// A long version that ends on the column shows no field start, so it must
// not become one: xan's 0.61.0 bump pushed 39 changed rows past the column
// by the width of wasi's version, one space from their hash.
func TestChangedCrateBlockKeepsRightAlignedVersions(t *testing.T) {
	t.Parallel()
	src := []byte("name fixture\nversion 1\n" + xanBlock + "\nlicense MIT\n")
	values, err := generated(src, Cargo)
	require.NoError(t, err)
	plan, err := Inspect(src, map[string]string{Cargo: strings.Join(values, " ")})
	require.NoError(t, err)
	stripped, err := plan.Strip(src)
	require.NoError(t, err)
	sha := strings.Repeat("c", 64)
	next := []string{
		"adler2", "2.0.1", "320119579fcad9c21884f5c4861d16174d0e06250625266f50fe6898340abefa",
		"aho-corasick", "1.1.4", sha,
		"binary-heap-plus", "0.5.0", sha,
		"bstr", "1.12.0", "234113d19d0d7d613b40e86fb654acf958910802bcceab913a4f9e7cda03b1a4",
		"encoding-index-simpchinese", "1.20141219.5", "d87a7194909b9118fc707194baa434a4e3b0fb6a5a757c73c3adb07aa25031f7",
		"wasi", "0.11.1+wasi-snapshot-preview1", "ccf3ec651a847eb01de73ccad15eb7d99f80485de043efb2f370cd654f4ea44b",
		"wasip3", "0.4.0+wasi-0.3.0-rc-2026-01-06", sha,
		"wit-bindgen-rust-macro-extended", "0.51.0", sha,
	}
	out, err := plan.Apply(stripped, map[string][]string{Cargo: next})
	require.NoError(t, err)
	text := string(out)
	require.Contains(t, text, "    aho-corasick                     1.1.4  "+sha, "a changed version ends on the column")
	require.Contains(t, text, "    binary-heap-plus                 0.5.0  "+sha, "a new crate ends on the column")
	require.Contains(t, text, "    encoding-index-simpchinese 1.20141219.5 d87a7194", "an unchanged row is byte-identical")
	require.Contains(t, text, "    wasip3  0.4.0+wasi-0.3.0-rc-2026-01-06  "+sha, "a new long version ends on the column too")
	require.Contains(t, text, "    wit-bindgen-rust-macro-extended 0.51.0  "+sha, "a name past the column keeps the block's smallest gap")
	regenerated, err := generated(out, Cargo)
	require.NoError(t, err)
	require.Equal(t, next, regenerated)
}
