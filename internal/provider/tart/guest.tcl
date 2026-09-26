# The guest side of dockhand's Tart provider (Design v3 §7). launchd runs it
# as root in a fresh clone of a prepared image, under the image's
# port-tclsh. It builds a check's targets in order, as MacPorts CI does
# (decisions 11 and 22): for each, everything is deactivated and only the
# target's dependencies are installed and activated, then it is linted,
# fetched, checksummed, and installed, and its declared tests run after.
# Each target's result is written to results.json as it finishes, so a
# guest lost midway keeps what it recorded (decision 44).
package require json
package require json::write
json::write indented false

set root /var/tmp/dockhand-check
if {[info exists ::env(DOCKHAND_GUEST_ROOT)]} { set root $::env(DOCKHAND_GUEST_ROOT) }
set fd [open $root/input.json r]
set input [json::json2dict [read $fd]]
close $fd
set prefix [dict get $input prefix]
set port $prefix/bin/port
set results {}
set environment [dict create]

# save writes the results so far, replacing the file whole.
proc save {state {detail ""}} {
    global root input results environment
    set targets {}
    foreach result $results {
        set fields {}
        dict for {key value} $result { lappend fields $key [json::write string $value] }
        lappend targets [json::write object {*}$fields]
    }
    set facts {}
    dict for {key value} $environment { lappend facts $key [json::write string $value] }
    set data [json::write object protocol [dict get $input protocol] state [json::write string $state] \
        detail [json::write string $detail] environment [json::write object {*}$facts] targets [json::write array {*}$targets]]
    set fd [open $root/results.json.tmp w]
    puts $fd $data
    close $fd
    file rename -force $root/results.json.tmp $root/results.json
}

# The external commands go through these three, which a test replaces.
# run runs a command with its output appended to a log, returning "" or
# why it failed.
if {[llength [info procs run]] == 0} {
    proc run {log argv} {
        if {[catch {exec {*}$argv >>& $log} message]} { return $message }
        return ""
    }
}
# timed runs a command with a deadline, returning "", "timed out", or why
# it failed. A run past its deadline is terminated, then killed.
if {[llength [info procs timed]] == 0} {
    proc timed {log argv seconds} {
        set out [open $log a]
        set chan [open |[concat $argv [list 2>@1]] r]
        fconfigure $chan -blocking 0
        set ::timedDone ""
        fileevent $chan readable [list apply {{chan out} {
            puts -nonewline $out [read $chan]
            if {[eof $chan]} { set ::timedDone eof }
        }} $chan $out]
        set timer [after [expr {$seconds * 1000}] {set ::timedDone timeout}]
        vwait ::timedDone
        after cancel $timer
        fileevent $chan readable {}
        close $out
        if {$::timedDone eq "timeout"} {
            foreach pid [pid $chan] { catch {exec kill -TERM $pid} }
            after 10000
            foreach pid [pid $chan] { catch {exec kill -KILL $pid} }
            catch {close $chan}
            return "timed out"
        }
        fconfigure $chan -blocking 1
        if {[catch {close $chan} message]} { return $message }
        return ""
    }
}
# declares_tests asks MacPorts whether the port declares a test phase.
if {[llength [info procs declares_tests]] == 0} {
    proc declares_tests {portdir name variants} {
        set handle [mportopen "file://$portdir" [list subport $name] $variants]
        set worker [ditem_key $handle workername]
        set declared [$worker eval {tbool test.run}]
        mportclose $handle
        return $declared
    }
}
# fact is a command's output, trimmed, or nothing.
proc fact {args} {
    if {[catch {exec {*}$args 2>/dev/null} value]} { return "" }
    return [string trim $value]
}

# why is what MacPorts said about a failure, from the log's last Error
# lines, or the command's own message.
proc why {log message} {
    set fd [open $log r]
    set text [read $fd]
    close $fd
    set errors {}
    foreach line [split $text \n] {
        if {[regexp {^Error: (.+)$} $line -> error] && ![regexp {^(See |Follow https://guide|Processing of port )} $error]} {
            lappend errors [string trim $error]
        }
    }
    if {[llength $errors]} { return [join [lrange $errors end-2 end] "; "] }
    return $message
}

# build builds one target, returning its result.
proc build {index target} {
    global root port input results
    set id [dict get $target id]
    set name [dict get $target name]
    set log $root/target-$index.log
    set result [dict create id $id outcome "" phase "" tests none log target-$index.log detail ""]
    close [open $log a]
    if {[dict exists $target blocked] && [dict get $target blocked]} {
        dict set result outcome blocked
        return $result
    }
    # A changed dependency that did not pass blocks its dependents: an old
    # build of it never stands in for the one the branch changes.
    if {[dict exists $target depends_on]} {
        foreach dependency [dict get $target depends_on] {
            foreach earlier $results {
                if {[dict get $earlier id] eq $dependency && [dict get $earlier outcome] ne "passed"} {
                    dict set result outcome blocked
                    dict set result detail "$dependency did not pass"
                    return $result
                }
            }
        }
    }
    set portdir [file dirname [file join $root ports [dict get $target portfile]]]
    set variants {}
    set selection [list subport=$name]
    if {[dict exists $target variants]} {
        dict for {variant enabled} [dict get $target variants] {
            set sign [expr {$enabled ? "+" : "-"}]
            lappend variants $variant $sign
            lappend selection $sign$variant
        }
    }
    set here [list $port -N -D $portdir]
    set fail {apply {{result phase detail} { dict set result outcome failed; dict set result phase $phase; dict set result detail $detail; return $result }}}

    # Only this target's dependencies are active when it builds.
    if {[fact $port -q installed active] ne ""} {
        if {[set message [run $log [list $port -N -f deactivate active]]] ne ""} {
            return [{*}$fail $result install "deactivating the ports before it failed: [why $log $message]"]
        }
    }
    if {[set message [run $log [concat $here lint $selection]]] ne ""} {
        return [{*}$fail $result lint [why $log $message]]
    }
    set dependencies [fact $port -q echo depof:$name]
    if {$dependencies ne ""} {
        if {[set message [run $log [concat [list $port -N -d install --unrequested] $dependencies]]] ne ""} {
            return [{*}$fail $result install "a dependency failed to install: [why $log $message]"]
        }
    }
    foreach phase {fetch checksum} {
        if {[set message [run $log [concat $here -d $phase $selection]]] ne ""} {
            return [{*}$fail $result $phase [why $log $message]]
        }
    }
    # Its dependencies are in place, as CI's install-port has them, unless
    # its variants ask for others, which install then brings.
    set install [concat $here -dk install --unrequested $selection]
    if {![llength $variants]} { set install [concat $here -dkn install --unrequested $selection] }
    if {[set message [run $log $install]] ne ""} {
        return [{*}$fail $result install [why $log $message]]
    }
    dict set result outcome passed
    set tests [dict get $input tests]
    if {$tests eq "skip"} {
        dict set result tests skipped
    } elseif {[declares_tests $portdir $name $variants]} {
        set message [timed $log [concat $here -d test $selection] [dict get $input test_timeout]]
        switch -- $message {
            "" { dict set result tests passed }
            "timed out" { dict set result tests timed-out }
            default { dict set result tests failed }
        }
        if {$message ne ""} {
            dict set result detail "tests: [why $log $message]"
            if {$tests eq "required"} { dict set result outcome failed; dict set result phase test }
        }
    }
    return $result
}

save running
try {
    dict set environment user [fact /usr/bin/id -un]
    dict set environment macos [fact /usr/bin/sw_vers -productVersion]
    dict set environment build [fact /usr/bin/sw_vers -buildVersion]
    dict set environment architecture [fact /usr/bin/uname -m]
    dict set environment developer_dir [fact /usr/bin/xcode-select -p]
    if {[regexp -line {^version: (.+)$} [fact /usr/sbin/pkgutil --pkg-info=com.apple.pkg.CLTools_Executables] -> tools]} {
        dict set environment tools $tools
    }
    # Xcode's version and build, in an image with Xcode; xcodebuild
    # refuses without it.
    set xcodebuild [fact /usr/bin/xcodebuild -version]
    if {[regexp -line {^Xcode (\S+)$} $xcodebuild -> xcode]} {
        dict set environment xcode $xcode
    }
    if {[regexp -line {^Build version (\S+)$} $xcodebuild -> xcodeBuild]} {
        dict set environment xcode_build $xcodeBuild
    }
    dict set environment macports [fact $port version]
    if {![info exists foreignManagers]} { set foreignManagers {/opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg} }
    foreach foreign $foreignManagers {
        if {[file exists $foreign]} { error "the image has another package manager, at $foreign" }
    }
    if {[set installed [fact $port -q installed]] ne ""} { error "the image already has ports installed: $installed" }
    if {![file exists $root/ports/PortIndex]} { error "the staged ports tree has no PortIndex" }
    set sources [open $prefix/etc/macports/sources.conf w]
    puts $sources "file://$root/ports \[default\]"
    close $sources
    package require macports
    mportinit
    set platform [dict get $input platform]
    foreach {key actual} [list OS $::macports::os_platform Version $::macports::os_major Architecture $::macports::build_arch] {
        if {[dict get $platform $key] ne $actual} { error "the guest's $key is $actual, not [dict get $platform $key]" }
    }
} on error {message} {
    save errored $message
    exit 1
}
set index 0
foreach target [dict get $input targets] {
    incr index
    if {[catch {build $index $target} result]} {
        save errored "building [dict get $target id]: $result"
        exit 1
    }
    lappend results $result
    save running
}
save finished
