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
    }
}
