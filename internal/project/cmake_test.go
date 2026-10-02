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
	facts := ParseCMake([]byte(`cmake_minimum_required(VERSION 3.20)
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
`)).Facts()
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
// on one alone, nested ones included, each statement in a line.
func TestCMakeWithoutLeavesOutOptionsAndWhatTheyGate(t *testing.T) {
	text := "project(x)\noption(A \"a\" OFF)\nif(A)\n  find_package(P)\n  if(WIN32)\n    message(x)\n  endif()\nendif()\nif(${A})\n  add_definitions(-DA)\nendif()\nif(B)\n  find_package(Q)\nendif()\nadd_library(x a.c)\n"
	document := ParseCMake([]byte(text))
	require.Equal(t, "project(x)\nif(B)\nfind_package(Q)\nendif()\nadd_library(x a.c)", StatementsText(document.Without(map[string]bool{"A": true}, map[string]bool{"A": true})))
	require.Equal(t, "project(x)\nif(A)\nfind_package(P)\nif(WIN32)\nmessage(x)\nendif()\nendif()\nif(${A})\nadd_definitions(-DA)\nendif()\nif(B)\nfind_package(Q)\nendif()\nadd_library(x a.c)",
		StatementsText(document.Without(map[string]bool{"A": true}, nil)), "an option on by default gates what the default build reads")
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
	require.Equal(t, map[string]bool{"A": true, "D": true}, ParseCMake([]byte(text)).Off(func(option string) bool { return option == "F" }))
}

// What the default build reads of a CMakeLists.txt is each branch whose
// condition isn't false where the options off are OFF: one taken in a cut
// branch's place is left as it's read, an elseif() opening the block and
// an else() no longer conditional. Comments aren't read.
func TestTheDocumentTakesTheBranchesTheDefaultBuildDoes(t *testing.T) {
	off := map[string]bool{"A": true, "B": true}
	for text, want := range map[string]string{
		"if(A)\n  a()\nelse()\n  b()\nendif()\n":                      "b()",
		"if(A)\n  a()\nelseif(Q)\n  q()\nelse()\n  r()\nendif()\n":    "if(Q)\nq()\nelse()\nr()\nendif()",
		"if(Q)\n  q()\nelseif(A)\n  a()\nendif()\n":                   "if(Q)\nq()\nendif()",
		"if(A OR B)\n  a()\nendif()\nx()\n":                           "x()",
		"if(A AND Q)\n  a()\nendif()\n":                               "",
		"if(A OR Q)\n  a()\nendif()\n":                                "if(A OR Q)\na()\nendif()",
		"if(NOT A)\n  a()\nendif()\n":                                 "if(NOT A)\na()\nendif()",
		"if((A OR B) AND (Q OR R))\n  a()\nendif()\n":                 "",
		"if(${A})\n  a()\nendif()\n":                                  "",
		"if(0)\n  a()\nendif()\nif(Q STREQUAL A)\n  q()\nendif()\n":   "if(Q STREQUAL A)\nq()\nendif()",
		"if(Q)\n  if(A)\n    a() # A's\n  endif()\n  q()\nendif()\n":  "if(Q)\nq()\nendif()",
		"x() # a comment\n#[[ a bracket\ncomment ]]\n\ny(\"#not\")\n": "x()\ny(#not)",
		"if(\n  A\n)\n  a()\nendif()\nx(\"two words\")\n":             "x(\"two words\")",
	} {
		require.Equal(t, want, StatementsText(ParseCMake([]byte(text)).Without(nil, off)), text)
	}
}

// A statement carries the conditions it's under, as the document reads
// them, one identity whether a block is cut as unreached or named where
// a change is: a multiline if(), an elseif(), and an else(), the branch
// before it negated (the architecture review's finding 3).
func TestAStatementKnowsTheBlocksItsWithin(t *testing.T) {
	document := ParseCMake([]byte("if(\n  DEMO\n  AND NOT WIN32\n)\n  a()\nelseif(${OTHER} STREQUAL x)\n  b()\nelse()\n  c()\nendif()\nd()\n"))
	under := map[string][]CMakeCondition{}
	for _, statement := range document.Statements(nil, nil) {
		under[statement.Text] = statement.Under
	}
	demo := CMakeCondition{Text: "DEMO AND NOT WIN32", Names: []string{"DEMO", "WIN32"}}
	other := CMakeCondition{Text: "${OTHER} STREQUAL x", Names: []string{"OTHER", "x"}}
	require.Equal(t, []CMakeCondition{demo}, under["a()"])
	require.Equal(t, []CMakeCondition{other}, under["b()"])
	require.Equal(t, []CMakeCondition{{Text: "NOT (${OTHER} STREQUAL x)", Names: []string{"OTHER", "x"}, Else: true}}, under["c()"])
	require.Empty(t, under["d()"])
	require.True(t, demo.Tests("DEMO"))
	require.False(t, under["c()"][0].Tests("OTHER"), "an else() negates what it tests")
}

// A file the CMakeLists.txt include()s is read in its place, by its path
// or as a module on the module path, so an option it declares or sets is
// the document's: fluent-bit keeps its plugins' options in
// cmake/plugins_options.cmake. One built from another variable, and
// CMake's own modules, aren't found; a cycle stops.
func TestADocumentReadsWhatItIncludes(t *testing.T) {
	reading := Reading{Files: map[string]File{
		"CMakeLists.txt":              {Data: []byte("list(APPEND CMAKE_MODULE_PATH \"${CMAKE_CURRENT_SOURCE_DIR}/cmake\")\ninclude(cmake/plugins_options.cmake)\ninclude(Helpers)\ninclude(GNUInstallDirs)\ninclude(${GENERATED}/x.cmake)\noption(B \"b\" OFF)\n")},
		"cmake/plugins_options.cmake": {Data: []byte("option(A \"a\" OFF)\ninclude(${CMAKE_CURRENT_LIST_DIR}/more.cmake)\n")},
		"cmake/more.cmake":            {Data: []byte("set(B ON)\ninclude(${CMAKE_CURRENT_LIST_DIR}/more.cmake)\n")},
		"cmake/Helpers.cmake":         {Data: []byte("option(C \"c\" ON)\n")},
	}}
	document := reading.CMakeDocument("CMakeLists.txt")
	require.Equal(t, []string{"cmake/plugins_options.cmake", "cmake/more.cmake", "cmake/Helpers.cmake"}, document.Included())
	require.Equal(t, map[string]CMakeOption{"A": {Default: "OFF"}, "B": {Default: "OFF"}, "C": {Default: "ON"}}, document.Facts().Options)
	require.Equal(t, map[string]bool{"A": true}, document.Off(func(string) bool { return false }), "B is set in a file included")
	var files []string
	for _, statement := range document.Statements(nil, nil) {
		files = append(files, statement.File+": "+statement.Text)
	}
	require.Equal(t, []string{
		`: list(APPEND CMAKE_MODULE_PATH ${CMAKE_CURRENT_SOURCE_DIR}/cmake)`, ": include(cmake/plugins_options.cmake)",
		"cmake/plugins_options.cmake: option(A a OFF)", "cmake/plugins_options.cmake: include(${CMAKE_CURRENT_LIST_DIR}/more.cmake)",
		"cmake/more.cmake: set(B ON)", "cmake/more.cmake: include(${CMAKE_CURRENT_LIST_DIR}/more.cmake)",
		": include(Helpers)", "cmake/Helpers.cmake: option(C c ON)", ": include(GNUInstallDirs)", ": include(${GENERATED}/x.cmake)", ": option(B b OFF)",
	}, files, "each included file's commands in its place")
	require.Empty(t, ParseCMake(reading.Files["CMakeLists.txt"].Data).Included(), "alone, it follows nothing")
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
