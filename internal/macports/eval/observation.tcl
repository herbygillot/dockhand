namespace eval ::dockhand {
    variable observing 0
    variable declarations 0
    proc observe_worker {cmd code result op} {
        if {$code != 0} { return }
        set worker [lindex $cmd 1]
        $worker eval {
            namespace eval ::dockhand_observation {
                variable events {}
                proc record {cmd op} {
                    if {[llength $cmd] < 2} { return }
                    set frames {}
                    for {set i 1} {$i < [info frame]} {incr i} {
                        set frame [info frame $i]
                        set file ""
                        if {[dict exists $frame file]} { set file [dict get $frame file] }
                        lappend frames [list $file [dict get $frame line] [dict get $frame cmd]]
                    }
                    variable events
                    lappend events [list $cmd $frames]
                }
                proc ready {cmd code result op} {
                    if {$code != 0} { return }
                    foreach name {checksums checksums-append checksums-prepend revision} {
                        trace add execution ::$name enter ::dockhand_observation::record
                    }
                }
            }
            trace add execution PortSystem leave ::dockhand_observation::ready
        }
    }
    proc observation_setup {platform trace_declarations} {
        variable observing 1
        variable declarations $trace_declarations
        if {[llength $platform]} {
            lassign $platform os major arch
            if {$os ne "darwin" || ![string is integer -strict $major] || $major < 8 || $arch ni {arm64 x86_64 i386 ppc ppc64}} {
                error "unsupported modeled platform"
            }
            set macos [expr {$major < 20 ? "10.[expr {$major - 4}]" : "26.0"}]
            if {$major >= 20 && $major < 25} { set macos [expr {$major - 9}].0 }
            set osarch [expr {$arch in {arm64} ? "arm" : ($arch in {ppc ppc64} ? "powerpc" : "i386")}]
            ::macports::override_vars [list os_platform darwin os_subplatform macosx os_major $major os_version $major.0.0 os_arch $osarch build_arch $arch macos_version $macos macos_version_major [lindex [split $macos .] 0] macosx_deployment_target $macos universal_archs [expr {$major >= 20 ? "x86_64 arm64" : "i386 x86_64"}] cxx_stdlib [expr {$major >= 13 ? "libc++" : "libstdc++"}]]
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
        return [list $events $artifacts $problems]
    }
}
::tclrpc::register observation_setup ::dockhand::observation_setup
