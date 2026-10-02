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
