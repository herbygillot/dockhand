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
	src := []byte("name fixture\nversion 1\n" + codexBlock + "\nlicense MIT\n")
	values, err := Generated(src, Cargo)
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
	regenerated, err := Generated(out, Cargo)
	require.NoError(t, err)
	require.Equal(t, next, regenerated)
}

func TestChangedGoBlockKeepsModuleAndFieldColumns(t *testing.T) {
	src := []byte("name fixture\n" + mdxBlock + "\n")
	values, err := Generated(src, Go)
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
	require.Nil(t, inferLayout(Cargo, "cargo.crates \\\n    a 1.0 \\\n    "+strings.Repeat("a", 64)), "a wrapped row is not a sample")
	require.Nil(t, inferLayout(Go, "go.vendors \\\n    example.com/x lock v1 sha256 abc"), "a single-line Go module is not go2port's shape")
	require.Nil(t, inferLayout(Cargo, "cargo.crates"))
	layout := inferLayout(CargoGit, "cargo.crates_github \\\n    name owner/repo main "+strings.Repeat("a", 40)+" "+strings.Repeat("b", 64))
	require.NotNil(t, layout)
	require.Equal(t, "cargo.crates_github \\\n    name owner/repo main "+strings.Repeat("a", 40)+" "+strings.Repeat("b", 64), layout.format([][]string{{"name", "owner/repo", "main", strings.Repeat("a", 40), strings.Repeat("b", 64)}}))
}
