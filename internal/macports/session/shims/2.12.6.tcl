# The port-tclsh session's init script: MacPorts semantics for one shell
# Session, serving both the evaluator's reads and the fetcher's downloads.
# Values only — spans never cross this boundary.
#
# mportinit runs once, here, for every op below. It may print to stdout;
# the Session protocol tolerates that as noise. The ui overrides further
# down are consulted only when livecheckrun re-initializes the ui layer,
# which is when its output is captured and otherwise silenced.
package require macports
mportinit

# snapshot returns the evaluated metadata of the port at an absolute
# portdir, as a flat dict. With a subport argument, that subport is the
# evaluation context. Fields absent from PortInfo are omitted. The subports
# field, when present, lists the port's subports — snapshot structure for
# the caller's enumeration, not metadata of this context.
proc snapshot {portdir {subport ""} {variations {}}} {
    set opts {}
    if {$subport ne ""} {
        set opts [list subport $subport]
    }
    set handle [mportopen "file://$portdir" $opts $variations]
    array set info [mportinfo $handle]
    set out [dict create]
    foreach field {
        name version revision epoch categories license
        maintainers platforms
        description homepage long_description
        depends_fetch depends_extract depends_patch
        depends_build depends_lib depends_run depends_test
        subports
    } {
        if {[info exists info($field)]} {
            dict set out $field $info($field)
        }
    }
    # Some of what a Portfile declares is a port option rather than a
    # PortInfo field; read those from the port's own worker interpreter
    # while the handle is open. Each costs microseconds against an
    # already-open port, so everything statically knowable is collected
    # in this one evaluation rather than in a call per group.
    set worker [ditem_key $handle workername]
    foreach opt {checksums distfiles worksrcdir filespath patchfiles
                 patch.pre_args
                 livecheck.type livecheck.url livecheck.regex livecheck.version
                 go.vendors cargo.crates cargo.crates_github} {
        if {![catch {$worker eval [list option $opt]} val] && $val ne ""} {
            dict set out $opt $val
        }
    }
    mportclose $handle
    return $out
}
::tclrpc::register snapshot snapshot

# fetchinfo returns, for the port at an absolute portdir, a dict with a
# files entry mapping each distfile to the list of full URLs it may be
# fetched from — sites expanded through MacPorts' own portfetch
# machinery (mirror macros resolved, tags applied), so nothing about URL
# assembly is reimplemented — plus the port's own fetch exceptions
# (fetch.use_epsv, fetch.ignore_sslcert, fetch.user_agent), the same
# options portfetch itself threads through to curl.
proc fetchinfo {portdir {subport ""} {variations {}} {no_mirrors 0}} {
    set opts {}
    if {$subport ne ""} {
        set opts [list subport $subport]
    }
    set handle [mportopen "file://$portdir" $opts $variations]
    set worker [ditem_key $handle workername]
    if {$no_mirrors} {
        # The switch behind `port fetch --no-mirrors`: checkfiles skips
        # the fallback mirrors, leaving only the port's own sites — a
        # new upstream release cannot be on the mirrors yet.
        $worker eval {set ports_fetch_no-mirrors yes}
    }
    set files [dict create]
    if {![catch {$worker eval {portfetch::checkfiles fetch_urls; set fetch_urls}} pairs]} {
        foreach {tag file} $pairs {
            set urls {}
            foreach site [$worker eval [list set portfetch::urlmap($tag)]] {
                lappend urls [$worker eval [list portfetch::assemble_url $site $file]]
            }
            dict set files $file $urls
        }
    }
    set out [dict create files $files]
    foreach {opt key} {
        fetch.use_epsv use_epsv
        fetch.ignore_sslcert ignore_sslcert
        fetch.user_agent user_agent
    } {
        if {![catch {$worker eval [list option $opt]} val]} {
            dict set out $key $val
        }
    }
    mportclose $handle
    return $out
}
::tclrpc::register fetchinfo fetchinfo

# options returns the values of the named port options for one
# evaluation context, omitting options the port does not have. The
# generic primitive behind reads like livecheck.* and forge
# coordinates.
proc portoptions {portdir subport variations args} {
    set opts {}
    if {$subport ne ""} {
        set opts [list subport $subport]
    }
    set handle [mportopen "file://$portdir" $opts $variations]
    set worker [ditem_key $handle workername]
    set out [dict create]
    foreach name $args {
        if {![catch {$worker eval [list option $name]} val]} {
            dict set out $name $val
        }
    }
    mportclose $handle
    return $out
}
::tclrpc::register options portoptions

# fetchdist downloads one URL to a destination file with the port's own
# fetch exceptions applied — the same flags portfetch.tcl passes to
# base's curl. Compression stays off: checksums are of the wire bytes.
proc fetchdist {url dest {disable_epsv 0} {ignore_sslcert 0} {ua ""}} {
    set args [list fetch]
    if {$disable_epsv} { lappend args --disable-epsv }
    if {$ignore_sslcert} { lappend args --ignore-ssl-cert }
    if {$ua ne ""} { lappend args --user-agent $ua }
    lappend args $url $dest
    curl {*}$args
    return ok
}
::tclrpc::register fetchdist fetchdist

# MacPorts' embedder ui API (macports.tcl documents ui_prefix and
# ui_channels): while a livecheck run is capturing, every ui message
# lands in the capture file, priority-tagged for parsing; otherwise ui
# is silenced — the protocol never depended on it.
proc ui_prefix {priority} { return "$priority\t" }
proc ui_channels {priority} {
    global dockhand_ui_fd
    if {[info exists dockhand_ui_fd]} { return [list $dockhand_ui_fd] }
    return {}
}

# livecheckrun executes the port's own livecheck phase — mportexec of
# the livecheck target, the exact machinery `port livecheck` drives —
# and returns the captured ui output. Every livecheck.type works,
# "default" resolution and the tree's type files included, because
# nothing is reimplemented.
proc livecheckrun {portdir subport dest} {
    global dockhand_ui_fd
    set opts {}
    if {$subport ne ""} {
        set opts [list subport $subport]
    }
    set handle [mportopen "file://$portdir" $opts {}]
    set dockhand_ui_fd [open $dest w]
    # ui channels are cached at init; re-init so ui_channels above is
    # consulted, capture, then re-init back to silence.
    macports::ui_init_all
    set code [catch {mportexec $handle livecheck} result]
    catch {close $dockhand_ui_fd}
    unset dockhand_ui_fd
    macports::ui_init_all
    mportclose $handle
    if {$code} {
        return -code error $result
    }
    set fd [open $dest r]
    set out [read $fd]
    close $fd
    return $out
}
::tclrpc::register livecheckrun livecheckrun

# vercmp exposes MacPorts' own version ordering.
::tclrpc::register vercmp vercmp
