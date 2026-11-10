# distributable.tcl answers MacPorts' checker's infoForPort from what
# dockhand hands it, so distributable_lib.tcl, kept verbatim, judges a port
# as the buildbot does without a lookup of its own: each port's
# dependencies, license, installs_libs, and license_noconflict, as a dict
# by name, set for each call.
namespace eval ::dockhand {}
set ::dockhand::license_ports {}

proc infoForPort {portName variantInfo} {
    if {![dict exists $::dockhand::license_ports $portName]} { return {} }
    return [dict get $::dockhand::license_ports $portName]
}

# distributable is check_licenses' verdict on port, [ok message] with ok 1
# where it's distributable, given ports, infoForPort's answers by name; a
# port it couldn't judge, as one ports doesn't hold, is unknown.
proc ::dockhand::distributable {port ports} {
    set ::dockhand::license_ports $ports
    set verdict [check_licenses $port {}]
    if {[llength $verdict] < 2} { return [list unknown "\"$port\" couldn't be judged"] }
    lassign $verdict code message
    return [list [expr {$code == 0}] $message]
}
::tclrpc::register distributable ::dockhand::distributable
