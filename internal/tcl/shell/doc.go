// Package shell provides tclsh processes.
//
// Proc is the absolute primitive: a long-lived tclsh child with honest
// access to its pipes and lifecycle. It embeds no Tcl and speaks no
// protocol; what flows over stdin and stdout is the caller's business.
//
// Nothing here is specific to dockhand or MacPorts. The framed
// request/response discipline dockhand actually converses in is the rpc
// package, layered on Proc through its exported API alone.
package shell
