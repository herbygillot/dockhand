namespace eval ::dockhand {
    proc base_version {} {
        if {[llength [info commands ::macports::version]] && ![catch {::macports::version} version]} { return $version }
        return unknown
    }

    proc incompatible {detail} {
        return -code error "MacPorts Base [base_version]: $detail; check the selected --prefix/port-tclsh installation or update MacPorts Base"
    }

    proc check_startup {} {
        foreach command {::mportinit ::mportopen ::mportinfo ::mportclose ::ditem_key ::vercmp} {
            if {![llength [info commands $command]]} { incompatible "required evaluator command $command is missing" }
        }
        if {[catch {expr {[vercmp 1.9 1.10] < 0 && [vercmp 1.10 1.9] > 0 && [vercmp 1.0 1.0] == 0}} ordered] || !$ordered} {
            incompatible "version comparison capability check failed"
        }
    }

    proc check_worker {worker} {
        if {$worker eq "" || ![llength [info commands $worker]]} { incompatible "metadata worker is unavailable" }
        if {[catch {$worker eval {
            foreach command {exists option} {
                if {![llength [info commands $command]]} { error "missing worker command $command" }
            }
            foreach field {name version revision epoch} {
                if {![exists $field]} { error "missing metadata option $field" }
                option $field
            }
        }} detail]} { incompatible "metadata capability check failed: $detail" }
    }

    proc check_fetch_registration {worker} {
        $worker eval {apply {{} {
            foreach command {target_new target_provides ditem_key ditem_delete} {
                if {![llength [info commands $command]]} { error "missing fetch registration command $command" }
            }
            set commands [info procs]
            set targets $::portutil::targets
            set item [target_new org.dockhand.compatibility portfetch::fetch_main]
            try {
                set name dockhand_probe_[clock clicks]
                target_provides $item $name
                pre-$name {return}
                post-$name {return}
                if {[ditem_key $item procedure] ne "portfetch::fetch_main"} { error "unrecognized target procedure registration" }
                foreach field {pre post} {
                    set hooks [ditem_key $item $field]
                    if {[llength $hooks] != 1} { error "unrecognized $field hook registration" }
                    set command user[lindex $hooks 0]
                    if {![llength [info procs $command]]} { error "unrecognized $field hook wrapper" }
                    if {[info body $command] ne "global {*}\[info globals\]\nreturn"} { error "unrecognized $field hook scope wrapper" }
                }
            } finally {
                set ::portutil::targets $targets
                ditem_delete $item
                foreach command [info procs] {
                    if {$command ni $commands} { rename $command {} }
                }
            }
        }}}
    }

    proc fetch_details {worker} {
        check_fetch_registration $worker
        return [$worker eval {
            if {![info exists org.macports.fetch]} { error "fetch target record is unavailable" }
            set target ${org.macports.fetch}
            set record [ditem_key $target]
            foreach field {name procedure} {
                if {![dict exists $record $field]} { error "fetch target lacks $field" }
            }
            if {[dict get $record name] ne "org.macports.fetch"} { error "unexpected fetch target identity" }
            set procedure [dict get $record procedure]
            if {![llength [info commands $procedure]]} { error "fetch procedure is unavailable" }
            set pre {}
            set origins {}
            # The files a hook can have been written in: the PortGroups this
            # port loaded, then its Portfile. A body found in one of them
            # gives the hook an origin and the line its text starts on.
            set sources {}
            if {[info exists PortInfo(portgroups)]} {
                foreach group $PortInfo(portgroups) {
                    if {[llength $group] >= 3} { lappend sources [list "the [lindex $group 0]-[lindex $group 1] PortGroup" [lindex $group 2]] }
                }
            }
            if {[info exists portpath]} { lappend sources [list Portfile [file join $portpath Portfile]] }
            foreach hook [ditem_key $target pre] {
                if {![llength [info procs user${hook}]]} { error "unrecognized pre-fetch wrapper" }
                set body [info body user${hook}]
                if {[string first "global {*}\[info globals\]\n" $body] != 0} { error "unrecognized pre-fetch scope wrapper" }
                lappend pre $body
                set needle [string trim [string range $body [string length "global {*}\[info globals\]\n"] end]]
                set origin {}
                foreach source $sources {
                    if {$needle eq ""} break
                    lassign $source label path
                    if {[catch {set fd [open $path r]; set content [read $fd]; close $fd}]} continue
                    set at [string first $needle $content]
                    if {$at < 0} continue
                    set origin [list $label [expr {[regexp -all {\n} [string range $content 0 $at-1]] + 1}]]
                    break
                }
                lappend origins $origin
            }
            set post [ditem_key $target post]
            llength $post
            list $procedure $pre $post $origins
        }]
    }
}
