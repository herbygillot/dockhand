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
    global root input steps failure detail testOmission
    set data [json::write object Protocol 1 ID [json::write string [dict get $input ID]] Digest [json::write string [dict get $input Digest]] Environment [environmentJSON] TestOmission [json::write string $testOmission] State [json::write string $state] Verdict [json::write string $verdict] Steps [json::write array {*}$steps] Failure $failure Detail [json::write string $detail]]
    set fd [open $root/result.json.tmp w]
    puts $fd $data
    close $fd
    file rename -force $root/result.json.tmp $root/result.json
}
proc step {phase argv {stepPackage ""}} {
    global name log steps detail failure root runUser
    if {$stepPackage eq ""} {set stepPackage $name}
    puts $log "dockhand: $stepPackage $phase"
    if {[catch {exec {*}$argv >@$log 2>@$log} message]} {
        lappend steps [json::write object Package [json::write string $stepPackage] Phase [json::write string $phase] Command [jsonStrings $argv] User [json::write string $runUser] Verdict [json::write string failed] Detail [json::write string $message]]
        set detail "$phase failed: $message"
        set fd [open $root/build.log r]
        set text [read $fd]
        close $fd
        set matches [regexp -all -inline {Error: Failed to ([^\n]+?) ([A-Za-z0-9_.+-]+):} $text]
        if {[llength $matches] >= 3} {
            set package [lindex $matches end]
            set failedphase [lindex $matches end-1]
            set kind dependency
            if {$package eq $name} { set kind target }
            set failure [json::write object Kind [json::write string $kind] Package [json::write string $package] Phase [json::write string $failedphase] Attribution [json::write string unknown] Detail [json::write string $message]]
        }
        return -code error $message
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
    if {[dict get $spec Config Tests] eq "declared" && [string is true -strict $declared]} {
        set phase test
        step test [concat $base -d test $selection]
    } elseif {[dict get $spec Config Tests] eq "skip"} {
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
