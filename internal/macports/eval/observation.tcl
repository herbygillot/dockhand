namespace eval ::dockhand {
    variable observing 0
    variable declarations 0
    variable modeled 0
    # session_modeled is set when the whole session describes a platform
    # other than its host's; see model_platform.
    variable session_modeled 0
    # observe_worker adds observation to the dispatcher guard_worker put
    # in a port's worker: the declarations traced, and the host reads kept
    # as events the tolerance rules judge.
    proc observe_worker {cmd code result op} {
        if {$code != 0} { return }
        set worker [lindex $cmd 1]
        $worker eval {
            namespace eval ::dockhand_observation {
                variable events {}
                proc host_event {cmd} {
                    variable ::dockhand_dispatcher::source_root
                    variable ::dockhand_dispatcher::base_root
                    set name [lindex $cmd 0]
                    switch -- $name {
                        exec { return "modeled context depends on a host process executed while evaluating the Portfile" }
                        glob { return "modeled context depends on directory enumeration while evaluating the Portfile" }
                        open {
                            set path [lindex $cmd 1]
                            if {[string index $path 0] eq "|"} {
                                return "modeled context depends on a host process opened while evaluating the Portfile"
                            }
                        }
                        source { set path [lindex $cmd end] }
                        file { set path [lindex $cmd 2] }
                        default { return "" }
                    }
                    # Resolve in the worker while its current directory is still
                    # the one used by the intercepted operation.
                    if {[catch {file normalize $path} normalized]} {
                        return "modeled context has an unresolved filesystem dependency"
                    }
                    set root [file normalize $source_root]
                    set base [file normalize $base_root]
                    if {$normalized ne $root && [string first "${root}/" $normalized] != 0
                        && $normalized ne $base && [string first "${base}/" $normalized] != 0} {
                        return "modeled context depends on filesystem state outside the captured ports tree"
                    }
                    # normalize does not resolve a final symlink on all Tcl
                    # versions. An opaque link must not certify captured input.
                    if {![catch {file type $normalized} kind] && $kind eq "link"} {
                        return "modeled context depends on a symbolic link while evaluating the Portfile"
                    }
                    return ""
                }
                # record keeps a declaration: checksums and revision, which
                # execution traces see.
                proc record {cmd op} {
                    variable ::dockhand_dispatcher::recording
                    if {$recording || [llength $cmd] < 2} { return }
                    lset cmd 0 [namespace tail [lindex $cmd 0]]
                    variable events
                    lappend events [list $cmd [::dockhand_dispatcher::frames 1]]
                }
                # host keeps a host read the dispatcher passed, as the traces
                # it replaced did: once the Portfile's PortSystem has run,
                # with a Portfile on the stack, and for file only the
                # subcommands that read the filesystem.
                proc host {skip cmd} {
                    variable ::dockhand_dispatcher::recording
                    if {[llength $cmd] < 2} { return }
                    set name [lindex $cmd 0]
                    if {$name eq "file" && [lindex $cmd 1] ni {exists isfile isdirectory readable writable executable mtime atime stat lstat size type readlink owned attributes}} { return }
                    # Whether the read reaches outside the tree comes first,
                    # since most don't; the frames, costly to walk, only for
                    # those that do.
                    set recording 1
                    try { set problem [host_event $cmd] } finally { set recording 0 }
                    if {$problem eq ""} { return }
                    set frames [::dockhand_dispatcher::frames $skip]
                    # A read is the Portfile's while the Portfile is being
                    # sourced, or under a procedure it defined. The frames
                    # alone can't say the first: a read made while a line's
                    # arguments are substituted has no frame for the line in
                    # compiled code, and the traces this replaces saw one only
                    # because tracing a command stops Tcl compiling it inline.
                    variable portfiles
                    set owned [expr {$portfiles > 0}]
                    foreach frame $frames { if {[string match */Portfile [lindex $frame 0]]} { set owned 1 } }
                    if {!$owned} { return }
                    variable events
                    lappend events [list [list dockhand.host-access $problem] $frames]
                }
                # source keeps an execution trace rather than the dispatcher:
                # a script run by a hidden source loses its frames, and the
                # Portfile's frame is what owns the reads beneath it. Its few
                # calls a port make the trace cheap.
                variable portfiles 0
                proc sourced {cmd op} {
                    variable ::dockhand_dispatcher::recording
                    if {$recording} { return }
                    variable ::dockhand_dispatcher::ledger
                    set cmd [lreplace $cmd 0 0 source]
                    dict incr ledger [list source "" [::dockhand_dispatcher::source_of source [lrange $cmd 1 end]] ""]
                    if {[string match */Portfile [lindex $cmd end]]} {
                        variable portfiles
                        incr portfiles
                    }
                    variable ::dockhand_dispatcher::ready
                    if {$ready} { host 1 $cmd }
                }
                proc unsourced {cmd code result op} {
                    if {[string match */Portfile [lindex $cmd end]]} {
                        variable portfiles
                        incr portfiles -1
                    }
                }
                trace add execution ::source enter ::dockhand_observation::sourced
                trace add execution ::source leave ::dockhand_observation::unsourced
                proc ready {cmd code result op} {
                    if {$code != 0} { return }
                    ::dockhand_platform::ready
                    set ::dockhand_dispatcher::ready 1
                    foreach name {checksums checksums-append checksums-prepend revision} {
                        trace add execution ::$name enter ::dockhand_observation::record
                    }
                }
            }
            trace add execution PortSystem leave ::dockhand_observation::ready
        }
        $worker eval $::dockhand::platform_script
        $worker eval [list set ::dockhand_platform::operands $::dockhand::operands]
    }
    # overrides is the list of variable and value pairs that make this
    # interpreter describe another platform, built once in Go by
    # macports.PlatformVariables, the same list a generated index gets;
    # empty observes the native platform.
    proc observation_setup {overrides trace_declarations operands_to_observe} {
        # Validate before recording anything, so a refused platform leaves
        # the session as it was.
        if {[llength $overrides] % 2 != 0} {
            error "unsupported modeled platform"
        }
        variable operands $operands_to_observe
        variable observing 1
        variable declarations $trace_declarations
        variable session_modeled
        variable modeled [expr {[llength $overrides] > 0 || $session_modeled}]
        if {[llength $overrides]} {
            ::macports::override_vars $overrides
        }
        if {$declarations} {
            trace add execution ::macports::worker_init leave ::dockhand::observe_worker
        }
    }
    proc observation_details {worker} {
        set events {}
        if {[$worker eval {info exists ::dockhand_observation::events}]} {
            set events [$worker eval {set ::dockhand_observation::events}]
        }
        set artifacts {}
        set problems {}
        set host_access 0
        set any_host_access 0
        foreach event $events {
            if {[lindex [lindex $event 0] 0] eq "dockhand.host-access"} {set any_host_access 1}
        }
        if {$::dockhand::modeled} {
            foreach event $events {
                lassign $event cmd frames
                if {[lindex $cmd 0] eq "dockhand.host-access"} { lappend problems [lindex $cmd 1] }
            }
            set problems [lsort -unique $problems]
            set host_access [expr {[llength $problems] > 0}]
        }
        if {[catch {
            load_fetch_target $worker
            $worker eval {
            apply {{} {
                set ::ports_fetch_no-mirrors yes
                set urls {}
                portfetch::checkfiles urls
                set artifacts {}
                foreach {site name} $urls {
                    set addresses {}
                    if {[info exists ::portfetch::urlmap($site)]} {
                        foreach location $::portfetch::urlmap($site) {
                            lappend addresses [portfetch::assemble_url $location $name]
                        }
                    }
                    lappend artifacts [list $name $addresses]
                }
                return $artifacts
            }}
        }} artifacts]} {
            lappend problems "native fetch plan unavailable: $artifacts"
            set artifacts {}
        }
        set operands {}
        if {[$worker eval {info exists ::dockhand_platform::events}]} {
            set operands [$worker eval {set ::dockhand_platform::events}]
        }
        set ledger {}
        if {[$worker eval {info exists ::dockhand_dispatcher::ledger}]} {
            set ledger [$worker eval {set ::dockhand_dispatcher::ledger}]
        }
        return [list $events $artifacts $problems $host_access $operands $any_host_access $ledger]
    }
}
::tclrpc::register observation_setup ::dockhand::observation_setup
