namespace eval ::dockhand {
    proc initialize {root} {
        variable source_root [file normalize $root]
        package require macports
        check_startup
        if {[catch {mportinit} detail]} { incompatible "initialization failed: $detail" }
        if {$root ne ""} {
            set url "file://[file normalize $root]"
            set ::macports::sources [list [list $url]]
            set ::macports::sources_default [list $url]
            set ::macports::porturl_prefix_map [dict create $url $url]
        }
        return [dict create platform [list $::macports::os_platform $::macports::os_major $::macports::build_arch] base_version [base_version] tcl_version [info patchlevel]]
    }

    proc metadata {portdir subport args} {
        set opts {}
        if {$subport ne ""} { lappend opts subport $subport }
        set handle [mportopen "file://$portdir" $opts $args]
        try {
            if {[catch {dict create {*}[mportinfo $handle]} out]} { incompatible "metadata dictionary could not be read" }
            set worker [ditem_key $handle workername]
            check_worker $worker
            set failures [dict create]
            foreach field {
                checksums distfiles extract.only extract.rename worksrcdir filespath master_sites fetch.type
                fetch.user_agent fetch.ignore_sslcert
                patchfiles patch.pre_args patch.dir livecheck.type livecheck.url livecheck.regex
                livecheck.version livecheck.ignore_sslcert livecheck.compression livecheck.curloptions go.vendors go.version go.package go.domain go.offline_build go.toolchain_min
                cargo.crates cargo.crates_github cargo.update cargo.dir cargo.offline_cmd
                github.author github.project github.version github.tag_prefix github.tag_suffix github.tarball_from
                gitlab.author gitlab.project gitlab.version gitlab.tag_prefix gitlab.tag_suffix gitlab.instance
                git.url git.branch
                use_xcode replaced_by
            } {
                if {[$worker eval [list exists $field]]} {
                    if {[catch {$worker eval [list option $field]} value]} {
                        dict set failures $field $value
                    } else {
                        dict set out $field $value
                    }
                }
            }
            # The effective livecheck, resolved the way port livecheck does and
            # with the tree's own checker definitions under
            # _resources/port1.0/livecheck, so a type such as pypi becomes the
            # regex it stands for and dockhand never restates what MacPorts
            # already knows. The declared type is kept beside it.
            if {![catch {$worker eval {
                apply {{} {
                    global livecheck.url livecheck.type livecheck.regex livecheck.name homepage master_sites name
                    set declared ${livecheck.type}
                    set has_master_sites [info exists master_sites]
                    set has_homepage [info exists homepage]
                    if {!$has_homepage} { set livecheck.url {} }
                    set types_dir [getdefaultportresourcepath "port1.0/livecheck"]
                    set available_types [glob -directory $types_dir -tails -types f *.tcl]
                    set available_types [regsub -all {\.tcl} [join $available_types |] {}]
                    if {${livecheck.type} eq "default"} {
                        if {$has_master_sites} {
                            foreach {master_site} ${master_sites} {
                                if {[regexp "^($available_types)(?::(\[^:\]+))?" ${master_site} _ site subdir]} {
                                    set subdirs [split $subdir /]
                                    if {[llength $subdirs] > 1} {
                                        if {[lindex $subdirs 0] eq "project"} {
                                            set subdir [lindex $subdirs 1]
                                        } else {
                                            set subdir ""
                                        }
                                    }
                                    if {${subdir} ne "" && ${livecheck.name} eq "default"} {
                                        set livecheck.name ${subdir}
                                    }
                                    set livecheck.type ${site}
                                    break
                                }
                            }
                        }
                        if {${livecheck.type} eq "default"} {
                            set livecheck.type "fallback"
                        }
                        if {$has_homepage} {
                            if {[regexp {^http://code.google.com/p/([^/]+)} $homepage _ tag]} {
                                set livecheck.type "googlecode"
                            } elseif {[regexp {^http://www.gnu.org/software/([^/]+)} $homepage _ tag]} {
                                set livecheck.type "gnu"
                            }
                        }
                    }
                    if {${livecheck.type} in [split $available_types "|"]} {
                        source "$types_dir/${livecheck.type}.tcl"
                    }
                    set livecheck.url [join ${livecheck.url}]
                    list $declared ${livecheck.type} ${livecheck.url} ${livecheck.regex} ${livecheck.name}
                }}
            }} effective]} {
                dict set out dockhand.livecheck_declared [lindex $effective 0]
                dict set out livecheck.type [lindex $effective 1]
                dict set out livecheck.url [lindex $effective 2]
                dict set out livecheck.regex [lindex $effective 3]
                dict set out livecheck.name [lindex $effective 4]
                foreach field {livecheck.type livecheck.url livecheck.regex} { dict unset failures $field }
            }
            set metadata_only 0
            if {![catch {$worker eval {
                apply {{} {
                    if {[exists replaced_by] && [option replaced_by] ne ""} { return 1 }
                    if {[llength [option distfiles]] || [option use_configure]} { return 0 }
                    set target ${::org.macports.build}
                    if {[llength [ditem_key $target pre]] || [llength [ditem_key $target post]]} { return 0 }
                    set procedure user[ditem_key $target procedure]
                    if {![llength [info procs $procedure]]} { return 0 }
                    expr {[string trim [info body $procedure]] eq {global {*}[info globals]}}
                }}
            }} value]} { set metadata_only $value }
            dict set out dockhand.metadata_only $metadata_only
            foreach field {cargo.dir patch.dir} {
                if {![dict exists $out $field]} { continue }
                set source [$worker eval {file normalize [option worksrcpath]}]
                set directory [file normalize [dict get $out $field]]
                if {$directory eq $source} {
                    dict set out $field @worksrc@
                } elseif {[string first "${source}/" $directory] == 0} {
                    dict set out $field "@worksrc@/[string range $directory [expr {[string length $source] + 1}] end]"
                }
            }
            if {[catch {fetch_details $worker} fetch]} {
                dict set failures fetch.archive_compatible "MacPorts Base [base_version]: $fetch; automatic archive preparation is unavailable; update Base or prepare this port manually"
            } else {
                dict set out fetch_details $fetch
            }
            if {[catch {$worker eval {
                set target ${org.macports.livecheck}
                expr {[ditem_key $target procedure] eq "portlivecheck::livecheck_main" &&
                      [llength [ditem_key $target pre]] == 0 && [llength [ditem_key $target post]] == 0}
            }} standard]} {
                dict set failures dockhand.livecheck_standard "cannot inspect livecheck target"
            } else {
                dict set out dockhand.livecheck_standard $standard
            }
            dict set out dockhand.base_version [base_version]
            foreach field {fetch.user fetch.password fetch_credentials macports::fetch_credentials} {
                dict unset out $field
            }
            if {[catch {fetch_credentials $worker} credentials]} {
                set credentials 1
                dict set failures fetch.has_credentials "cannot determine applicable fetch credentials"
            }
            dict set out fetch.has_credentials $credentials
            dict set out option_errors $failures
            if {$::dockhand::observing} { dict set out dockhand.observation [observation_details $worker] }
            return $out
        } finally {
            mportclose $handle
        }
    }
}
::tclrpc::register initialize ::dockhand::initialize
::tclrpc::register metadata ::dockhand::metadata
