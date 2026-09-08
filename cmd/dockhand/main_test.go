package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A BINARY THAT CANNOT NAME ITSELF SIGNS OFF ON PULL REQUESTS ANYWAY.
// publish.Env.Version is carried into every body dockhand opens "so that
// a published sentence found to be wrong can be traced to the build that
// wrote it", and the field measured every one of them going upstream as
// "dockhand 0.0.0-dev". The placeholder is the one value that must not
// survive when the toolchain knows the commit.
func TestThePlaceholderNeverSurvivesAKnownCommit(t *testing.T) {
	assert.Equal(t, "4a1cbe9f0d2e", fold(placeholder, "4a1cbe9f0d2e5b7c", false))
	assert.Equal(t, "4a1cbe9f0d2e-dirty", fold(placeholder, "4a1cbe9f0d2e5b7c", true))
}

// AND A LINKED VERSION IS KEPT, with the commit beside it. A tagged
// build one dirty commit ahead of its tag is both facts, and a reader
// tracing a wrong sentence needs the commit more than the tag.
func TestALinkedVersionKeepsItsNameAndGainsTheCommit(t *testing.T) {
	assert.Equal(t, "v1.2.0 (4a1cbe9f0d2e)", fold("v1.2.0", "4a1cbe9f0d2e5b7c", false))
}

// A DESCRIBE THAT ALREADY CARRIES THE COMMIT IS NOT MADE TO CARRY IT
// TWICE. `git describe --dirty` spells the same sha the toolchain does.
func TestAVersionThatAlreadyNamesTheCommitIsLeftAlone(t *testing.T) {
	assert.Equal(t, "v1.2.0-3-g4a1cbe9f0d2e", fold("v1.2.0-3-g4a1cbe9f0d2e", "4a1cbe9f0d2e5b7c", false))
}

// AND A BUILD WITH NO VCS ANSWER KEEPS WHAT IT WAS GIVEN — including the
// placeholder, because inventing a commit would be worse than admitting
// there is none (rule 7).
func TestNoVCSAnswerLeavesTheVersionAsLinked(t *testing.T) {
	assert.Equal(t, placeholder, fold(placeholder, "", false))
	assert.Equal(t, "v1.2.0", fold("v1.2.0", "", true))
}
