package provision

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveRecoveryPartitionUpdatesBothGPTCopies(t *testing.T) {
	path := writeGPTFixture(t, true)
	removed, err := removeRecoveryPartition(path)
	require.NoError(t, err)
	require.True(t, removed)
	removed, err = removeRecoveryPartition(path)
	require.NoError(t, err)
	require.False(t, removed)

	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	primary, err := readGPTHeader(file, gptSectorSize)
	require.NoError(t, err)
	backup, err := readGPTHeader(file, int64(primary.alternateLBA)*gptSectorSize)
	require.NoError(t, err)
	entries := make([]byte, int(primary.entryCount*primary.entrySize))
	backupEntries := make([]byte, len(entries))
	_, err = file.ReadAt(entries, int64(primary.entriesLBA)*gptSectorSize)
	require.NoError(t, err)
	_, err = file.ReadAt(backupEntries, int64(backup.entriesLBA)*gptSectorSize)
	require.NoError(t, err)
	require.Equal(t, entries, backupEntries)
	require.Equal(t, crc32.ChecksumIEEE(entries), primary.entriesCRC)
	require.Equal(t, crc32.ChecksumIEEE(entries), backup.entriesCRC)
	require.Equal(t, make([]byte, 128), entries[128:256])
}

func TestRemoveRecoveryPartitionDoesNothingWithoutRecoveryEntry(t *testing.T) {
	path := writeGPTFixture(t, false)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	removed, err := removeRecoveryPartition(path)
	require.NoError(t, err)
	require.False(t, removed)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestRemoveRecoveryPartitionRefusesDisagreeingTables(t *testing.T) {
	path := writeGPTFixture(t, true)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = file.WriteAt([]byte{0xff}, 90*gptSectorSize)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = removeRecoveryPartition(path)
	require.ErrorContains(t, err, "backup GPT entry checksum is invalid")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func writeGPTFixture(t *testing.T, recovery bool) string {
	t.Helper()
	const sectors = 100
	data := make([]byte, sectors*gptSectorSize)
	entries := make([]byte, 4*128)
	entries[0] = 1
	if recovery {
		copy(entries[128:144], apfsRecoveryType[:])
	}
	copy(data[2*gptSectorSize:], entries)
	copy(data[90*gptSectorSize:], entries)
	entriesCRC := crc32.ChecksumIEEE(entries)
	writeHeader := func(lba, alternate, entriesLBA uint64) {
		sector := data[int(lba)*gptSectorSize : int(lba+1)*gptSectorSize]
		copy(sector, "EFI PART")
		binary.LittleEndian.PutUint32(sector[8:12], 0x00010000)
		binary.LittleEndian.PutUint32(sector[12:16], 96)
		binary.LittleEndian.PutUint64(sector[24:32], lba)
		binary.LittleEndian.PutUint64(sector[32:40], alternate)
		binary.LittleEndian.PutUint64(sector[72:80], entriesLBA)
		binary.LittleEndian.PutUint32(sector[80:84], 4)
		binary.LittleEndian.PutUint32(sector[84:88], 128)
		binary.LittleEndian.PutUint32(sector[88:92], entriesCRC)
		copy(sector[92:96], []byte{1, 2, 3, 4})
		binary.LittleEndian.PutUint32(sector[16:20], crc32.ChecksumIEEE(sector[:96]))
	}
	writeHeader(1, 99, 2)
	writeHeader(99, 1, 90)
	path := t.TempDir() + "/disk.img"
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}
