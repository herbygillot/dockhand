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
                checksums distfiles worksrcdir filespath
                patchfiles patch.pre_args livecheck.type livecheck.url livecheck.regex
                livecheck.version go.vendors cargo.crates cargo.crates_github
            } {
                if {[$worker eval [list exists $field]]} {
                    if {[catch {$worker eval [list option $field]} value]} {
                        dict set failures $field $value
                    } else {
                        dict set out $field $value
                    }
                }
            }
            dict set out option_errors $failures
            return $out
        } finally {
            mportclose $handle
        }
    }
}
::tclrpc::register initialize ::dockhand::initialize
::tclrpc::register metadata ::dockhand::metadata
