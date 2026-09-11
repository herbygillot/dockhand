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

    proc ping {} { return pong }
    register ping ::tclrpc::ping

    proc evalop {script} { uplevel #0 $script }
    register eval ::tclrpc::evalop
}
