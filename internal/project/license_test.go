package project

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A license file's text is classified by the licenses it is, as
// licensecheck finds them, with how much of the text they cover: its
// copyright line isn't the license, and MIT with its notice clause
// dropped is MIT-0.
func TestALicenseFileIsClassifiedByItsText(t *testing.T) {
	mit := File{Data: []byte("Copyright (c) 2016-2026 Kenneth Shaw\n\n" + testsupport.MITText)}.License()
	require.Equal(t, []string{"MIT"}, mit.IDs)
	require.True(t, mit.Known())
	require.Equal(t, "MIT", mit.String())

	both := File{Data: []byte("Copyright (c) 2020 A\n\n" + testsupport.MITText + "\nThe compat/ directory:\n\n" + testsupport.ISCText)}.License()
	require.Equal(t, "MIT and ISC", both.String())
	require.True(t, both.Known())
	require.True(t, both.Same(LicenseText{IDs: []string{"ISC", "MIT"}}))
	require.False(t, both.Same(mit))

	dropped := File{Data: []byte(strings.Replace(testsupport.MITText, "The above copyright notice and this permission notice shall be included in all\ncopies or substantial portions of the Software.\n", "", 1))}.License()
	require.Equal(t, "MIT-0", dropped.String(), "MIT without its notice clause is MIT-0, another license")

	notice := File{Data: []byte("This project is licensed under the terms of the Apache License 2.0 or MIT,\nand includes the works of many people listed in AUTHORS, who keep their rights.\n")}.License()
	require.False(t, notice.Known(), "a notice naming licenses says more than they do")
	require.False(t, File{Data: []byte(testsupport.MITText), Truncated: true}.License().Known(), "what wasn't read whole isn't classified")
}

// An SPDX expression is read as its specification has it, through
// go-spdx: normalized, or refused where it isn't one.
func TestALicenseExpressionIsReadByItsSpecification(t *testing.T) {
	for expression, want := range map[string]string{
		"MIT":                   "MIT",
		"mit or apache-2.0":     "MIT OR Apache-2.0",
		"MIT/Apache-2.0":        "MIT OR Apache-2.0",
		"((MIT))":               "MIT",
		"(MIT OR ISC) AND Zlib": "(MIT OR ISC) AND Zlib",
	} {
		got, ok := LicenseExpression(expression)
		require.True(t, ok, expression)
		require.Equal(t, want, got, expression)
	}
	for _, expression := range []string{"", "Apache 2", "NOASSERTION", "MIT OR", "MIT, Apache-2.0"} {
		_, ok := LicenseExpression(expression)
		require.False(t, ok, expression)
	}
}
