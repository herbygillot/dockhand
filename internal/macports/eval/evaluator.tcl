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
                patchfiles patch.pre_args livecheck.type livecheck.url livecheck.regex
                livecheck.version livecheck.ignore_sslcert livecheck.compression livecheck.curloptions go.vendors go.version go.package go.domain go.offline_build go.toolchain_min
                cargo.crates cargo.crates_github cargo.update cargo.dir
                github.author github.project github.version github.tag_prefix github.tag_suffix github.tarball_from
                gitlab.author gitlab.project gitlab.version gitlab.tag_prefix gitlab.tag_suffix gitlab.instance
                git.url git.branch
                use_xcode
            } {
                if {[$worker eval [list exists $field]]} {
                    if {[catch {$worker eval [list option $field]} value]} {
                        dict set failures $field $value
                    } else {
                        dict set out $field $value
                    }
                }
            }
            set metadata_only 0
            if {![catch {$worker eval {
                apply {{} {
                    if {[llength [option distfiles]] || [option use_configure]} { return 0 }
                    set target ${::org.macports.build}
                    if {[llength [ditem_key $target pre]] || [llength [ditem_key $target post]]} { return 0 }
                    set procedure user[ditem_key $target procedure]
                    if {![llength [info procs $procedure]]} { return 0 }
                    expr {[string trim [info body $procedure]] eq {global {*}[info globals]}}
                }}
            }} value]} { set metadata_only $value }
            dict set out dockhand.metadata_only $metadata_only
            if {[dict exists $out cargo.dir]} {
                set source [$worker eval {file normalize [option worksrcpath]}]
                set directory [file normalize [dict get $out cargo.dir]]
                if {$directory eq $source} {
                    dict set out cargo.dir @worksrc@
                } elseif {[string first "${source}/" $directory] == 0} {
                    dict set out cargo.dir "@worksrc@/[string range $directory [expr {[string length $source] + 1}] end]"
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
