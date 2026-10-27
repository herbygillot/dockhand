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
                        # Xcode's contents are the host's where the model has Xcode.
                        return [expr {[dict get $toolchain xcode] ? "" : "absent"}]
                    }
                    if {[regexp {^/usr/bin/([^/]+)$} $path -> tool] && $tool in $shims} {
                        if {[dict get $toolchain darwin] < 13} { return "" }
                        return [expr {$tools || [dict get $toolchain xcode] ? "file" : "absent"}]
                    }
                    if {$path ne $clt && [string first "$clt/" $path] != 0} { return "" }
                    if {!$tools} { return absent }
                    set rest [string range $path [string length $clt]+1 end]
                    if {$path eq $clt || $rest eq "SDKs"} { return directory }
                    if {[string match usr/bin/* $rest] && [llength [split $rest /]] == 3} { return file }
                    if {[string match SDKs/* $rest]} {
                        set sdks [dict get $toolchain sdks]
                        if {![llength $sdks]} { return "" }
                        set parts [split $rest /]
                        if {[lindex $parts 1] ni $sdks} { return absent }
                        return [expr {[llength $parts] == 2 ? "directory" : "file"}]
                    }
                    return ""
                }
                # modelled is the modelled context's answer to a call about
                # the developer tools, as fresh's are; or nothing.
                proc modelled {name arguments} {
                    variable toolchain
                    # Whether a directory of the snapshot is in a work tree
                    # has one answer in every context, git's own there.
                    if {$name eq "exec"} { return [work_tree $arguments] }
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
                    set sdks [dict get $toolchain sdks]
                    if {$directory ne "$clt/SDKs" || ![llength $sdks] || ![dict get $toolchain tools]} { return }
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

    # lookups are Base's SDK and developer tool lookups, by the name its
    # port1.0 calls each and the procedure Base's master aliases it to in
    # its own interpreter (macports.tcl's worker_init), where 2.12 ran them
    # in the port's worker. There, their file exists, glob, and xcrun see
    # the Mac, past the dispatcher, so a modelled macOS 12 context read the
    # Mac's MacOSX.sdk, and their caches hold one context's answer for the
    # next.
    variable lookups {
        file_exists portlib::configure::file_exists
        find_close_sdk portlib::configure::find_close_sdk
        _get_sdkroot portlib::configure::get_sdkroot
        find_developer_tool portlib::configure::find_developer_tool
        get_system_compiler_version portlib::configure::get_system_compiler_version
        _configure_get_compiler portlib::configure::configure_get_compiler
    }
    # lookup_helpers are what those call of Base's own beyond each other,
    # which 2.12 runs in this interpreter too, aliased into the worker, and
    # so they stay, answered from the context's caches the table fills.
    variable lookup_helpers {macports::get_tool_path macports::get_compiler_version}
    # lookup_state are the values those read of Base's, which in a
    # modelled context are the context's (macports::override_vars); and
    # lookup_caches are their caches of the host's answers, each starting
    # empty in each worker.
    variable lookup_state {
        macports::macos_version_major macports::macos_version macports::xcodeversion
        macports::developer_dir macports::os_major macports::prefix_frozen
    }
    variable lookup_caches {portlib::configure::file_exists_cache portlib::configure::sdkroot_cache}

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
        if {![dict size $toolchain]} { return }
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
