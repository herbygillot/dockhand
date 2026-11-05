namespace eval ::dockhand {
    # toolchain is what a modelled context's developer tools are
    # (docs/oracle.md, phase 5), as macports.ToolchainAnswers builds it from
    # the facts table: darwin, and whether the tools, Xcode, and libxcselect
    # are there, and the tools' SDKs. It is empty in the Mac's own context,
    # whose tools are the host's. Go sets it for a session that models
    # every context (model_platform) and for each observed one
    # (observation_setup), before any worker is made.
    variable toolchain {}

    # install_toolchain gives a worker's dispatcher the modelled tools.
    proc install_toolchain {worker} {
        variable toolchain
        $worker eval {
            namespace eval ::dockhand_dispatcher {
                variable toolchain {}
                variable clt /Library/Developer/CommandLineTools
                variable xcode_app /Applications/Xcode.app
                # shims are the programs /usr/bin holds for the developer
                # tools from OS X 10.9 on.
                variable shims {clang clang++ cc c++ gcc g++ cpp}
                # kind is what a path of the developer tools is in the
                # modelled context, a file, a directory, or absent, or
                # nothing where the table doesn't say.
                proc kind {path} {
                    variable toolchain
                    variable clt
                    variable xcode_app
                    variable shims
                    set tools [dict get $toolchain tools]
                    if {$path eq "/usr/lib/libxcselect.dylib"} {
                        return [expr {[dict get $toolchain xcselect] ? "file" : "absent"}]
                    }
                    if {$path eq $xcode_app || [string first "$xcode_app/" $path] == 0} {
                        if {![dict get $toolchain xcode]} { return absent }
                        # Xcode's SDKs are the table's where it lists them
                        # (#17: qt5's PortGroups test them, 367 of the 533
                        # ports a modelled context sent to the host); its
                        # other contents are the host's.
                        set directory [xcode_sdk_directory]
                        if {[string first "$directory/" $path] != 0} { return "" }
                        return [listed [dict get $toolchain xcode_sdks] [string range $path [string length $directory]+1 end]]
                    }
                    if {[regexp {^/usr/bin/([^/]+)$} $path -> tool] && $tool in $shims} {
                        if {[dict get $toolchain darwin] < 13} { return "" }
                        return [expr {$tools || [dict get $toolchain xcode] ? "file" : "absent"}]
                    }
                    if {$path eq "/usr/include/sys/cdefs.h"} {
                        # Base's portconfigure asks whether the system has
                        # headers in /usr/include: the tools put them there
                        # through macOS 10.13, the headers package on 10.14
                        # alone, the host's to say, and 10.15's read-only
                        # system volume has none (Apple's 10.14 and 10.15
                        # notes). Before 10.9 an Xcode put them there too, or
                        # not, by version, which the table doesn't say.
                        set darwin [dict get $toolchain darwin]
                        if {$darwin >= 19} { return absent }
                        if {$darwin >= 13 && $darwin < 18} { return [expr {$tools ? "file" : "absent"}] }
                        return ""
                    }
                    if {$path ne $clt && [string first "$clt/" $path] != 0} { return "" }
                    if {!$tools} { return absent }
                    set rest [string range $path [string length $clt]+1 end]
                    if {$path eq $clt || $rest eq "SDKs"} { return directory }
                    if {[string match usr/bin/* $rest] && [llength [split $rest /]] == 3} { return file }
                    if {[string match SDKs/* $rest]} {
                        return [listed [dict get $toolchain sdks] [string range $rest 5 end]]
                    }
                    return ""
                }
                # xcode_sdk_directory is where Xcode keeps its macOS SDKs.
                proc xcode_sdk_directory {} {
                    variable xcode_app
                    return $xcode_app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs
                }
                # listed is what a path below an SDKs directory is, by the
                # SDKs the table lists there: an SDK a directory, what's in
                # it a file, one not listed absent, and nothing where the
                # table lists none.
                proc listed {sdks rest} {
                    if {![llength $sdks]} { return "" }
                    set parts [split $rest /]
                    if {[lindex $parts 0] ni $sdks} { return absent }
                    return [expr {[llength $parts] == 1 ? "directory" : "file"}]
                }
                # show_sdk_path answers `xcrun --sdk macosx<version>
                # --show-sdk-path`, as Base's get_sdkroot and qt5's
                # PortGroups run it, from the SDKs the table lists in the
                # developer directory it runs in: DEVELOPER_DIR where the
                # command line sets it, else the row's. It finds an SDK by
                # its version exactly, as the SDK's own directory names it,
                # MacOSX26.5.sdk, never by a link's shorter name: macosx26
                # isn't located where MacOSX26.sdk links to MacOSX26.5.sdk.
                # In the tools it says the first name, in order, of the
                # SDK's directory or a link to it, and --sdk macosx says
                # MacOSX.sdk; Xcode's one SDK is MacOSX.sdk, and xcrun names
                # it by its version's link, MacOSX27.2.sdk, for --sdk macosx
                # too. Read with Xcode 27.2 and the tools' 27.0 on the
                # person's Mac, 2026-11-04, and --sdk macosx from each Tart
                # image's probe; xcrun documents none of it. Nothing where
                # the table lists none, or the command is another.
                proc show_sdk_path {arguments} {
                    variable toolchain
                    variable clt
                    if {[lindex $arguments 0] eq "-ignorestderr"} { set arguments [lrange $arguments 1 end] }
                    if {[lindex $arguments end-1] eq "2>" && [lindex $arguments end] eq "/dev/null"} { set arguments [lrange $arguments 0 end-2] }
                    set developer [dict get $toolchain developer_dir]
                    if {[lindex $arguments 0] eq "env" && [regexp {^DEVELOPER_DIR=(.+)$} [lindex $arguments 1] -> developer]} {
                        set arguments [lrange $arguments 2 end]
                    }
                    if {[llength $arguments] != 4 || [file tail [lindex $arguments 0]] ne "xcrun" || [lindex $arguments 1] ne "--sdk"
                        || [lindex $arguments 3] ne "--show-sdk-path" || ![regexp {^macosx([0-9.]*)$} [lindex $arguments 2] -> version]} {
                        return
                    }
                    if {$developer eq $clt} {
                        if {![dict get $toolchain tools]} { return }
                        set directory $clt/SDKs
                        set sdks [dict get $toolchain sdks]
                        set links [dict get $toolchain links]
                    } elseif {$developer eq [file dirname [file dirname [file dirname [file dirname [xcode_sdk_directory]]]]]} {
                        if {![dict get $toolchain xcode]} { return }
                        set directory [xcode_sdk_directory]
                        set sdks [dict get $toolchain xcode_sdks]
                        set links [dict get $toolchain xcode_links]
                    } else {
                        return
                    }
                    if {![llength $sdks]} { return }
                    # Each SDK's directory, by its version: the tools' a
                    # directory named for it, Xcode's MacOSX.sdk, whose
                    # version its links name.
                    set versions {}
                    foreach sdk [lsort -unique $sdks] {
                        if {[dict exists $links $sdk]} { continue }
                        if {[regexp {^MacOSX([0-9]+\.[0-9.]+)\.sdk$} $sdk -> named]} {
                            dict set versions $named $sdk
                        }
                    }
                    dict for {link target} $links {
                        if {$target eq "MacOSX.sdk" && [regexp {^MacOSX([0-9]+\.[0-9.]+)\.sdk$} $link -> named]} {
                            dict set versions $named $target
                        }
                    }
                    set found ""
                    if {$version eq ""} {
                        if {$directory eq "$clt/SDKs"} {
                            if {"MacOSX.sdk" in $sdks} { set found MacOSX.sdk }
                        } else {
                            dict for {named target} $versions {
                                if {$target eq "MacOSX.sdk"} { set found MacOSX${named}.sdk }
                            }
                        }
                    } elseif {[dict exists $versions $version]} {
                        set target [dict get $versions $version]
                        if {$directory eq "$clt/SDKs"} {
                            foreach sdk [lsort -unique $sdks] {
                                if {$sdk eq $target || ([dict exists $links $sdk] && [dict get $links $sdk] eq $target)} {
                                    set found $sdk
                                    break
                                }
                            }
                        } else {
                            set found MacOSX${version}.sdk
                        }
                    }
                    if {$found eq ""} {
                        return [list xcrun 1 "child process exited abnormally" {-code 1 -errorcode {CHILDSTATUS 0 1}}]
                    }
                    return [list "" 0 $directory/$found {-code 0}]
                }
                # modelled is the modelled context's answer to a call about
                # the developer tools, as fresh's are; or nothing.
                proc modelled {name arguments} {
                    variable toolchain
                    # Whether a directory of the snapshot is in a work tree
                    # has one answer in every context, git's own there.
                    if {$name eq "exec"} {
                        set answer [work_tree $arguments]
                        if {$answer eq "" && [dict size $toolchain]} { set answer [show_sdk_path $arguments] }
                        return $answer
                    }
                    if {![dict size $toolchain]} { return }
                    switch -- $name {
                        file {
                            set subcommand [lindex $arguments 0]
                            if {$subcommand ni {exists isfile isdirectory executable readable}} { return }
                            set kind [kind [lindex $arguments 1]]
                            if {$kind eq ""} { return }
                            switch -- $subcommand {
                                isfile { set answer [expr {$kind eq "file"}] }
                                isdirectory { set answer [expr {$kind eq "directory"}] }
                                default { set answer [expr {$kind ne "absent"}] }
                            }
                            return [list "" 0 $answer {-code 0}]
                        }
                        glob { return [sdks $arguments] }
                    }
                }
                # work_tree answers base's first question of when a port last
                # changed, source_date_epoch's default (portmain.tcl's
                # get_source_date_epoch): `git -c safe.directory=* -C <dir>
                # rev-parse --is-inside-work-tree` of the port's directory or
                # a PortGroup's. A tree dockhand evaluates is a snapshot with
                # no .git (workspace.Open), so where the directory is in it,
                # the answer is git's there, not a work tree, given without a
                # process, and base reads the files' modification times, as
                # in the Mac's own context; syft's update refused on the
                # process (Link Dupont's report on macports-dev). Any other
                # command line is no question of this.
                proc work_tree {arguments} {
                    variable ::dockhand_dispatcher::source_root
                    variable ::dockhand_dispatcher::base_root
                    if {[lindex $arguments 0] eq "-ignorestderr"} { set arguments [lrange $arguments 1 end] }
                    if {[lindex $arguments end-1] eq "2>" && [lindex $arguments end] eq "/dev/null"} { set arguments [lrange $arguments 0 end-2] }
                    if {[llength $arguments] != 7 || [file tail [lindex $arguments 0]] ne "git"
                        || [lrange $arguments 1 3] ne {-c safe.directory=* -C} || [lrange $arguments 5 end] ne {rev-parse --is-inside-work-tree}} {
                        return
                    }
                    if {[catch {file normalize [lindex $arguments 4]} directory]} { return }
                    foreach root [list $source_root $base_root] {
                        if {$root eq ""} { continue }
                        set root [file normalize $root]
                        if {$directory eq $root || [string first "${root}/" $directory] == 0} {
                            return [list git 1 "fatal: not a git repository (or any of the parent directories): .git" {-code 1 -errorcode NONE}]
                        }
                    }
                }
                # sdks answers a glob of the tools' SDK directory, as Base's
                # find_close_sdk makes one, from the SDKs the table lists.
                proc sdks {arguments} {
                    variable toolchain
                    variable clt
                    set directory ""
                    set tails 0
                    set complain 1
                    set i 0
                    while {$i < [llength $arguments]} {
                        switch -- [tcl::prefix match -error {} {-directory -nocomplain -tails -types --} [lindex $arguments $i]] {
                            -directory {
                                set directory [lindex $arguments $i+1]
                                incr i 2
                            }
                            -types { incr i 2 }
                            -tails {
                                set tails 1
                                incr i
                            }
                            -nocomplain {
                                set complain 0
                                incr i
                            }
                            -- {
                                incr i
                                break
                            }
                            default { break }
                        }
                    }
                    switch -- $directory \
                        "$clt/SDKs" {
                            if {![dict get $toolchain tools]} { return }
                            set sdks [dict get $toolchain sdks]
                        } \
                        [xcode_sdk_directory] {
                            if {![dict get $toolchain xcode]} { return }
                            set sdks [dict get $toolchain xcode_sdks]
                        } \
                        default { return }
                    if {![llength $sdks]} { return }
                    set patterns [lrange $arguments $i end]
                    set found {}
                    foreach sdk $sdks {
                        foreach pattern $patterns {
                            if {[string match $pattern $sdk]} {
                                lappend found [expr {$tails ? $sdk : "$directory/$sdk"}]
                                break
                            }
                        }
                    }
                    if {![llength $found] && $complain} {
                        set noun [expr {[llength $patterns] == 1 ? "pattern" : "patterns"}]
                        return [list "" 1 "no files matched glob $noun \"[join $patterns]\"" {-code 1 -errorcode {TCL OPERATION GLOB NOMATCH}}]
                    }
                    return [list "" 0 [lsort -unique $found] {-code 0}]
                }
            }
        }
        $worker eval [list set ::dockhand_dispatcher::toolchain $toolchain]
        install_lookups $worker
    }

    # lookups are Base's SDK, developer tool, and clang compiler list
    # lookups, by the name its port1.0 calls each and the procedure Base's
    # master aliases it to in its own interpreter (macports.tcl's
    # worker_init), where 2.12 ran them in the port's worker. There, their
    # file exists, glob, and xcrun see the Mac, past the dispatcher, so a
    # modelled macOS 12 context read the Mac's MacOSX.sdk, and their caches
    # hold one context's answer for the next, the compiler list's keyed by
    # its file alone.
    variable lookups {
        file_exists portlib::configure::file_exists
        find_close_sdk portlib::configure::find_close_sdk
        _get_sdkroot portlib::configure::get_sdkroot
        find_developer_tool portlib::configure::find_developer_tool
        get_system_compiler_version portlib::configure::get_system_compiler_version
        _configure_get_compiler portlib::configure::configure_get_compiler
        get_clang_compilers portlib::configure::get_clang_compilers
    }
    # lookup_helpers are what those call of Base's own beyond each other,
    # which 2.12 runs in this interpreter too, aliased into the worker, and
    # so they stay, answered from the context's caches the table fills.
    variable lookup_helpers {macports::get_tool_path macports::get_compiler_version macports::getportresourcepath}
    # lookup_state are the values those read of Base's, which in a
    # modelled context are the context's (macports::override_vars); and
    # lookup_caches are their caches of the host's answers, each starting
    # empty in each worker.
    variable lookup_state {
        macports::macos_version_major macports::macos_version macports::xcodeversion
        macports::developer_dir macports::os_major macports::os_platform macports::prefix_frozen
    }
    variable lookup_caches {portlib::configure::file_exists_cache portlib::configure::sdkroot_cache portlib::configure::clang_compilers_cache}

    # install_lookups gives a modelled context's worker its own copy of
    # each lookup Base's master aliases to its interpreter, and points the
    # alias at the copy, so the dispatcher answers what each asks from the
    # context's row of the table, as it does on 2.12, and its caches are
    # the worker's. On 2.12, where they are the worker's already, and in
    # the Mac's own context, nothing changes.
    proc install_lookups {worker} {
        variable toolchain
        variable lookups
        variable lookup_helpers
        variable lookup_state
        variable lookup_caches
        # Every modelled context, whether or not the table has its tools,
        # as Darwin 9's hasn't: a host read there is a gap the dispatcher
        # must see, as it does on 2.12.
        if {![dict size $toolchain] && !$::dockhand::modeled && !$::dockhand::session_modeled} { return }
        set moved {}
        dict for {name target} $lookups {
            if {[interp alias $worker portconfigure::$name] ne ""} { lappend moved $name $target }
        }
        if {![llength $moved]} { return }
        foreach variable $lookup_state {
            if {[info exists ::$variable]} {
                $worker eval [list namespace eval ::[namespace qualifiers $variable] [list variable [namespace tail $variable] [set ::$variable]]]
            }
        }
        foreach variable $lookup_caches {
            $worker eval [list namespace eval ::[namespace qualifiers $variable] [list variable [namespace tail $variable] [dict create]]]
        }
        foreach procedure [dict values $lookups] {
            copy_procedure $worker $procedure
        }
        foreach procedure $lookup_helpers {
            interp alias $worker ::$procedure {} ::$procedure
        }
        foreach {name target} $moved {
            interp alias $worker portconfigure::$name {}
            interp alias $worker portconfigure::$name $worker ::$target
        }
    }

    # copy_procedure defines a procedure of this interpreter's in a worker,
    # in the same namespace, with the same arguments and body.
    proc copy_procedure {worker procedure} {
        set arguments {}
        foreach argument [info args ::$procedure] {
            if {[info default ::$procedure $argument default]} {
                lappend arguments [list $argument $default]
            } else {
                lappend arguments $argument
            }
        }
        $worker eval [list namespace eval ::[namespace qualifiers $procedure] [list proc [namespace tail $procedure] $arguments [info body ::$procedure]]]
    }
}
