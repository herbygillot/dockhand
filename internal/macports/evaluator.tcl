namespace eval ::dockhand {
    proc initialize {root} {
        package require macports
        mportinit
        if {$root ne ""} {
            set url "file://[file normalize $root]"
            set ::macports::sources [list [list $url]]
            set ::macports::sources_default [list $url]
            set ::macports::porturl_prefix_map [dict create $url $url]
        }
        return [list $::macports::os_platform $::macports::os_major $::macports::build_arch]
    }

    proc metadata {portdir subport args} {
        set opts {}
        if {$subport ne ""} { lappend opts subport $subport }
        set handle [mportopen "file://$portdir" $opts $args]
        try {
            set out [dict create {*}[mportinfo $handle]]
            set worker [ditem_key $handle workername]
            set failures [dict create]
            foreach field {
                checksums distfiles worksrcdir filespath master_sites fetch.type
                fetch.user_agent fetch.ignore_sslcert
                patchfiles patch.pre_args livecheck.type livecheck.url livecheck.regex
                livecheck.version go.vendors go.version go.package go.domain go.offline_build go.toolchain_min
                cargo.crates cargo.crates_github
                github.author github.project github.version github.tag_prefix github.tag_suffix github.tarball_from
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
            if {[catch {$worker eval {
                set target ${org.macports.fetch}
                set pre {}
                foreach hook [ditem_key $target pre] {
                    lappend pre [info body user${hook}]
                }
                list [ditem_key $target procedure] $pre [ditem_key $target post]
            }} fetch]} {
                dict set failures fetch.archive_compatible "cannot inspect fetch target"
            } else {
                dict set out fetch_details $fetch
            }
            set credentials 0
            foreach field {fetch.user fetch.password} {
                if {[$worker eval [list exists $field]]} {
                    if {[catch {$worker eval [list option $field]} value]} {
                        dict set failures fetch.has_credentials "cannot evaluate fetch credentials"
                    } elseif {$value ne ""} {
                        set credentials 1
                    }
                }
                dict unset out $field
            }
            dict set out fetch.has_credentials $credentials
            dict set out option_errors $failures
            return $out
        } finally {
            mportclose $handle
        }
    }
}
::tclrpc::register initialize ::dockhand::initialize
::tclrpc::register metadata ::dockhand::metadata
