package provision

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

// The guest's recovery partition sits between the main APFS container
// and any space a disk grow adds, and SIP refuses to erase or move
// recovery-typed partitions anywhere — inside the guest, and even on
// the host against an attached image. A VM base never boots
// recoveryOS (tart cannot automate it; that is exactly why this file
// exists) and a golden never takes the OS updates recovery serves, so
// the automated equivalent of the community's boot-into-recovery
// recipe is this: remove the recovery entry from the image's GPT by
// plain file I/O, before first boot. No hdiutil, no /dev nodes — the
// host's own disks are structurally out of reach of this code.

const sectorSize = 512

// recoveryTypeGUID is Apple_APFS_Recovery
// (52637672-7900-11AA-AA11-00306543ECAC) in GPT's mixed-endian byte
// order.
var recoveryTypeGUID = []byte{
	0x72, 0x76, 0x63, 0x52, 0x00, 0x79, 0xAA, 0x11,
	0xAA, 0x11, 0x00, 0x30, 0x65, 0x43, 0xEC, 0xAC,
}

// diskImagePath names a tart VM's raw disk file.
func diskImagePath(vm string) (string, error) {
	home := os.Getenv("TART_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(h, ".tart")
	}
	return filepath.Join(home, "vms", vm, "disk.img"), nil
}

// removeRecoveryPartition deletes the guest recovery entry from both
// GPT tables of a raw disk image, fixing the CRCs. removed is false
// when the image carries no recovery entry — nothing to do, not an
// error. Every structural expectation is checked before a byte is
// written; anything unexpected refuses.
func removeRecoveryPartition(path string) (removed bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer f.Close() //nolint:errcheck // read-modify-write; Sync below is the real barrier

	hdr := make([]byte, 92)
	if _, err := f.ReadAt(hdr, sectorSize); err != nil {
		return false, err
	}
	if !bytes.Equal(hdr[:8], []byte("EFI PART")) {
		return false, fmt.Errorf("%s carries no GPT where one belongs", path)
	}
	altLBA := binary.LittleEndian.Uint64(hdr[32:])
	entriesLBA := binary.LittleEndian.Uint64(hdr[72:])
	numEntries := binary.LittleEndian.Uint32(hdr[80:])
	entrySize := binary.LittleEndian.Uint32(hdr[84:])
	if entrySize < 128 || entrySize > 4096 || numEntries == 0 || numEntries > 512 {
		return false, fmt.Errorf("%s: implausible GPT geometry (%d entries of %d bytes)", path, numEntries, entrySize)
	}

	entries := make([]byte, int(numEntries)*int(entrySize))
	if _, err := f.ReadAt(entries, int64(entriesLBA)*sectorSize); err != nil {
		return false, err
	}
	found := -1
	for i := 0; i < int(numEntries); i++ {
		if bytes.Equal(entries[i*int(entrySize):i*int(entrySize)+16], recoveryTypeGUID) {
			found = i
			break
		}
	}
	if found < 0 {
		return false, nil
	}
	for i := range entrySize {
		entries[found*int(entrySize)+int(i)] = 0
	}
	entriesCRC := crc32.ChecksumIEEE(entries)

	patchHeader := func(off int64) error {
		h := make([]byte, 92)
		if _, err := f.ReadAt(h, off); err != nil {
			return err
		}
		if !bytes.Equal(h[:8], []byte("EFI PART")) {
			return fmt.Errorf("%s: no GPT header at offset %d", path, off)
		}
		binary.LittleEndian.PutUint32(h[88:], entriesCRC)
		binary.LittleEndian.PutUint32(h[16:], 0)
		binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(h))
		_, err := f.WriteAt(h, off)
		return err
	}

	// Backup table location comes from the backup header's own field.
	bh := make([]byte, 92)
	if _, err := f.ReadAt(bh, int64(altLBA)*sectorSize); err != nil {
		return false, err
	}
	if !bytes.Equal(bh[:8], []byte("EFI PART")) {
		return false, fmt.Errorf("%s: no backup GPT header at LBA %d", path, altLBA)
	}
	backupEntriesLBA := binary.LittleEndian.Uint64(bh[72:])

	if _, err := f.WriteAt(entries, int64(entriesLBA)*sectorSize); err != nil {
		return false, err
	}
	if _, err := f.WriteAt(entries, int64(backupEntriesLBA)*sectorSize); err != nil {
		return false, err
	}
	if err := patchHeader(sectorSize); err != nil {
		return false, err
	}
	if err := patchHeader(int64(altLBA) * sectorSize); err != nil {
		return false, err
	}
	return true, f.Sync()
}
