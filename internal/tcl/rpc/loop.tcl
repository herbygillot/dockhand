# The Session dispatch loop. Loaded into a bare tclsh by rpc.Session —
# this file is the protocol's Tcl half and is owned by that type, not by
# the process primitive.
#
# Wire format, both directions, designed so that neither side ever quotes
# or escapes anything:
#
#   request:  "CALL <nargs>\n" then per argument "<bytelen>\n<bytes>\n"
#   reply:    "TCLRPC1 ok|err <bytelen>\n<bytes>\n"
#
# Channels run in binary mode; arguments and payloads are UTF-8, converted
# explicitly at this boundary so byte counts are exact. Replies are located
# by the sentinel, so anything a handler (or the code it calls) prints to
# stdout between frames is skippable noise rather than corruption.
#
# Handlers are registered in ::tclrpc::ops; init scripts supplied by the
# Session's creator add theirs before the loop starts.

namespace eval ::tclrpc {
    variable ops [dict create]

    proc register {name cmd} {
        variable ops
        dict set ops $name $cmd
    }

    proc reply {status payload} {
        set bytes [encoding convertto utf-8 $payload]
        puts stdout "TCLRPC1 $status [string length $bytes]"
        puts -nonewline stdout $bytes
        puts stdout ""
        flush stdout
    }

    proc loop {} {
        variable ops
        fconfigure stdin -translation binary
        fconfigure stdout -translation binary
        while {[gets stdin header] >= 0} {
            if {![string match "CALL *" $header]} continue
            set n [lindex $header 1]
            set argv {}
            for {set i 0} {$i < $n} {incr i} {
                gets stdin len
                lappend argv [encoding convertfrom utf-8 [read stdin $len]]
                read stdin 1
            }
            if {[llength $argv] == 0} {
                reply err "empty call"
                continue
            }
            set op [lindex $argv 0]
            if {![dict exists $ops $op]} {
                reply err "unknown op: $op"
                continue
            }
            if {[catch {{*}[dict get $ops $op] {*}[lrange $argv 1 end]} result]} {
                reply err $result
            } else {
                reply ok $result
            }
        }
    }

    # Built-in ops: liveness, and evaluation of arbitrary Tcl — which is
    # what a differential-testing consumer is for.
    proc ping {} { return pong }
    register ping ::tclrpc::ping

    proc evalop {script} { uplevel #0 $script }
    register eval ::tclrpc::evalop
}
# The loop is started by the Session after init scripts have run, so their
# registrations land before the first frame is read.
