# Included only in surveymeasure builds, and installed only for the fresh
# session selected by WithStandinAnswers. The normal dispatcher is unchanged.
namespace eval ::dockhand_survey {
    variable worker_script {
        namespace eval ::dockhand_dispatcher {
            proc standin_answer {name arguments} {
                switch -- $name {
                    sysctl {
                        if {[llength $arguments] == 1 && [lindex $arguments 0] in {
                            hw.activecpu hw.ncpu hw.logicalcpu hw.logicalcpu_max
                            hw.physicalcpu hw.physicalcpu_max hw.memsize hw.memsize_usable hw.pagesize
                        }} { return [list 0 1099511627776 {-code 0}] }
                    }
                    file {
                        lassign $arguments subcommand path
                        if {$subcommand in {exists isfile isdirectory readable writable executable owned}
                            && [string first /dockhand-standin/ [file normalize $path]] == 0} {
                            return [list 0 1 {-code 0}]
                        }
                    }
                }
                return ""
            }
            # Stand-ins carry their own provenance in events, never closed.
            proc standin_event {skip problem call} {
                if {![namespace exists ::dockhand_observation]} { return }
                set frames [frames $skip]
                set owner base
                foreach frame $frames {
                    set file [lindex $frame 0]
                    if {[string match */Portfile $file]} { set owner portfile; break }
                    if {[string match */_resources/port1.0/group/* $file]} { set owner portgroup }
                }
                lappend ::dockhand_observation::events [list [list dockhand.host-access $problem standin $owner $call] $frames]
            }
            rename closed_answer survey_original_answer
            proc closed_answer {name arguments} {
                set answer [standin_answer $name $arguments]
                if {$answer ne ""} { return $answer }
                return [survey_original_answer $name $arguments]
            }
            rename shut survey_original_shut
            proc shut {name arguments asked {skip 4}} {
                set answer [standin_answer $name $arguments]
                if {$answer eq ""} { return [survey_original_shut $name $arguments $asked [expr {$skip + 1}]] }
                variable recording
                set recording 1
                try { set problem [host_path $name $arguments] } finally { set recording 0 }
                if {$problem eq ""} { return "" }
                variable ledger
                variable record_paths
                variable walking
                dict incr ledger [list $name [expr {$name eq "file" ? [lindex $arguments 0] : ""}] standin [expr {$walking ? "fingerprint" : ""}]]
                if {$record_paths} { note $name $asked standin }
                if {!$walking} { standin_event $skip $problem [string range [list $name {*}$asked] 0 199] }
                return $answer
            }
            # Preserve lookup's catch/return protocol, replacing only a
            # lookup that the empty Mac actually closed, not table answers.
            proc lookup {name subject where body} {
                variable passed
                variable depth
                if {$depth == 0} { set passed 0 }
                incr depth
                try { set code [catch {uplevel 1 $body} result options] } finally { incr depth -1 }
                if {$code == 2 && [dict get $options -level] == 1} { set code [dict get $options -code] }
                if {$depth == 0 && $passed && ($code == 1 || $result in {"" 0})} {
                    set tail [file tail $subject]
                    if {$name in {findBinary binaryInPath _mportsearchpath} && $tail ni {"" . ..}} {
                        set result /dockhand-standin/bin/$tail
                        if {$name eq "_mportsearchpath" && ![uplevel 1 {set return_match}]} { set result 1 }
                        set code 0
                        set options {-code 0 -level 0}
                        variable ledger
                        variable walking
                        dict incr ledger [list $name "" standin [expr {$walking ? "fingerprint" : $subject}]]
                        if {!$walking} {
                            standin_event 4 "modeled context depends on $subject, which $name looks for $where" [string range [list $name $subject] 0 199]
                        }
                    } else {
                        closed_lookup $name $subject "modeled context depends on $subject, which $name looks for $where" $code $result
                    }
                }
                dict incr options -level
                return -options $options $result
            }
        }
    }
    proc install {cmd code result op} {
        if {$code != 0} { return }
        variable worker_script
        set worker [lindex [lindex $cmd 1] 1]
        $worker eval $worker_script
    }
}
trace add execution ::dockhand::guard_worker leave ::dockhand_survey::install
