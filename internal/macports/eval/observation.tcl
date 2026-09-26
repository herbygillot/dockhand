namespace eval ::dockhand {
    variable observing 0
    variable declarations 0
    variable modeled 0
    # session_modeled is set when the whole session describes a platform
    # other than its host's; see model_platform.
    variable session_modeled 0
    proc observe_worker {cmd code result op} {
        if {$code != 0} { return }
        set worker [lindex $cmd 1]
        $worker eval {
            namespace eval ::dockhand_observation {
                variable events {}
                variable recording 0
                proc host_event {cmd} {
                    variable source_root
                    variable base_root
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
                # frames is the caller's stack, below the procedures of
                # this namespace that are running: skip of them.
                proc frames {skip} {
                    set frames {}
                    for {set i 1} {$i <= [info frame] - $skip} {incr i} {
                        set frame [info frame $i]
                        set file ""
                        if {[dict exists $frame file]} { set file [dict get $frame file] }
                        lappend frames [list $file [dict get $frame line] [dict get $frame cmd]]
                    }
                    return $frames
                }
                # record keeps a declaration: checksums and revision, which
                # execution traces see.
                proc record {cmd op} {
                    variable recording
                    if {$recording || [llength $cmd] < 2} { return }
                    lset cmd 0 [namespace tail [lindex $cmd 0]]
                    variable events
                    lappend events [list $cmd [frames 1]]
                }
                # host keeps a host read the dispatcher passed, as the traces
                # it replaced did: once the Portfile's PortSystem has run,
                # with a Portfile on the stack, and for file only the
                # subcommands that read the filesystem.
                proc host {skip cmd} {
                    variable recording
                    if {[llength $cmd] < 2} { return }
                    set name [lindex $cmd 0]
                    if {$name eq "file" && [lindex $cmd 1] ni {exists isfile isdirectory readable writable executable mtime atime stat lstat size type readlink owned attributes}} { return }
                    # Whether the read reaches outside the tree comes first,
                    # since most don't; the frames, costly to walk, only for
                    # those that do.
                    set recording 1
                    try { set problem [host_event $cmd] } finally { set recording 0 }
                    if {$problem eq ""} { return }
                    set frames [frames $skip]
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
                # The dispatcher (docs/oracle.md, phase 1). exec, file, open,
                # and glob are hidden in the worker and reach the host only
                # through dispatch, which passes every call to the host
                # unchanged and counts it in the ledger by where its answer
                # comes from: pure path arithmetic, the captured tree, Base's
                # own library, a process, a relative path, a directory
                # enumeration, or the host. It walks frames only for the host reads it keeps as
                # events, since a frame walk is what made the traces costly.
                variable ready 0
                variable ledger [dict create]
                variable source_root ""
                variable base_root ""
                variable base_library ""
                variable pure {join normalize dirname tail split extension rootname separator nativename pathtype}
                proc source_of {name arguments} {
                    variable source_root
                    variable base_root
                    variable pure
                    switch -- $name {
                        exec { return process }
                        glob { return enumeration }
                        open {
                            set path [lindex $arguments 0]
                            if {[string index $path 0] eq "|"} { return process }
                        }
                        source { set path [lindex $arguments end] }
                        file {
                            if {[lindex $arguments 0] in $pure} { return pure }
                            set path [lindex $arguments 1]
                        }
                        default { return host }
                    }
                    if {[string index $path 0] ne "/"} { return relative }
                    foreach root [list $source_root $base_root] {
                        if {$root ne "" && ($path eq $root || [string first "$root/" $path] == 0)} { return tree }
                    }
                    variable base_library
                    if {$base_library ne "" && [string first "$base_library/" $path] == 0} { return base }
                    return host
                }
                proc dispatch {name args} {
                    variable recording
                    if {!$recording} {
                        variable ledger
                        dict incr ledger [list $name [expr {$name eq "file" ? [lindex $args 0] : ""}] [source_of $name $args]]
                        variable ready
                        if {$ready} { host 2 [list $name {*}$args] }
                    }
                    return [interp invokehidden {} $name {*}$args]
                }
                foreach name {exec file open glob} {
                    interp hide {} $name
                    interp alias {} $name {} ::dockhand_observation::dispatch $name
                }
                # source keeps an execution trace instead: a script run by a
                # hidden source loses its frames, and the Portfile's frame is
                # what owns the reads beneath it. Its few calls a port make
                # the trace cheap.
                variable portfiles 0
                proc sourced {cmd op} {
                    variable recording
                    if {$recording} { return }
                    variable ledger
                    set cmd [lreplace $cmd 0 0 source]
                    dict incr ledger [list source "" [source_of source [lrange $cmd 1 end]]]
                    if {[string match */Portfile [lindex $cmd end]]} {
                        variable portfiles
                        incr portfiles
                    }
                    variable ready
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
                    variable ready 1
                    foreach name {checksums checksums-append checksums-prepend revision} {
                        trace add execution ::$name enter ::dockhand_observation::record
                    }
                }
            }
            trace add execution PortSystem leave ::dockhand_observation::ready
        }
        $worker eval $::dockhand::platform_script
        $worker eval [list set ::dockhand_platform::operands $::dockhand::operands]
        $worker eval [list set ::dockhand_observation::source_root $::dockhand::source_root]
        $worker eval [list set ::dockhand_observation::base_root $::dockhand::base_root]
        $worker eval [list set ::dockhand_observation::base_library [base_library]]
    }
    # base_library is where MacPorts Base's own Tcl lives, the port1.0 and
    # macports1.0 its workers load: Base's code, not host state.
    variable library ""
    proc base_library {} {
        variable library
        if {$library eq "" && [regexp {(\S+)/macports1\.0/} [package ifneeded macports [package present macports]] -> found]} {
            set library $found
        }
        return $library
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
        if {[$worker eval {info exists ::dockhand_observation::ledger}]} {
            set ledger [$worker eval {set ::dockhand_observation::ledger}]
        }
        return [list $events $artifacts $problems $host_access $operands $any_host_access $ledger]
    }
}
::tclrpc::register observation_setup ::dockhand::observation_setup
