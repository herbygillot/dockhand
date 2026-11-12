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
                # host_event is why a read reaches the host, or empty: the
                # dispatcher's one reading, which the closed model shares
                # (::dockhand_dispatcher::host_path).
                proc host_event {cmd} {
                    return [::dockhand_dispatcher::host_path [lindex $cmd 0] [lrange $cmd 1 end]]
                }
                # closed keeps a read the closed model answered as an event
                # the verdict weighs, whoever asked: the Portfile, a
                # PortGroup, or Base, which a read no Portfile owns left
                # unseen before (#110), with the call it closed. skip is the
                # dispatcher's frames.
                proc closed {skip problem call} {
                    set frames [::dockhand_dispatcher::frames $skip]
                    set owner base
                    foreach frame $frames {
                        set file [lindex $frame 0]
                        if {[string match */Portfile $file]} { set owner portfile; break }
                        if {[string match */_resources/port1.0/group/* $file]} { set owner portgroup }
                    }
                    variable events
                    lappend events [list [list dockhand.host-access $problem closed $owner $call] $frames]
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
                    if {$name eq "file" && [::dockhand_dispatcher::subcommand [lindex $cmd 1]] ni {exists isfile isdirectory readable writable executable mtime atime stat lstat size type readlink owned attributes}} { return }
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
                    variable ::dockhand_dispatcher::record_paths
                    if {$record_paths} { ::dockhand_dispatcher::note source [lrange $cmd 1 end] host }
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
        # An observed context's clock is UTC's, set up before the Portfile
        # runs: a Portfile that formats source_date_epoch, as uv's and
        # cloudflared's build dates do, read the Mac's own zone,
        # /etc/localtime, and clock's first use probes the Mac's zoneinfo
        # directories, host state an observation judges, and which a
        # modelled context hasn't.
        # The probe is dockhand's own setup, uncounted and unclosed: with
        # TCL_TZ set, where the zoneinfo is changes no answer.
        $worker eval {
            set ::env(TCL_TZ) :UTC
            set ::dockhand_dispatcher::recording 1
            try { catch {clock format 0} } finally { set ::dockhand_dispatcher::recording 0 }
        }
    }
    # overrides is the list of variable and value pairs that make this
    # interpreter describe another platform, built once in Go by
    # macports.PlatformVariables, the same list a generated index gets;
    # empty observes the native platform.
    proc observation_setup {overrides trace_declarations operands_to_observe toolchain} {
        # Validate before recording anything, so a refused platform leaves
        # the session as it was.
        if {[llength $overrides] % 2 != 0 || [llength $toolchain] % 2 != 0} {
            error "unsupported modeled platform"
        }
        # A modelled context's developer tools are the facts table's; the
        # Mac's own context's are its own.
        set ::dockhand::toolchain $toolchain
        variable operands $operands_to_observe
        variable observing 1
        variable declarations $trace_declarations
        variable session_modeled
        variable modeled [expr {[llength $overrides] > 0 || $session_modeled}]
        # What a modelled setup overrides is kept as the Mac had it, and
        # the native setup after it puts it back, a deferred option's
        # traces too, so the native context describes this Mac again
        # (Codex's review of #110 and #125, finding 7).
        variable native
        if {![info exists native]} { set native {} }
        if {[llength $overrides]} {
            foreach {name value} $overrides {
                if {[dict exists $native $name] || [namespace which -variable ::macports::$name] eq ""} { continue }
                set traces [trace info variable ::macports::$name]
                set exists [info exists ::macports::$name]
                set kept ""
                if {$exists && $traces eq ""} { set kept [set ::macports::$name] }
                dict set native $name [list $exists $kept $traces]
            }
            ::macports::override_vars $overrides
        } else {
            dict for {name state} $native {
                lassign $state exists kept traces
                if {$exists && $traces eq ""} {
                    set ::macports::$name $kept
                    continue
                }
                unset -nocomplain ::macports::$name
                namespace eval ::macports [list variable $name]
                foreach trace $traces { trace add variable ::macports::$name {*}$trace }
            }
            set native {}
        }

        if {$declarations} {
            trace add execution ::macports::worker_init leave ::dockhand::observe_worker
        }
    }
    proc observation_details {worker} {
        set artifacts {}
        # The fetch plan first, then what the verdict weighs: a closed
        # read first made inside checkfiles is an event like any other.
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
            set fetch_problem "native fetch plan unavailable: $artifacts"
            set artifacts {}
        }
        set events {}
        if {[$worker eval {info exists ::dockhand_observation::events}]} {
            set events [$worker eval {set ::dockhand_observation::events}]
        }
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
        # What the master's closed exec answered for the worker before the
        # plan has no frames to explain it by, and is weighed: on a release
        # the table has no row for, Base's own questions about its
        # developer tools reach the master, and their answer decided a
        # fetch field (Darwin 9's distfiles, #110).
        if {$::dockhand::modeled && [dict size $::dockhand::master_closed]} {
            set programs {}
            dict for {line count} $::dockhand::master_closed {
                set words $line
                while {[lindex $words 0] in {-ignorestderr -keepnewline --}} { set words [lrange $words 1 end] }
                set program [file tail [lindex $words 0]]
                if {$program ni $programs} { lappend programs $program }
            }
            lappend problems "modeled context has no answer for what MacPorts asked of this release's developer tools ([join $programs {, }]): the facts table doesn't have them"
            set problems [lsort -unique $problems]
            set host_access 1
        }
        if {[info exists fetch_problem]} { lappend problems $fetch_problem }
        set operands {}
        if {[$worker eval {info exists ::dockhand_platform::events}]} {
            set operands [$worker eval {set ::dockhand_platform::events}]
        }
        set ledger {}
        set reads {}
        set execs {}
        set hooks {}
        if {[$worker eval {info exists ::dockhand_dispatcher::ledger}]} {
            set ledger [$worker eval {set ::dockhand_dispatcher::ledger}]
            # What the master's closed exec answered on the worker's behalf.
            dict for {line count} $::dockhand::master_closed {
                dict incr ledger [list exec "" closed master] $count
            }
            # What a survey records (recording_paths); empty otherwise.
            lassign [$worker eval {list $::dockhand_dispatcher::reads $::dockhand_dispatcher::execs $::dockhand_dispatcher::hooks}] reads execs hooks
        }
        # Each source frame goes once, in a table the events name by
        # index: every checksums-append go.vendors makes had the whole
        # go.vendors command on its stack, so opkssh's ~500-line block
        # sent 18.5 MB, past the RPC frame limit (field testing,
        # 2026-10-23), growing with the square of the block.
        set table {}
        set ids [dict create]
        set intern {{events} {
            upvar 1 table table ids ids
            set sent {}
            foreach event $events {
                lassign $event cmd frames
                set named {}
                foreach frame $frames {
                    if {![dict exists $ids $frame]} {
                        dict set ids $frame [llength $table]
                        lappend table $frame
                    }
                    lappend named [dict get $ids $frame]
                }
                lappend sent [list $cmd $named]
            }
            return $sent
        }}
        set events [apply $intern $events]
        set operands [apply $intern $operands]
        # Where Base's own Tcl is, by the path its frames name it: the
        # verdict takes a frame as Base's code only under it (#110).
        set library [base_library]
        if {$library ne ""} { set library [file normalize $library] }
        return [list $events $artifacts $problems $host_access $operands $any_host_access $ledger $table $reads $execs $hooks $library]
    }
}
::tclrpc::register observation_setup ::dockhand::observation_setup
