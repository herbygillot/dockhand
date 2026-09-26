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
