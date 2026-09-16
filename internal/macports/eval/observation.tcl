namespace eval ::dockhand {
    variable observing 0
    variable declarations 0
    variable modeled 0
    proc observe_worker {cmd code result op} {
        if {$code != 0} { return }
        set worker [lindex $cmd 1]
        $worker eval {
            namespace eval ::dockhand_observation {
                variable events {}
                variable recording 0
                proc host_event {cmd} {
                    variable source_root
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
                    if {$normalized ne $root && [string first "${root}/" $normalized] != 0} {
                        return "modeled context depends on filesystem state outside the captured ports tree"
                    }
                    # normalize does not resolve a final symlink on all Tcl
                    # versions. An opaque link must not certify captured input.
                    if {![catch {file type $normalized} kind] && $kind eq "link"} {
                        return "modeled context depends on a symbolic link while evaluating the Portfile"
                    }
                    return ""
                }
                proc record {cmd op} {
                    variable recording
                    if {$recording || [llength $cmd] < 2} { return }
                    lset cmd 0 [namespace tail [lindex $cmd 0]]
                    set name [lindex $cmd 0]
                    if {$name eq "file" && [lindex $cmd 1] ni {exists isfile isdirectory readable writable executable mtime atime stat lstat size type readlink owned attributes}} { return }
                    set frames {}
                    for {set i 1} {$i < [info frame]} {incr i} {
                        set frame [info frame $i]
                        set file ""
                        if {[dict exists $frame file]} { set file [dict get $frame file] }
                        lappend frames [list $file [dict get $frame line] [dict get $frame cmd]]
                    }
                    if {$name in {file exec open source glob}} {
                        set owned 0
                        foreach frame $frames { if {[string match */Portfile [lindex $frame 0]]} { set owned 1 } }
                        if {!$owned} { return }
                        set recording 1
                        try { set problem [host_event $cmd] } finally { set recording 0 }
                        if {$problem eq ""} { return }
                        set cmd [list dockhand.host-access $problem]
                    }
                    variable events
                    lappend events [list $cmd $frames]
                }
                proc ready {cmd code result op} {
                    if {$code != 0} { return }
                    foreach name {checksums checksums-append checksums-prepend revision exec file open source glob} {
                        trace add execution ::$name enter ::dockhand_observation::record
                    }
                }
            }
            trace add execution PortSystem leave ::dockhand_observation::ready
        }
        $worker eval [list set ::dockhand_observation::source_root $::dockhand::source_root]
    }
    proc observation_setup {platform trace_declarations} {
        variable observing 1
        variable declarations $trace_declarations
        variable modeled [expr {[llength $platform] > 0}]
        if {[llength $platform]} {
            lassign $platform os major arch macos
            if {$os ne "darwin" || ![string is integer -strict $major] || $major < 8 || $arch ni {arm64 x86_64 i386 ppc ppc64}} {
                error "unsupported modeled platform"
            }
            set deployment $macos
            if {$major >= 20} { append deployment .0 }
            set universal [expr {$major >= 20 ? "arm64 x86_64" : ($major == 19 ? "x86_64" : ($major >= 10 ? "x86_64 i386" : "i386 ppc"))}]
            set osarch [expr {$arch in {arm64} ? "arm" : ($arch in {ppc ppc64} ? "powerpc" : "i386")}]
            ::macports::override_vars [list os_platform darwin os_subplatform macosx os_major $major os_version $major.0.0 os_arch $osarch build_arch $arch macos_version $macos macos_version_major $macos macosx_version $macos macosx_deployment_target $deployment universal_archs $universal cxx_stdlib [expr {$major >= 10 ? "libc++" : "libstdc++"}]]
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
        if {$::dockhand::modeled} {
            foreach event $events {
                lassign $event cmd frames
                if {[lindex $cmd 0] eq "dockhand.host-access"} { lappend problems [lindex $cmd 1] }
            }
            set problems [lsort -unique $problems]
            set host_access [expr {[llength $problems] > 0}]
        }
        if {[catch {$worker eval {
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
        return [list $events $artifacts $problems $host_access]
    }
}
::tclrpc::register observation_setup ::dockhand::observation_setup
