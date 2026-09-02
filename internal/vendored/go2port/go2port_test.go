package go2port

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/vendored"
)

const generatedPortfile = `# -*- coding: utf-8; mode: tcl -*-

PortSystem          1.0
PortGroup           golang 1.0

go.setup            github.com/ericchiang/pup 0.4.0 v
categories          textproc
license             MIT

description         CLI HTML parser
long_description    ${description}

go.vendors          golang.org/x/text \
                        lock    v0.3.2 \
                        rmd160  3b9523084f6a8b2e6a6987e49c56f05e22ad69eb \
                        sha256  d624899dfd390d9d4a77e5c8e5abd8c45f0b6163e0dc7176aee39f25c5f1bed0 \
                        size    7168458 \
                    golang.org/x/net \
                        lock    v0.0.0-20190404232315-eb5bcb51f2a3 \
                        rmd160  6547b831afd7544cf84853c58d6744887e97d4af \
                        sha256  ac404719da5dc2f0fcf63e45e8791173391c13e0b19ce9894d6df664d8e773f2 \
                        size    1234567

checksums           rmd160  0000000000000000000000000000000000000000 \
                    sha256  0000000000000000000000000000000000000000000000000000000000000000 \
                    size    1
`

func TestExtractBlockFindsGoVendors(t *testing.T) {
	block, err := ExtractBlock([]byte(generatedPortfile))
	require.NoError(t, err)
	s := string(block)
	assert.NotEmpty(t, s)
	assert.Contains(t, s, "go.vendors")
	assert.Contains(t, s, "golang.org/x/net")
	assert.Contains(t, s, "size    1234567")
	assert.NotContains(t, s, "go.setup", "only the block, never the rest of the generated portfile")
	assert.NotContains(t, s, "checksums", "the checksums block is the port's own, not the tool's to write")
}

func TestExtractBlockRefusesOutputWithNoBlock(t *testing.T) {
	_, err := ExtractBlock([]byte("PortSystem 1.0\nname foo\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no go.vendors block")
}

// scripted is a finder whose go2port is a shell script with the given
// body, every other lookup answered for real.
func scripted(t *testing.T, body string) *tool.Finder {
	t.Helper()
	path := filepath.Join(t.TempDir(), ToolName)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Go2Port) {
			return path, nil
		}
		return exec.LookPath(name)
	})
}

func TestGenerateNamesTheGeneratorWhenItIsMissing(t *testing.T) {
	absent := tool.NewFinder(func(string) (string, error) { return "", errors.New("absent") })
	_, err := Generate(context.Background(), absent, "example.com/m", "v1.0.0")
	require.ErrorIs(t, err, vendored.ErrNoGenerator)
	assert.Equal(t, "vendored: block generator not found: go2port", err.Error())
}

func TestGenerateWordsAToolFailureWithItsStderr(t *testing.T) {
	tools := scripted(t, "echo 'no such module' >&2\nexit 1\n")
	_, err := Generate(context.Background(), tools, "example.com/m", "v1.0.0")
	require.Error(t, err)
	assert.Equal(t, "vendored: go2port: no such module", err.Error())
}
