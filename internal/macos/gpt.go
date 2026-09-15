package macos

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

const gptSectorSize = 512

var apfsRecoveryType = [16]byte{
	0x72, 0x76, 0x63, 0x52, 0x00, 0x79, 0xaa, 0x11,
	0xaa, 0x11, 0x00, 0x30, 0x65, 0x43, 0xec, 0xac,
}

type gptHeader struct {
	offset       int64
	headerSize   uint32
	currentLBA   uint64
	alternateLBA uint64
	entriesLBA   uint64
	entryCount   uint32
	entrySize    uint32
	entriesCRC   uint32
	sector       []byte
}

// RemoveRecoveryPartition removes the APFS recovery entry from a raw disk image.
// The caller must ensure the image is offline and exclusively held during modification.
func RemoveRecoveryPartition(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer file.Close()

	primary, err := readGPTHeader(file, gptSectorSize)
	if err != nil {
		return false, fmt.Errorf("%s: primary GPT: %w", path, err)
	}
	if primary.entryCount == 0 || primary.entryCount > 512 || primary.entrySize < 128 || primary.entrySize > 4096 {
		return false, fmt.Errorf("%s: implausible GPT entry geometry", path)
	}
	entriesLength := uint64(primary.entryCount) * uint64(primary.entrySize)
	if entriesLength > 2<<20 {
		return false, fmt.Errorf("%s: GPT entry table is too large", path)
	}
	entries := make([]byte, int(entriesLength))
	if _, err := file.ReadAt(entries, int64(primary.entriesLBA)*gptSectorSize); err != nil {
		return false, err
	}
	if crc32.ChecksumIEEE(entries) != primary.entriesCRC {
		return false, fmt.Errorf("%s: primary GPT entry checksum is invalid", path)
	}
	recoveryIndex := -1
	for index := 0; index < int(primary.entryCount); index++ {
		start := index * int(primary.entrySize)
		if bytes.Equal(entries[start:start+len(apfsRecoveryType)], apfsRecoveryType[:]) {
			if recoveryIndex >= 0 {
				return false, fmt.Errorf("%s: more than one APFS recovery partition", path)
			}
			recoveryIndex = index
		}
	}
	if recoveryIndex < 0 {
		return false, nil
	}
	backup, err := readGPTHeader(file, int64(primary.alternateLBA)*gptSectorSize)
	if err != nil {
		return false, fmt.Errorf("%s: backup GPT: %w", path, err)
	}
	if primary.currentLBA != 1 || backup.currentLBA != primary.alternateLBA || backup.alternateLBA != primary.currentLBA || backup.entryCount != primary.entryCount || backup.entrySize != primary.entrySize {
		return false, fmt.Errorf("%s: primary and backup GPT geometry disagree", path)
	}
	backupEntries := make([]byte, len(entries))
	if _, err := file.ReadAt(backupEntries, int64(backup.entriesLBA)*gptSectorSize); err != nil {
		return false, err
	}
	if crc32.ChecksumIEEE(backupEntries) != backup.entriesCRC {
		return false, fmt.Errorf("%s: backup GPT entry checksum is invalid", path)
	}
	if !bytes.Equal(entries, backupEntries) {
		return false, fmt.Errorf("%s: primary and backup GPT entry tables disagree", path)
	}

	start := recoveryIndex * int(primary.entrySize)
	clear(entries[start : start+int(primary.entrySize)])
	entriesCRC := crc32.ChecksumIEEE(entries)
	patchGPTHeader(primary, entriesCRC)
	patchGPTHeader(backup, entriesCRC)
	if _, err := file.WriteAt(entries, int64(backup.entriesLBA)*gptSectorSize); err != nil {
		return false, err
	}
	if _, err := file.WriteAt(backup.sector, backup.offset); err != nil {
		return false, err
	}
	if _, err := file.WriteAt(entries, int64(primary.entriesLBA)*gptSectorSize); err != nil {
		return false, err
	}
	if _, err := file.WriteAt(primary.sector, primary.offset); err != nil {
		return false, err
	}
	return true, file.Sync()
}

func readGPTHeader(file *os.File, offset int64) (gptHeader, error) {
	sector := make([]byte, gptSectorSize)
	if _, err := file.ReadAt(sector, offset); err != nil {
		return gptHeader{}, err
	}
	if !bytes.Equal(sector[:8], []byte("EFI PART")) {
		return gptHeader{}, fmt.Errorf("signature is missing")
	}
	headerSize := binary.LittleEndian.Uint32(sector[12:16])
	if headerSize < 92 || headerSize > gptSectorSize {
		return gptHeader{}, fmt.Errorf("invalid header size %d", headerSize)
	}
	storedCRC := binary.LittleEndian.Uint32(sector[16:20])
	header := append([]byte(nil), sector[:headerSize]...)
	clear(header[16:20])
	if crc32.ChecksumIEEE(header) != storedCRC {
		return gptHeader{}, fmt.Errorf("header checksum is invalid")
	}
	return gptHeader{
		offset:       offset,
		headerSize:   headerSize,
		currentLBA:   binary.LittleEndian.Uint64(sector[24:32]),
		alternateLBA: binary.LittleEndian.Uint64(sector[32:40]),
		entriesLBA:   binary.LittleEndian.Uint64(sector[72:80]),
		entryCount:   binary.LittleEndian.Uint32(sector[80:84]),
		entrySize:    binary.LittleEndian.Uint32(sector[84:88]),
		entriesCRC:   binary.LittleEndian.Uint32(sector[88:92]),
		sector:       sector,
	}, nil
}

func patchGPTHeader(header gptHeader, entriesCRC uint32) {
	binary.LittleEndian.PutUint32(header.sector[88:92], entriesCRC)
	clear(header.sector[16:20])
	binary.LittleEndian.PutUint32(header.sector[16:20], crc32.ChecksumIEEE(header.sector[:header.headerSize]))
}
