package main

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestUnperturbedClosedReadsUseEventsAndCountCallsOnce(t *testing.T) {
	event := func(source, call string) macports.Declaration {
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{"host question", source, "base", call}}
	}
	p := macports.PortObservation{Declarations: []macports.Declaration{
		event("closed", "sysctl hw.optional.arm64"), event("closed", "::sysctl {hw.optional.arm64}"),
		event("closed", "sysctl hw.cpu64bit_capable"), event("closed", "sysctl sysctl.proc_translated"),
		event("closed", "exec uname -m"), event("closed", "exec uname -m"),
		event("closed", "glob -nocomplain /host/*"), event("closed", "file exists /host/header"),
		event("standin", "sysctl hw.activecpu"), event("standin", "file exists /dockhand-standin/bin/make"),
		{Command: "dockhand.host-access", Values: []string{"native question"}},
		{Command: "checksums", Values: []string{"ignored", "closed", "base", "exec false"}},
	}, Ledger: []macports.LedgerEntry{{Command: "sysctl", Source: "closed", Subject: "not-an-event", Count: 10}}}
	require.Equal(t, []string{"exec uname -m", "file exists /host/header", "glob -nocomplain /host/*", "sysctl hw.cpu64bit_capable", "sysctl hw.optional.arm64", "sysctl sysctl.proc_translated"}, unperturbedClosedReads(p))
}
