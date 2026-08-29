package eval

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Decoder tests are hermetic: replies are Tcl dicts as the shim's own
// [dict create] would format them, and decoding needs no interpreter.

func TestDecodeSnapshot(t *testing.T) {
	v, subs, err := decodeSnapshot(
		"name libftdi version 0.20 revision 0 epoch 1 categories devel subports {libftdi0 libftdi1}")
	require.NoError(t, err)
	require.Equal(t, "libftdi", v.Name)
	require.Equal(t, "0.20", v.Version)
	require.Equal(t, "1", v.Epoch)
	require.Equal(t, []string{"devel"}, v.Categories)
	require.Equal(t, []string{"libftdi0", "libftdi1"}, subs)
}

func TestDecodeSnapshotNoSubports(t *testing.T) {
	v, subs, err := decodeSnapshot("name ivy version 0.4.0 categories math")
	require.NoError(t, err)
	require.Equal(t, "ivy", v.Name)
	require.Nil(t, subs)
}

func TestDecodeSnapshotLicenseAndDepends(t *testing.T) {
	v, _, err := decodeSnapshot(
		"name x license {Apache-2 {LGPL-2.1 GPL-2}} " +
			"depends_build {port:pkgconfig} " +
			"depends_lib {port:zlib path:lib/pkgconfig/libusb-1.0.pc:libusb}")
	require.NoError(t, err)
	require.Equal(t, []string{"Apache-2", "LGPL-2.1 GPL-2"}, v.License)
	require.Equal(t, []string{"port:pkgconfig"}, v.Depends.Build)
	require.Equal(t, []string{"port:zlib", "path:lib/pkgconfig/libusb-1.0.pc:libusb"}, v.Depends.Lib)
	require.Nil(t, v.Depends.Run)
}

func TestDecodeSnapshotWorkerOptionFields(t *testing.T) {
	v, _, err := decodeSnapshot(
		"name x maintainers {{@alice example.com:alice} openmaintainer} " +
			"platforms darwin " +
			"distfiles {foo-1.0.tar.gz} " +
			"checksums {rmd160 abc sha256 def size 123}")
	require.NoError(t, err)
	require.Equal(t, []string{"@alice example.com:alice", "openmaintainer"}, v.Maintainers)
	require.Equal(t, []string{"darwin"}, v.Platforms)
	require.Equal(t, []string{"foo-1.0.tar.gz"}, v.Distfiles)
	require.Equal(t, []string{"rmd160", "abc", "sha256", "def", "size", "123"}, v.Checksums)
}

func TestDecodeSnapshotBracedValues(t *testing.T) {
	v, _, err := decodeSnapshot("name x categories {devel test} version {1 2}")
	require.NoError(t, err)
	require.Equal(t, []string{"devel", "test"}, v.Categories)
	require.Equal(t, "1 2", v.Version)
}

func TestDecodeSnapshotMalformedReply(t *testing.T) {
	_, _, err := decodeSnapshot("name x version") // odd length
	require.Error(t, err)
	_, _, err = decodeSnapshot("name x categories {unterminated")
	require.Error(t, err)
}
