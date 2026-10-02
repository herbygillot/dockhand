package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A CMakeLists.txt's options, with their defaults, and the packages it
// finds, with the if() conditions they're found under, are read as CMake
// writes its commands: names in any case, arguments quoted or bracketed,
// comments passed over (the fluent-bit run, batch 23).
func TestACMakeListsOptionsAndPackagesAreRead(t *testing.T) {
	facts := ReadCMake([]byte(`cmake_minimum_required(VERSION 3.20)
project(fluent-bit VERSION 5.1.3)
# option(FLB_COMMENTED "not one" ON)
#[[ option(FLB_BRACKETED "nor this" ON) ]]
option(FLB_TLS "Build with TLS" ON)
OPTION(FLB_KAFKA "Kafka output")
option(FLB_PROTOBUF_ENCODER "Protobuf" No)
option(FLB_SHARED "Shared" ${BUILD_SHARED_LIBS})
cmake_dependent_option(FLB_KAFKA_SASL "SASL" ON "FLB_KAFKA" OFF)
find_package(Threads REQUIRED)
if(FLB_TLS AND (NOT FLB_SYSTEM_WINDOWS))
  find_package(OpenSSL 3.0 REQUIRED)
elseif(FLB_MBEDTLS)
  find_package(MbedTLS)
else()
  message(STATUS [=[no "TLS" here]=])
endif()
`))
	require.Equal(t, map[string]CMakeOption{
		"FLB_TLS": {Default: "ON"}, "FLB_KAFKA": {Default: "OFF"}, "FLB_KAFKA_SASL": {Default: "ON", Dependent: true},
		"FLB_PROTOBUF_ENCODER": {Default: "OFF"}, "FLB_SHARED": {Default: "${BUILD_SHARED_LIBS}"},
	}, facts.Options)
	require.Equal(t, []CMakePackage{
		{Name: "Threads", Required: true},
		{Name: "OpenSSL", Version: "3.0", Required: true, Under: []string{"FLB_TLS AND (NOT FLB_SYSTEM_WINDOWS)"}},
		{Name: "MbedTLS", Under: []string{"FLB_MBEDTLS"}},
	}, facts.Packages)
}

// A CMakeLists.txt less the options named, and the if() blocks that gate
// on one alone, nested ones included, with the lines left empty dropped.
func TestCMakeWithoutLeavesOutOptionsAndWhatTheyGate(t *testing.T) {
	text := "project(x)\noption(A \"a\" OFF)\nif(A)\n  find_package(P)\n  if(WIN32)\n    message(x)\n  endif()\nendif()\nif(${A})\n  add_definitions(-DA)\nendif()\nif(B)\n  find_package(Q)\nendif()\nadd_library(x a.c)\n"
	require.Equal(t, "project(x)\nif(B)\n  find_package(Q)\nendif()\nadd_library(x a.c)", string(CMakeWithout([]byte(text), map[string]bool{"A": true}, map[string]bool{"A": true})))
	require.Equal(t, "project(x)\nif(A)\n  find_package(P)\n  if(WIN32)\n    message(x)\n  endif()\nendif()\nif(${A})\n  add_definitions(-DA)\nendif()\nif(B)\n  find_package(Q)\nendif()\nadd_library(x a.c)", string(CMakeWithout([]byte(text), map[string]bool{"A": true}, nil)), "an option on by default gates what the default build reads")
}

// An option is off where it's declared off and nothing turns it on: no
// command but the if()s that test it, and message(), takes it as an
// argument, as set() or a macro like fluent-bit's FLB_OPTION() would, and
// the Portfile doesn't name it.
func TestCMakeOffIsWhatNothingTurnsOn(t *testing.T) {
	text := `option(A "a" OFF)
option(B "b" No)
option(C "c")
option(D "d" OFF)
option(E "e" ON)
option(F "f" OFF)
option(G "g" OFF)
option(G "g again" ON)
option(H "h" OFF)
cmake_dependent_option(H "h" ON "A" OFF)
if(A)
  set(B ON)
endif()
FLB_OPTION(C ON)
if(D AND NOT A)
  message(FATAL_ERROR "D requires A")
endif()
`
	require.Equal(t, map[string]bool{"A": true, "D": true}, CMakeOff([]byte(text), func(option string) bool { return option == "F" }))
}

// What the default build reads of a CMakeLists.txt is each branch whose
// condition isn't false where the options off are OFF: one taken in a cut
// branch's place is left as it's read, an elseif() opening the block and
// an else() no longer conditional. Comments, and the lines left empty, go.
func TestCMakeLessTakesTheBranchesTheDefaultBuildDoes(t *testing.T) {
	off := map[string]bool{"A": true, "B": true}
	for text, want := range map[string]string{
		"if(A)\n  a()\nelse()\n  b()\nendif()\n":                      "  b()",
		"if(A)\n  a()\nelseif(Q)\n  q()\nelse()\n  r()\nendif()\n":    "if(Q)\n  q()\nelse()\n  r()\nendif()",
		"if(Q)\n  q()\nelseif(A)\n  a()\nendif()\n":                   "if(Q)\n  q()\nendif()",
		"if(A OR B)\n  a()\nendif()\nx()\n":                           "x()",
		"if(A AND Q)\n  a()\nendif()\n":                               "",
		"if(A OR Q)\n  a()\nendif()\n":                                "if(A OR Q)\n  a()\nendif()",
		"if(NOT A)\n  a()\nendif()\n":                                 "if(NOT A)\n  a()\nendif()",
		"if((A OR B) AND (Q OR R))\n  a()\nendif()\n":                 "",
		"if(${A})\n  a()\nendif()\n":                                  "",
		"if(0)\n  a()\nendif()\nif(Q STREQUAL A)\n  q()\nendif()\n":   "if(Q STREQUAL A)\n  q()\nendif()",
		"if(Q)\n  if(A)\n    a() # A's\n  endif()\n  q()\nendif()\n":  "if(Q)\n  q()\nendif()",
		"x() # a comment\n#[[ a bracket\ncomment ]]\n\ny(\"#not\")\n": "x()\ny(\"#not\")",
	} {
		require.Equal(t, want, string(CMakeWithout([]byte(text), nil, off)), text)
	}
}

// A CMakeLists.txt that sets its version in parts sets each as the
// version has it: set(FLB_VERSION_PATCH 2) becoming 3 is 5.1.2 becoming
// 5.1.3, and one set to anything else isn't the version.
func TestAVersionSetInPartsIsTheVersion(t *testing.T) {
	require.True(t, DeclaresVersionPart("CMakeLists.txt", "set(FLB_VERSION_PATCH  2)", "set(FLB_VERSION_PATCH  3)", "5.1.2", "5.1.3"))
	require.True(t, DeclaresVersionPart("sub/CMakeLists.txt", `SET(VERSION_MINOR "1")`, `SET(VERSION_MINOR "2")`, "5.1.9", "5.2.0"))
	require.False(t, DeclaresVersionPart("CMakeLists.txt", "set(FLB_VERSION_PATCH 2)", "set(FLB_VERSION_PATCH 4)", "5.1.2", "5.1.3"))
	require.False(t, DeclaresVersionPart("CMakeLists.txt", "set(FLB_VERSION_PATCH 2)", "set(OTHER_VERSION_PATCH 3)", "5.1.2", "5.1.3"))
	require.False(t, DeclaresVersionPart("meson.build", "set(FLB_VERSION_PATCH 2)", "set(FLB_VERSION_PATCH 3)", "5.1.2", "5.1.3"))
	require.Equal(t, "set(A_VERSION_MAJOR 5)\nset(A_VERSION_PATCH 3)\nset(A_VERSION_TWEAK 7)", VersionPartsAs("set(A_VERSION_MAJOR 5)\nset(A_VERSION_PATCH 2)\nset(A_VERSION_TWEAK 7)", "5.1.2", "5.1.3"))
}
