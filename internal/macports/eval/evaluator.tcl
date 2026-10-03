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
        # The dispatcher goes into every worker first, before the model
        # and observation add theirs: leave traces run in the order they
        # were added.
        trace add execution ::macports::worker_init leave ::dockhand::guard_worker
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
    # with the pairs macports.ModelVariables builds; every observation
    # after it is modeled, native or not. toolchain is what its developer
    # tools' files are (macports.ToolchainAnswers), which the dispatcher
    # answers from in every worker (toolchain.tcl).
    proc model_platform {overrides toolchain} {
        if {[llength $overrides] == 0 || [llength $overrides] % 2 != 0 || [dict size $toolchain] == 0} {
            error "unsupported modeled platform"
        }
        ::macports::override_vars $overrides
        variable session_modeled 1
        set ::dockhand::toolchain $toolchain
        return [list $::macports::os_platform $::macports::os_major $::macports::build_arch]
    }

    # code_fingerprint is a checksum of each Portfile hook and variant
    # body in a port's worker, the userproc- and variant- procedures Base
    # makes of them (portutil.tcl's target_provides and variant), one of
    # every other procedure, the Portfile's and its PortGroups' own, one
    # of every option's value, and one of each patch it applies, as name
    # and checksum pairs. Each is read with the evaluation's root written
    # <source>, so two roots read alike.
    proc code_fingerprint {worker} {
        variable source_root
        variable base_root
        set code [dict create]
        set rest {}
        foreach name [lsort [$worker eval {info procs}]] {
            set text "[$worker eval [list info args $name]]\n[$worker eval [list info body $name]]"
            set text [string map [list $source_root <source> $base_root <source>] $text]
            if {[string match userproc-* $name] || [string match variant-* $name]} {
                dict set code $name [checksum $text]
            } else {
                append rest $name \n $text \n
            }
        }
        dict set code procedures [checksum $rest]
        # Every option's value, each read as the Portfile would, with the
        # port's build directory, which is named for where it was
        # evaluated, written <build>: the options dockhand reads are
        # compared by name, so this says any other moved. A value known
        # to move without an edit is its own, unstable: source_date_epoch
        # is the Portfile's modification time, which is when the
        # evaluation's files were written.
        set build [file dirname [$worker eval {option workpath}]]
        set values {}
        foreach name [lsort [$worker eval {interp aliases {}}]] {
            if {[lindex [$worker eval [list interp alias {} $name]] 0] ne "handle_option"} { continue }
            if {$name in $::dockhand::read_options || $name in {name version revision epoch}} { continue }
            if {[catch {$worker eval [list set ::$name]} value]} { set value "<unset>" }
            set value [string map [list $build <build> $source_root <source> $base_root <source>] $value]
            if {$name eq "source_date_epoch"} {
                dict set code unstable:$name [checksum $value]
                continue
            }
            append values $name \n $value \n
        }
        dict set code options [checksum $values]
        # The patches the port applies, as they are in its files.
        foreach patch [$worker eval {expr {[exists patchfiles] ? [option patchfiles] : {}}}] {
            set file [file join [$worker eval {option filespath}] $patch]
            if {[file isfile $file]} {
                set channel [open $file rb]
                try { dict set code patch:$patch [checksum [read $channel] 0] } finally { close $channel }
            }
        }
        return $code
    }

    # checksum tells two texts apart: their CRC-32 and Adler-32, which Tcl
    # has without a package.
    proc checksum {text {encode 1}} {
        set bytes [expr {$encode ? [encoding convertto utf-8 $text] : $text}]
        format %08x%08x [zlib crc32 $bytes] [zlib adler32 $bytes]
    }

    # metadata reports a port that attempted a refused effect by its
    # refusals alone, dockhand.refused, whether or not it opened.
    proc metadata {portdir subport args} {
        variable refusals {}
        set opts {}
        if {$subport ne ""} { lappend opts subport $subport }
        if {[catch {mportopen "file://$portdir" $opts $args} handle options]} {
            if {[llength $refusals]} { return [dict create dockhand.refused $refusals] }
            return -options $options $handle
        }
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
                    # Base's own globals for this (portlivecheck_run.tcl):
                    # master-sites.tcl reads livecheck.distname, and every
                    # port on a plain master site's default livecheck read
                    # as unresolved without it (batch 30).
                    global livecheck.url livecheck.type livecheck.md5 livecheck.regex \
                           livecheck.branch livecheck.name livecheck.distname livecheck.version \
                           livecheck.ignore_sslcert livecheck.compression livecheck.curloptions \
                           livecheck.user_agent homepage master_sites name subport
                    set declared ${livecheck.type}
                    set has_master_sites [info exists master_sites]
                    set has_homepage [info exists homepage]
                    if {!$has_homepage} { set livecheck.url {} }
                    set types_dir [getdefaultportresourcepath "port1.0/livecheck"]
                    # Tcl 9's glob returns nothing where 8.6 raised an
                    # error; a tree without checker definitions has no
                    # effective livecheck on either.
                    set available_types [glob -nocomplain -directory $types_dir -tails -types f *.tcl]
                    if {![llength $available_types]} { error "no livecheck types under $types_dir" }
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
            }} value]} {
                dict set out dockhand.metadata_only $value
            } else {
                dict set failures dockhand.metadata_only "cannot tell whether the port builds anything: $value"
            }
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
            # Whether the port is known to fail here, as MacPorts tests it,
            # string is true -strict, and whether its platforms exclude this
            # release, as Base's own _handle_platforms decides it, which
            # defaults known_fail to yes where they do: a port that declares
            # no known_fail can still be one, and the reason is its
            # platforms. Base's default is caught while the check runs, and
            # put back; a Base without the procedure leaves it unsaid.
            if {[catch {$worker eval {expr {[exists known_fail] && [string is true -strict [option known_fail]] ? 1 : 0}}} known]} {
                dict set failures dockhand.known_fail $known
            } else {
                dict set out dockhand.known_fail $known
            }
            if {[catch {$worker eval {
                apply {{} {
                    if {![llength [info procs ::_handle_platforms]] || ![llength [info procs ::default]]} { return "" }
                    set ::dockhand_platforms_compatible 1
                    rename ::default ::dockhand_default
                    proc ::default {option value} { if {$option eq "known_fail"} { set ::dockhand_platforms_compatible 0 } }
                    set failed [catch {::_handle_platforms platforms set [option platforms]} problem]
                    rename ::default {}
                    rename ::dockhand_default ::default
                    if {$failed} { error $problem }
                    return $::dockhand_platforms_compatible
                }}
            }} compatible]} {
                dict set failures dockhand.platforms_compatible $compatible
            } elseif {$compatible ne ""} {
                dict set out dockhand.platforms_compatible $compatible
            }
            # The Xcode the port requires on this macOS where the modelled
            # one is older, or none: minimum_xcodeversions, the xcodeversion
            # PortGroup's documented option, which it refuses by before
            # extracting. sand-runner's {24 26.0} read as a failed build on
            # macOS 15 with Xcode 16.4 (the sand-runner port); empty where
            # it's met or declared for another macOS.
            if {[catch {$worker eval {
                apply {{} {
                    if {![exists minimum_xcodeversions]} { return "" }
                    foreach {major minimum} [concat {*}[option minimum_xcodeversions]] {
                        if {$major != [option os.major]} { continue }
                        if {![info exists ::xcodeversion] || $::xcodeversion eq "none" || [vercmp $::xcodeversion $minimum] < 0} {
                            return $minimum
                        }
                    }
                    return ""
                }}
            }} minimum]} {
                dict set failures dockhand.minimum_xcode $minimum
            } elseif {$minimum ne ""} {
                dict set out dockhand.minimum_xcode $minimum
                # And the Xcode the environment is modelled with, which
                # the minimum isn't met by, for the person to read.
                dict set out dockhand.xcode [$worker eval {
                    expr {[info exists ::xcodeversion] ? $::xcodeversion : "none"}
                }]
            }
            # The PortGroups the port loads, by name, as Base records them
            # for the registry and the PortIndex.
            if {[catch {$worker eval {
                if {[info exists PortInfo(portgroups)]} { lmap group $PortInfo(portgroups) {lindex $group 0} }
            }} groups]} {
                dict set failures dockhand.portgroups $groups
            } else {
                dict set out dockhand.portgroups $groups
            }
            # The Python the python PortGroup would build the port with, were
            # python.default_version not pinned: its own default of it,
            # python_get_default_version, which Portfiles call too, as it
            # says in the port, capped at the newest of its python.versions;
            # and as it says for a port that names none, its proc run as it
            # is in an empty interpreter, since a port not named py- that
            # pins the version has python.versions set to the pin
            # (python_set_default_version). Which applies is the Go side's
            # (macports.PortInfo.PythonPinned). A port without the PortGroup
            # has neither.
            if {[$worker eval {llength [info procs python_get_default_version]}]} {
                if {[catch {$worker eval python_get_default_version} python]} {
                    dict set failures dockhand.python_default $python
                } else {
                    dict set out dockhand.python_default $python
                }
                if {[catch {
                    set probe [interp create -safe]
                    try {
                        $probe eval [list proc default {} [$worker eval {info body python_get_default_version}]]
                        $probe eval default
                    } finally {
                        interp delete $probe
                    }
                } python]} {
                    dict set failures dockhand.python_group_default $python
                } else {
                    dict set out dockhand.python_group_default $python
                }
            }
            # Whether the port declares tests, read as MacPorts reads it,
            # tbool test.run: an option Base gives no default, so unset is
            # off, not unknown.
            if {[catch {$worker eval {tbool test.run}} tests]} {
                dict set failures dockhand.test_run $tests
            } else {
                dict set out dockhand.test_run $tests
            }
            foreach field {fetch.user fetch.password fetch_credentials macports::fetch_credentials} {
                dict unset out $field
            }
            if {[catch {fetch_credentials $worker} credentials]} {
                set credentials 1
                dict set failures fetch.has_credentials "cannot determine applicable fetch credentials"
            }
            dict set out fetch.has_credentials $credentials
            # The port's code that no option shows: its phases' bodies, its
            # variants' and its own procedures, which a change record
            # compares (fidelity.Changes).
            if {[catch {code_fingerprint $worker} code]} {
                dict set failures dockhand.code "cannot read the port's procedures: $code"
            } else {
                dict set out dockhand.code $code
            }
            dict set out option_errors $failures
            if {$::dockhand::observing} { dict set out dockhand.observation [observation_details $worker] }
            if {[llength $refusals]} { return [dict create dockhand.refused $refusals] }
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
