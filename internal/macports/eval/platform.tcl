namespace eval ::dockhand {
    variable platform_script {
        namespace eval ::dockhand_platform {
            variable operands {}
            variable events {}
            variable recording 0
            proc capture {name value} {
                variable events
                set frames {}
                for {set i 1} {$i < [info frame]} {incr i} {
                    set frame [info frame $i]
                    set file ""
                    if {[dict exists $frame file]} { set file [dict get $frame file] }
                    lappend frames [list $file [dict get $frame line] [dict get $frame cmd]]
                }
                lappend events [list [list dockhand.operand $name $value] $frames]
            }
            proc scalar {name index op} {
                variable recording
                if {$recording || $index ne ""} { return }
                set recording 1
                try {
                    # Do not invoke a PortGroup's lazy option getter to observe a write.
                    foreach entry [uplevel #0 [list trace info variable $name]] {
                        if {"read" in [lindex $entry 0]} { return }
                    }
                    capture [string trimleft $name :] [uplevel #0 [list set $name]]
                } finally { set recording 0 }
            }
            proc option_result {cmd code result op} {
                variable operands
                if {$code == 0 && [llength $cmd] == 2} {
                    set name option:[lindex $cmd 1]
                    if {$name in $operands} { capture $name $result }
                }
            }
            proc ready {} {
                variable operands
                foreach name $operands {
                    if {![string match option:* $name]} {
                        trace add variable ::$name write ::dockhand_platform::scalar
                    }
                }
                trace add execution ::option leave ::dockhand_platform::option_result
            }
        }
    }
}
