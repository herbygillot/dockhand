package binaryarchive

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"debug/macho"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

// Packages is the site MacPorts' buildbots publish their archives to.
const Packages = "https://packages.macports.org"

// ArchiveName is MacPorts' file name for a port's archive, as its
// buildbots name one: jq-1.8.1_0+universal.darwin_25.arm64.tbz2, the
// variants each with its sign, in name order.
func ArchiveName(port, version, revision string, variants []string, darwin int, arch string) string {
	sorted := slices.Sorted(slices.Values(variants))
	var signed strings.Builder
	for _, variant := range sorted {
		signed.WriteString("+" + variant)
	}
	return fmt.Sprintf("%s-%s_%s%s.darwin_%d.%s.tbz2", port, version, revision, signed.String(), darwin, arch)
}

// URL is where packages.macports.org serves a port's archive.
func URL(port, name string) string { return Packages + "/" + port + "/" + name }

// maxMember is the most of one archive member read for its Mach-O load
// commands; a library past it is passed over.
const maxMember = 512 << 20

// Contents are what a port's archive installs, read from the archive
// alone (Then's item 9): its files, each as the destroot places it under
// the prefix, "/opt/local/bin/jq", MacPorts' own +CONTENTS and the rest
// of its metadata left out, and its dynamic libraries' install names and
// compatibility versions from their LC_ID_DYLIB, as a build's are read
// with otool -l. A bzip2 tar, as MacPorts' tbz2 is.
func Contents(archive io.Reader) (files []string, libraries []model.Library, err error) {
	files, libraries = []string{}, []model.Library{}
	reader := tar.NewReader(bzip2.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, libraries, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("binaryarchive: %w", err)
		}
		name := strings.TrimPrefix(path.Clean("/"+header.Name), "/")
		if name == "" || strings.HasPrefix(name, "+") || header.Typeflag == tar.TypeDir {
			continue
		}
		file := "/" + name
		files = append(files, file)
		if header.Typeflag != tar.TypeReg || header.Size > maxMember {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxMember))
		if err != nil {
			return nil, nil, fmt.Errorf("binaryarchive: %s: %w", file, err)
		}
		if id, ok := libraryID(data); ok {
			id.File = file
			libraries = append(libraries, id)
		}
	}
}

// libraryID is a Mach-O's LC_ID_DYLIB, thin or the first of a universal
// one's, which debug/macho keeps as raw load bytes: its name, at the
// offset the command gives, and its compatibility version, x.y.z from
// the packed word.
func libraryID(data []byte) (model.Library, bool) {
	var file *macho.File
	if fat, err := macho.NewFatFile(bytes.NewReader(data)); err == nil && len(fat.Arches) > 0 {
		file = fat.Arches[0].File
	} else if thin, err := macho.NewFile(bytes.NewReader(data)); err == nil {
		file = thin
	} else {
		return model.Library{}, false
	}
	const lcIDDylib = 0xd
	for _, load := range file.Loads {
		raw := load.Raw()
		if len(raw) < 24 || file.ByteOrder.Uint32(raw) != lcIDDylib {
			continue
		}
		offset := int(file.ByteOrder.Uint32(raw[8:]))
		compatibility := file.ByteOrder.Uint32(raw[20:])
		if offset >= len(raw) {
			return model.Library{}, false
		}
		name, _, _ := bytes.Cut(raw[offset:], []byte{0})
		return model.Library{Name: string(name), Compatibility: packedVersion(compatibility)}, true
	}
	return model.Library{}, false
}

// packedVersion is a Mach-O's packed version word as otool says it:
// 28.0.0 for 0x001c0000.
func packedVersion(v uint32) string {
	return fmt.Sprintf("%d.%d.%d", v>>16, v>>8&0xff, v&0xff)
}
