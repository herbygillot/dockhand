namespace eval ::tclrpc {
    variable ops [dict create]

    proc register {name cmd} {
        variable ops
        dict set ops $name $cmd
    }

    # reply frames a result. Tcl 9 refuses to encode a string that is not
    # Unicode, such as a lone surrogate, where 8.6 wrote invalid UTF-8; the
    # refusal becomes the call's error rather than ending the loop.
    proc reply {status payload} {
        if {[catch {encoding convertto utf-8 $payload} bytes]} {
            set status err
            if {[catch {encoding convertto utf-8 "reply is not encodable as UTF-8: $bytes"} bytes]} {
                set bytes "reply is not encodable as UTF-8"
            }
        }
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
            set undecodable 0
            # Every argument is read, so the stream stays framed, before an
            # undecodable one refuses the call: Tcl 9 refuses invalid UTF-8
            # that 8.6 decoded leniently.
            for {set i 0} {$i < $n} {incr i} {
                gets stdin len
                set raw [read stdin $len]
                read stdin 1
                if {[catch {encoding convertfrom utf-8 $raw} value]} {
                    set undecodable 1
                } else {
                    lappend argv $value
                }
            }
            if {$undecodable} {
                reply err "call argument is not valid UTF-8"
                continue
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

    proc ping {} { return pong }
    register ping ::tclrpc::ping

    proc evalop {script} { uplevel #0 $script }
    register eval ::tclrpc::evalop
}
