namespace eval ::dockhand {
    # probe loads the macports package without initializing it and says
    # which Base it is, so the version can be judged before mportinit
    # reads the host's configuration.
    proc probe {} {
        package require macports
        return [base_version]
    }

    # base is the directory an overlay's shared files resolve to, the
    # base workspace whose _resources the overlay links; reads there are
    # reads of the captured tree. It is the root itself for a base.
    proc initialize {root {base ""}} {
        variable source_root [file normalize $root]
        variable base_root [file normalize [expr {$base eq "" ? $root : $base}]]
        package require macports
        check_startup
        if {[catch {mportinit} detail]} { incompatible "initialization failed: $detail" }
        check_initialized
        if {$root ne ""} {
            set url "file://[file normalize $root]"
            set ::macports::sources [list [list $url]]
            set ::macports::sources_default [list $url]
            set ::macports::porturl_prefix_map [dict create $url $url]
        }
        return [dict create platform [list $::macports::os_platform $::macports::os_major $::macports::build_arch] base_version [base_version] tcl_version [info patchlevel]]
    }

    # model_platform makes this interpreter describe another platform for
    # the rest of the session, a host that is not a Mac describing a macOS,
    # with the pairs macports.PlatformVariables builds; every observation
    # after it is modeled, native or not.
    proc model_platform {overrides tools} {
        if {[llength $overrides] == 0 || [llength $overrides] % 2 != 0 || $tools eq ""} {
            error "unsupported modeled platform"
        }
        ::macports::override_vars $overrides
        variable session_modeled 1
        variable model_tools $tools
        trace add execution ::macports::worker_init leave ::dockhand::model_worker
        return [list $::macports::os_platform $::macports::os_major $::macports::build_arch]
    }

    # model_worker gives a port's worker the modeled Mac's Command Line
    # Tools. Base asks the host's filesystem whether they are installed,
    # through the tools' make for use_xcode's default and through their
    # clang when /usr/bin/clang is absent; in a modeled session the model
    # answers that the tools' programs exist, and every other question
    # about the filesystem goes to the host as before.
    proc model_worker {cmd code result op} {
        if {$code != 0} { return }
        variable model_tools
        [lindex $cmd 1] eval [list apply {{tools} {
            rename ::file ::dockhand_host_file
            proc ::file {args} [string map [list @TOOLS@ $tools] {
                if {[llength $args] == 2 && [lindex $args 0] in {exists executable isfile}
                    && [string match {@TOOLS@/usr/bin/*} [lindex $args 1]]} {
                    return 1
                }
                tailcall ::dockhand_host_file {*}$args
            }]
        }} $model_tools]
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
            # The options read are the one list Go holds, macports.ReadOptions,
            # set before this script is sourced.
            foreach field $::dockhand::read_options {
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
::tclrpc::register probe ::dockhand::probe
::tclrpc::register initialize ::dockhand::initialize
::tclrpc::register model_platform ::dockhand::model_platform
::tclrpc::register metadata ::dockhand::metadata
