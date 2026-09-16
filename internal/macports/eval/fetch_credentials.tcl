namespace eval ::dockhand {
    proc fetch_credentials {worker} {
        set credentials ""
        foreach field {fetch.user fetch.password} {
            if {[$worker eval [list exists $field]] && [$worker eval [list option $field]] ne ""} {
                set credentials "dockhand:probe"
            }
        }
        if {![info exists ::macports::fetch_credentials] || [dict size $::macports::fetch_credentials] == 0} {
            return [expr {$credentials ne ""}]
        }

        set needed {}
        foreach file [$worker eval {option distfiles}] {
            set tag [$worker eval [list getdisttag $file]]
            if {[string first : $file] >= 0 && $tag eq ""} {error "unrecognized distfile tag"}
            dict set needed $tag 1
        }
        set sites {}
        foreach entry [$worker eval {option master_sites}] {
            lassign [$worker eval [list portfetch::separate_tag $entry]] site tags
            if {$tags eq ""} {set tags [list ""]} else {set tags [split $tags ,]}
            foreach tag $tags {
                if {[dict exists $needed $tag]} {
                    lappend sites $site
                    break
                }
            }
        }
        if {[llength $sites] == 0} {return 0}

        # Run the installed selector with an inert curl in an isolated interpreter.
        # The probe has no filesystem, process, or network access.
        set probe [interp create -safe]
        try {
            $probe eval {namespace eval ::macports {}; namespace eval ::uri {}}
            interp alias $probe ::uri::split {} ::uri::split
            foreach command {::macports::curlwrap ::macports::_curlwrap_credential_args} {
                if {[llength [info procs $command]] == 0} {continue}
                set arguments {}
                foreach argument [info args $command] {
                    if {[info default $command $argument default]} {
                        lappend arguments [list $argument $default]
                    } else {
                        lappend arguments $argument
                    }
                }
                $probe eval [list proc $command $arguments [info body $command]]
            }
            # Only empty/nonempty credential values affect selection. Real secrets
            # never need to enter the probe or its possible error messages.
            set configured {}
            dict for {site value} $::macports::fetch_credentials {
                dict set configured $site [expr {$value eq "" ? "" : "dockhand:configured"}]
            }
            $probe eval [list set ::macports::fetch_credentials $configured]
            $probe eval {
                proc curl {action args} {
                    if {$action ne "fetch" || [lrange $args end-1 end] ne {dockhand-url dockhand-file}} {
                        error "unrecognized credential selector invocation"
                    }
                    set flags [lrange $args 0 end-2]
                    if {[llength $flags] != 0 && ([llength $flags] != 2 || [lindex $flags 0] ne "-u")} {
                        error "unrecognized credential selector options"
                    }
                    incr ::calls
                    set ::selected [expr {[llength $flags] != 0}]
                }
            }
            foreach site $sites {
                $probe eval {set ::calls 0; set ::selected 0}
                $probe eval [list ::macports::curlwrap fetch $site $credentials dockhand-url dockhand-file]
                if {[$probe eval {set ::calls}] != 1} {error "credential selector did not invoke curl"}
                if {[$probe eval {set ::selected}]} {return 1}
            }
            return 0
        } finally {
            interp delete $probe
        }
    }
}
