package provision

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildImage writes a minimal valid GPT: primary header, three
// entries (ISC, APFS, Recovery), backup table and header.
func buildImage(t *testing.T, withRecovery bool) string {
	t.Helper()
	const sectors = 128
	img := make([]byte, sectors*sectorSize)
	apfsGUID := []byte{0xEF, 0x57, 0x34, 0x7C, 0x00, 0x00, 0xAA, 0x11, 0xAA, 0x11, 0x00, 0x30, 0x65, 0x43, 0xEC, 0xAC}

	entries := make([]byte, 128*128)
	writeEntry := func(i int, guid []byte, first, last uint64) {
		copy(entries[i*128:], guid)
		binary.LittleEndian.PutUint64(entries[i*128+32:], first)
		binary.LittleEndian.PutUint64(entries[i*128+40:], last)
	}
	writeEntry(0, apfsGUID, 40, 50)
	writeEntry(1, apfsGUID, 51, 80)
	if withRecovery {
		writeEntry(2, recoveryTypeGUID, 81, 90)
	}
	entriesCRC := crc32.ChecksumIEEE(entries)

	altLBA := uint64(sectors - 1)
	backupEntriesLBA := altLBA - 32
	header := func(myLBA, otherLBA, entriesAt uint64) []byte {
		h := make([]byte, 92)
		copy(h, "EFI PART")
		binary.LittleEndian.PutUint32(h[8:], 0x00010000)
		binary.LittleEndian.PutUint32(h[12:], 92)
		binary.LittleEndian.PutUint64(h[24:], myLBA)
		binary.LittleEndian.PutUint64(h[32:], otherLBA)
		binary.LittleEndian.PutUint64(h[72:], entriesAt)
		binary.LittleEndian.PutUint32(h[80:], 128)
		binary.LittleEndian.PutUint32(h[84:], 128)
		binary.LittleEndian.PutUint32(h[88:], entriesCRC)
		binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(h))
		return h
	}
	copy(img[sectorSize:], header(1, altLBA, 2))
	copy(img[2*sectorSize:], entries)
	copy(img[backupEntriesLBA*sectorSize:], entries)
	copy(img[altLBA*sectorSize:], header(altLBA, 1, backupEntriesLBA))

	path := filepath.Join(t.TempDir(), "disk.img")
	require.NoError(t, os.WriteFile(path, img, 0o644))
	return path
}

func TestRemoveRecoveryPartitionEditsBothTablesAndCRCs(t *testing.T) {
	path := buildImage(t, true)
	removed, err := removeRecoveryPartition(path)
	require.NoError(t, err)
	assert.True(t, removed)

	img, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, entriesOff := range []int{2 * sectorSize, (128 - 1 - 32) * sectorSize} {
		entries := img[entriesOff : entriesOff+128*128]
		assert.False(t, bytes.Contains(entries, recoveryTypeGUID), "recovery entry gone at %d", entriesOff)
		assert.Equal(t, crc32.ChecksumIEEE(entries), binary.LittleEndian.Uint32(img[sectorSize+88:]),
			"entries CRC matches the rewritten table")
	}
	// Header CRCs verify with their own field zeroed.
	for _, hdrOff := range []int{sectorSize, (128 - 1) * sectorSize} {
		h := append([]byte(nil), img[hdrOff:hdrOff+92]...)
		want := binary.LittleEndian.Uint32(h[16:])
		binary.LittleEndian.PutUint32(h[16:], 0)
		assert.Equal(t, want, crc32.ChecksumIEEE(h), "header CRC at %d", hdrOff)
	}

	// Idempotent: a second pass finds nothing and touches nothing.
	removed, err = removeRecoveryPartition(path)
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestRemoveRecoveryPartitionRefusesNonGPT(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	_, err := removeRecoveryPartition(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GPT")
}

func TestRemoveRecoveryPartitionNoRecoveryIsFine(t *testing.T) {
	removed, err := removeRecoveryPartition(buildImage(t, false))
	require.NoError(t, err)
	assert.False(t, removed)
}
