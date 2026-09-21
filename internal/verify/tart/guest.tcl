package require json
package require json::write
json::write indented false
set root /var/tmp/dockhand2
set fd [open $root/input.json r]
set input [json::json2dict [read $fd]]
close $fd
set spec [dict get $input Spec]
set target [dict get $spec Target]
set name [dict get $target Name]
set prefix [dict get $input Prefix]
set log [open $root/build.log a]
fconfigure $log -buffering line
set steps {}
set failure null
set detail ""
set testOmission ""
set testFailure ""
set testTimeout 1800
if {[dict exists $input TestTimeoutSeconds] && [dict get $input TestTimeoutSeconds] > 0} {set testTimeout [dict get $input TestTimeoutSeconds]}
set environment [dict create NoActivePorts false NoForeignPackageManagers false]
set runUser ""

proc diagnostic {args} {
    if {[catch {exec {*}$args 2>/dev/null} value]} {return ""}
    return [string trim $value]
}
proc jsonStrings {values} {
    set encoded {}
    foreach value $values {lappend encoded [json::write string $value]}
    return [json::write array {*}$encoded]
}
proc environmentJSON {} {
    global environment
    set fields {}
    dict for {key value} $environment {
        if {$key ni {NoActivePorts NoForeignPackageManagers}} {set value [json::write string $value]}
        lappend fields $key $value
    }
    return [json::write object {*}$fields]
}

proc save {state verdict} {
    global root input steps failure detail testOmission testFailure
    set data [json::write object Protocol 1 ID [json::write string [dict get $input ID]] Digest [json::write string [dict get $input Digest]] Environment [environmentJSON] TestOmission [json::write string $testOmission] TestFailure [json::write string $testFailure] State [json::write string $state] Verdict [json::write string $verdict] Steps [json::write array {*}$steps] Failure $failure Detail [json::write string $detail]]
    set fd [open $root/result.json.tmp w]
    puts $fd $data
    close $fd
    file rename -force $root/result.json.tmp $root/result.json
}
# launch starts a command whose merged output is read through the returned
# channel; a test prelude may define it first to stand in for port.
if {[llength [info procs launch]] == 0} {
    proc launch {argv} {return [open |[concat $argv 2>@1] r]}
}
proc pump {chan} {
    global log runDone
    set data [read $chan]
    if {$data ne ""} {puts -nonewline $log $data; flush $log}
    if {[eof $chan]} {set runDone eof}
}
# timed runs a command with a deadline, returning "" on success or a message.
# A run past its deadline is terminated, then killed, and reported as such.
proc timed {argv seconds} {
    global runDone
    set chan [launch $argv]
    fconfigure $chan -blocking 0 -buffering none
    set runDone ""
    fileevent $chan readable [list pump $chan]
    set timer [after [expr {$seconds * 1000}] [list set runDone timeout]]
    vwait runDone
    after cancel $timer
    fileevent $chan readable {}
    if {$runDone eq "timeout"} {
        foreach p [pid $chan] {catch {exec kill -TERM $p}}
        after 10000
        foreach p [pid $chan] {catch {exec kill -KILL $p}}
        fconfigure $chan -blocking 1
        catch {close $chan}
        return "timed out after ${seconds}s"
    }
    fconfigure $chan -blocking 1
    if {[catch {close $chan} message]} {return $message}
    return ""
}
# summarize reads the failing step's own part of the build log, after the
# marker line step wrote before it, and keeps what MacPorts said rather than
# the exit message exec gives: its Error lines up to the last "Failed to
# <phase> <package>:" line, which names the failing package and phase, and for
# a distfile that failed to fetch every mirror tried for it with the reason
# each gave. MacPorts chooses those mirrors, so the log is the only place
# they are named. A fallback that later succeeded is not a failure and is
# left out.
proc summarize {text marker} {
    set text "\n$text"
    set at [string last "\n$marker\n" $text]
    if {$at >= 0} {set text [string range $text [expr {$at + [string length $marker] + 2}] end]}
    set errors {}
    set attempts {}
    set pending ""
    set package ""
    set failedphase ""
    set last -1
    foreach line [split $text "\n"] {
        if {[regexp {^(?:--->\s+)?Attempting to fetch (\S+)$} $line -> url]} {
            set pending $url
            continue
        }
        if {$pending ne "" && [regexp {^DEBUG: Fetching (\S+) failed: (.*)$} $line -> url reason] && $url eq $pending} {
            lappend attempts [list $url [string trim $reason]]
        }
        set pending ""
        if {[regexp {^Error: (.*)$} $line -> message]} {
            set message [string trim $message]
            if {$message eq ""} continue
            lappend errors $message
            if {[regexp {^Failed to ([^\s:]+) ([A-Za-z0-9_.+-]+):} $message -> phase found]} {
                set failedphase $phase
                set package $found
                set last [expr {[llength $errors] - 1}]
            }
        }
    }
    if {$last >= 0} {
        set errors [lrange $errors 0 $last]
    } else {
        set kept {}
        foreach message $errors {
            if {![regexp {^(See |Follow https://guide\.macports\.org|Processing of port )} $message]} {lappend kept $message}
        }
        set errors $kept
    }
    if {[llength $errors] > 6} {set errors [concat [lrange $errors 0 4] [list [lindex $errors end]]]}
    set distfiles {}
    foreach message $errors {
        if {[regexp {^Failed to fetch ([^\s:]+):} $message -> distfile]} {lappend distfiles $distfile}
    }
    set fetches {}
    foreach attempt $attempts {
        set url [lindex $attempt 0]
        if {[file tail $url] in $distfiles && [llength $fetches] < 20} {lappend fetches $attempt}
    }
    return [dict create Detail [join $errors "; "] Package $package Phase $failedphase Fetches $fetches]
}
proc step {phase argv {stepPackage ""} {advisory 0} {seconds 0}} {
    global name log steps detail failure root runUser testFailure
    if {$stepPackage eq ""} {set stepPackage $name}
    puts $log "dockhand: $stepPackage $phase"
    if {$seconds > 0} {
        set message [timed $argv $seconds]
        set failed [expr {$message ne ""}]
    } else {
        set failed [catch {exec {*}$argv >@$log 2>@$log} message]
    }
    if {$failed} {
        set fd [open $root/build.log r]
        set text [read $fd]
        close $fd
        set cause [summarize $text "dockhand: $stepPackage $phase"]
        set why [dict get $cause Detail]
        if {$why eq ""} {set why $message}
        lappend steps [json::write object Package [json::write string $stepPackage] Phase [json::write string $phase] Command [jsonStrings $argv] User [json::write string $runUser] Verdict [json::write string failed] Detail [json::write string $why]]
    }
    if {$failed && $advisory} {
        set testFailure $why
        puts $log "dockhand: $phase failed; advisory under the declared test policy: $why"
        return
    }
    if {$failed} {
        set detail "$phase failed: $why"
        if {[dict get $cause Phase] ne ""} {
            set package [dict get $cause Package]
            set kind dependency
            if {$package eq $name} { set kind target }
            set fetches {}
            foreach attempt [dict get $cause Fetches] {
                lappend fetches [json::write object URL [json::write string [lindex $attempt 0]] Reason [json::write string [lindex $attempt 1]]]
            }
            set failure [json::write object Kind [json::write string $kind] Package [json::write string $package] Phase [json::write string [dict get $cause Phase]] Attribution [json::write string unknown] Detail [json::write string $why] Fetches [json::write array {*}$fetches]]
        }
        return -code error $why
    }
    lappend steps [json::write object Package [json::write string $stepPackage] Phase [json::write string $phase] Command [jsonStrings $argv] User [json::write string $runUser] Verdict [json::write string passed]]
}

save running unknown
set phase setup
try {
    set runUser [diagnostic /usr/bin/id -un]
    dict set environment GuestAgentVersion [diagnostic /opt/dockhand/bin/tart-guest-agent --version]
    dict set environment MacOSVersion [diagnostic /usr/bin/sw_vers -productVersion]
    dict set environment MacOSBuild [diagnostic /usr/bin/sw_vers -buildVersion]
    dict set environment Architecture [diagnostic /usr/bin/uname -m]
    set cltVersion ""
    set version [diagnostic /usr/sbin/pkgutil --pkg-info=com.apple.pkg.CLTools_Executables]
    if {[regexp -line {^version: (.+)$} $version -> cltVersion]} {
        dict set environment CommandLineToolsVersion $cltVersion
    }
    set tools [diagnostic /usr/bin/xcode-select -p]
    if {[string match */CommandLineTools $tools]} {
        dict set environment DeveloperTools command-line-tools
        dict set environment DeveloperToolsVersion $cltVersion
    } elseif {$tools ne ""} {
        dict set environment DeveloperTools xcode
        dict set environment DeveloperToolsVersion [diagnostic /usr/bin/xcodebuild -version]
    }
    dict set environment MacPortsVersion [diagnostic $prefix/bin/port version]
    set sources [open $prefix/etc/macports/sources.conf w]
    puts $sources "file://$root/ports \[default\]"
    close $sources
    foreach foreign {/opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg} {
        if {[file exists $foreign]} {error "unexpected package-manager prefix $foreign"}
    }
    dict set environment NoForeignPackageManagers true
    set installed [exec $prefix/bin/port -q installed active]
    if {[string trim $installed] ne ""} {error "prepared image already has installed ports: $installed"}
    dict set environment NoActivePorts true
    package require macports
    mportinit
    set expected [dict get $spec Config Platform]
    foreach {key actual} [list OS $::macports::os_platform Version $::macports::os_major Architecture $::macports::build_arch] {
        if {[dict get $expected $key] ne $actual} {error "guest $key is $actual; requested [dict get $expected $key]"}
    }
    if {![file exists $root/ports/PortIndex] || ![file exists $root/ports/PortIndex.quick]} {error "staged source has no PortIndex"}
    set phase setup
    set portdir [file dirname [file join $root ports [dict get $target Portfile]]]
    set variants {}
    if {[dict get $target Variants] ne "null"} {
        dict for {variant enabled} [dict get $target Variants] {
            lappend variants $variant [expr {$enabled ? "+" : "-"}]
        }
    }
    set handle [mportopen "file://$portdir" [list subport $name] $variants]
    set info [dict create {*}[mportinfo $handle]]
    if {[dict get $info name] ne $name} {error "resolved target does not match request"}
    set worker [ditem_key $handle workername]
    set declared [$worker eval {tbool test.run}]
    mportclose $handle
    set base [list $prefix/bin/port -N -D $portdir]
    if {[dict get $spec Config FromSource]} {lappend base -s}
    set selection [list subport=$name]
    foreach {variant sign} $variants {lappend selection $sign$variant}
    # Each dependent gets its own guest with the edited roots built first.
    if {[dict exists $spec Preinstall]} {
        foreach sourceTarget [dict get $spec Preinstall] {
            set sourceName [dict get $sourceTarget Name]
            set sourceDir [file dirname [file join $root ports [dict get $sourceTarget Portfile]]]
            set sourceBase [list $prefix/bin/port -N -D $sourceDir]
            if {[dict get $spec Config FromSource]} {lappend sourceBase -s}
            set sourceSelection [list subport=$sourceName]
            if {[dict get $sourceTarget Variants] ne "null"} {
                dict for {variant enabled} [dict get $sourceTarget Variants] {
                    lappend sourceSelection [expr {$enabled ? "+" : "-"}]$variant
                }
            }
            set phase build
            step build [concat $sourceBase -d build $sourceSelection] $sourceName
            set phase install
            step install [concat $sourceBase -d install $sourceSelection] $sourceName
        }
    }
    set phase lint
    step lint [concat $base lint $selection]
    set phase build
    step build [concat $base -d build $selection]
    set tests [dict get $spec Config Tests]
    if {$tests in {declared required} && [string is true -strict $declared]} {
        set phase test
        step test [concat $base -d test $selection] "" [expr {$tests eq "declared"}] $testTimeout
    } elseif {$tests eq "skip"} {
        set testOmission "Skipped by request"
    } else {
        set testOmission "Port declares no test phase"
    }
    set phase install
    step install [concat $base -d install $selection]
    save finished passed
} on error {message options} {
    if {$detail eq ""} {set detail "$phase failed: $message"}
    puts $log $detail
    if {$phase eq "setup" || $phase eq "index"} {
        set failure [json::write object Kind [json::write string infrastructure] Package [json::write string $name] Phase [json::write string $phase] Attribution [json::write string unknown] Detail [json::write string $message]]
        save finished errored
    } else {
        save finished failed
    }
}
close $log
