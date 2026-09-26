# probe.tcl -- harvest macOS toolchain facts as MacPorts Base sees them.
#
# Run in a guest as the admin user:
#   /opt/local/bin/port-tclsh probe.tcl <out.json>
#
# Emits one JSON object to <out.json>. Every fact is recorded as
# {"value": ..., "error": ...}; a failing fact records its error and the
# probe carries on. Shell facts are gathered first, before mportinit cleans
# the environment, so they are the plain shell view; Base facts come from
# Base's own variables and procedures in the parent and in a port worker.

set outfile [lindex $argv 0]
if {$outfile eq ""} {
    puts stderr "usage: port-tclsh probe.tcl <out.json>"
    exit 2
}

# ---------------------------------------------------------------- JSON --

proc jstr {s} {
    set s [string map [list \\ \\\\ \" \\\" \n \\n \r \\r \t \\t \b \\b \f \\f] $s]
    # any other control character
    set out ""
    foreach c [split $s ""] {
        scan $c %c code
        if {$code < 0x20} {
            append out [format \\u%04x $code]
        } else {
            append out $c
        }
    }
    return "\"$out\""
}

proc jlist {l} {
    set parts [list]
    foreach e $l {lappend parts [jstr $e]}
    return "\[[join $parts ,]\]"
}

# facts: ordered list of {section name json-value json-error}
set ::facts [list]

# fact section name script ?list?
#   Evaluates script at global level. Records its result (as a JSON string,
#   or as a JSON array if kind is "list") or its error.
proc fact {section name script {kind str}} {
    # code 2 (a script ending in "return") counts as success
    if {[catch {uplevel #0 $script} result] == 1} {
        lappend ::facts [list $section $name null [jstr $result]]
        return ""
    }
    if {$kind eq "list"} {
        lappend ::facts [list $section $name [jlist $result] null]
    } elseif {$kind eq "bool"} {
        lappend ::facts [list $section $name [expr {$result ? "true" : "false"}] null]
    } else {
        lappend ::facts [list $section $name [jstr $result] null]
    }
    return $result
}

# run cmd... -> combined stdout+stderr; errors (non-zero exit) propagate
# with the output and exit status in the message.
proc run {args} {
    if {[catch {exec -ignorestderr -- {*}$args 2>@1} out opts]} {
        set code [dict get $opts -errorcode]
        if {[lindex $code 0] eq "CHILDSTATUS"} {
            error "exit [lindex $code 2]: $out"
        }
        error $out
    }
    return $out
}

set started [clock format [clock seconds] -gmt 1 -format %Y-%m-%dT%H:%M:%SZ]

# --------------------------------------------------------- shell facts --

fact shell sw_vers_productVersion {run /usr/bin/sw_vers -productVersion}
fact shell sw_vers_buildVersion {run /usr/bin/sw_vers -buildVersion}
fact shell sw_vers_raw {run /usr/bin/sw_vers}
fact shell uname_r {run /usr/bin/uname -r}
fact shell uname_m {run /usr/bin/uname -m}
fact shell whoami {run /usr/bin/whoami}
fact shell xcode_select_p {run /usr/bin/xcode-select -p}
fact shell xcrun_show_sdk_path {run /usr/bin/xcrun --show-sdk-path}
fact shell xcrun_show_sdk_version {run /usr/bin/xcrun --show-sdk-version}
fact shell xcrun_show_sdk_build_version {run /usr/bin/xcrun --show-sdk-build-version}
fact shell xcrun_sdk_macosx_show_sdk_path {run /usr/bin/xcrun --sdk macosx --show-sdk-path}
fact shell xcrun_find_clang {run /usr/bin/xcrun --find clang}
fact shell xcodebuild_version {run /usr/bin/xcodebuild -version}
fact shell pkgutil_pkgs_clt {run /usr/sbin/pkgutil --pkgs=com\\.apple\\.pkg\\.(CLTools_Executables|CLTools_Base|DeveloperToolsCLI|DeveloperToolsCLILeo)}
set cltinfo [fact shell pkgutil_CLTools_Executables_raw {run /usr/sbin/pkgutil --pkg-info=com.apple.pkg.CLTools_Executables}]
fact shell pkgutil_CLTools_Executables_version {
    if {![regexp -line {^version: (\S+)} $cltinfo -> v]} {error "no version line"}
    set v
}
fact shell clt_sdks {
    set l [list]
    foreach p [lsort [glob -nocomplain -directory /Library/Developer/CommandLineTools/SDKs *]] {
        if {[file type $p] eq "link"} {
            lappend l "[file tail $p] -> [file readlink $p]"
        } else {
            lappend l [file tail $p]
        }
    }
    set l
} list
fact shell xcode_apps {lsort [glob -nocomplain -directory /Applications -types d Xcode*.app]} list
fact shell xcode_sdks {
    set l [list]
    foreach app [lsort [glob -nocomplain -directory /Applications -types d Xcode*.app]] {
        set dir $app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs
        foreach p [lsort [glob -nocomplain -directory $dir *]] {
            if {[file type $p] eq "link"} {
                lappend l "$app: [file tail $p] -> [file readlink $p]"
            } else {
                lappend l "$app: [file tail $p]"
            }
        }
    }
    set l
} list
fact shell usr_bin_clang_version {run /usr/bin/clang --version}
fact shell usr_bin_clang_version_first_line {lindex [split [run /usr/bin/clang --version] \n] 0}
fact shell usr_bin_clang_v {run /usr/bin/clang -v}
fact shell usr_bin_gcc_version_first_line {lindex [split [run /usr/bin/gcc --version] \n] 0}
fact shell clt_clang_version_first_line {lindex [split [run /Library/Developer/CommandLineTools/usr/bin/clang --version] \n] 0}

# The host checks Base makes (portmain use_xcode default, get_min_command_line,
# set_xcodecltversion).
fact host_checks file_exists_usr_lib_libxcselect_dylib {file exists /usr/lib/libxcselect.dylib} bool
fact host_checks file_executable_clt_usr_bin_make {file executable /Library/Developer/CommandLineTools/usr/bin/make} bool
fact host_checks file_executable_usr_bin_clang {file executable /usr/bin/clang} bool
fact host_checks file_executable_usr_bin_clangxx {file executable /usr/bin/clang++} bool
fact host_checks file_executable_usr_bin_gcc {file executable /usr/bin/gcc} bool
fact host_checks file_exists_usr_include_sys_cdefs_h {file exists /usr/include/sys/cdefs.h} bool

# ---------------------------------------------------------- Base parent --

package require macports
array set ui_options {}
set ::mportinit_error ""
if {[catch {mportinit ui_options} err]} {
    set ::mportinit_error $err
}
fact base mportinit {expr {$::mportinit_error eq "" ? "ok" : [error $::mportinit_error]}}

fact base macports_version {macports::version}
foreach v {os_platform os_subplatform os_major os_arch os_version macos_version
           macos_version_major macosx_version build_arch universal_archs
           macosx_sdk_version macosx_deployment_target} {
    fact base $v [list set macports::$v] [expr {$v eq "universal_archs" ? "list" : "str"}]
}
# Reading xcodeversion (or xcodebuildcmd) triggers the lazy setxcodeinfo.
fact base xcodeversion {set macports::xcodeversion}
fact base xcodecltversion {set macports::xcodecltversion}
fact base developer_dir {set macports::developer_dir}
fact base xcodebuildcmd {set macports::xcodebuildcmd}
fact base xcode_license_unaccepted {set macports::xcode_license_unaccepted}
fact base cxx_stdlib {set macports::cxx_stdlib}
fact base delete_la_files {set macports::delete_la_files}
fact base prefix {set macports::prefix}

foreach tool {clang clang++ gcc g++ cc c++ llvm-gcc-4.2 gcc-4.2} {
    fact base_tools get_tool_path($tool) [list macports::get_tool_path $tool]
}
foreach tool {clang clang++ gcc g++ cc c++ llvm-gcc-4.2 gcc-4.2} {
    fact base_tools get_compiler_version($tool) [list apply {{tool} {
        set p [macports::get_tool_path $tool]
        if {$p eq ""} {error "get_tool_path returned empty"}
        macports::get_compiler_version $p $macports::developer_dir
    }} $tool]
}
# Also the version of the CLT clang, which is what the worker uses when
# use_xcode is false.
fact base_tools get_compiler_version(CLT/clang) {
    macports::get_compiler_version /Library/Developer/CommandLineTools/usr/bin/clang /Library/Developer/CommandLineTools
}

# Base's own sysinfo header, as it would appear in a port -d log.
set sysinfo_lines [list]
fact base sysinfo_header {
    set ::sysinfo_lines [list]
    rename ::ui_debug ::probe_saved_ui_debug
    proc ::ui_debug {args} {lappend ::sysinfo_lines "DEBUG: [lindex $args end]"}
    if {![info exists macports::current_phase]} {set macports::current_phase main}
    set rc [catch {macports::_log_sysinfo} err]
    rename ::ui_debug {}
    rename ::probe_saved_ui_debug ::ui_debug
    if {$rc} {error $err}
    join $::sysinfo_lines \n
}

# ---------------------------------------------------------- Base worker --

set portdir [file join [expr {[info exists env(TMPDIR)] ? $env(TMPDIR) : "/tmp"}] dockhand-probe-port-[pid]]
file mkdir $portdir
set fd [open $portdir/Portfile w]
puts $fd {# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4

PortSystem          1.0

name                dockhandprobe
version             1.0
categories          devel
license             MIT
maintainers         nomaintainer
description         throwaway probe port
long_description    A throwaway C port used to ask MacPorts Base \
                    which compiler and SDK it chooses by default.
homepage            https://example.org/
platforms           darwin
master_sites        https://example.org/dist/
checksums           rmd160  0000000000000000000000000000000000000000 \
                    sha256  0000000000000000000000000000000000000000000000000000000000000000 \
                    size    0
}
close $fd

set mport ""
fact worker mportopen {
    set ::mport [mportopen file://$::portdir {} {}]
    return ok
}

if {$mport ne ""} {
    set ::workername [ditem_key $mport workername]
    proc wset {name} {$::workername eval [list set $name]}
    proc weval {script} {$::workername eval $script}

    foreach opt {os.platform os.subplatform os.major os.arch os.version
                 xcodeversion developer_dir use_xcode
                 compiler.blacklist compiler.whitelist compiler.fallback
                 configure.compiler configure.cc configure.cxx configure.objc
                 configure.objcxx configure.cpp configure.cxx_stdlib
                 configure.build_arch configure.sdk_version configure.sdkroot
                 configure.developer_dir configure.cc_archflags
                 configure.ld_archflags configure.cflags configure.cxxflags
                 configure.cppflags configure.ldflags
                 compiler.cpath compiler.library_path
                 compiler.c_standard compiler.cxx_standard
                 macosx_deployment_target build_arch} {
        if {$opt in {compiler.blacklist compiler.whitelist compiler.fallback}} {
            fact worker $opt [list wset $opt] list
        } else {
            fact worker $opt [list wset $opt]
        }
    }
    fact worker get_compiler_fallback() {weval {portconfigure::get_compiler_fallback}} list
    fact worker configure_get_default_compiler() {weval {portconfigure::configure_get_default_compiler}}
    fact worker no_default_compiler_allowed {weval {set portconfigure::no_default_compiler_allowed}}
    fact worker get_apple_compilers_xcode_version() {weval {portconfigure::get_apple_compilers_xcode_version}} list
    fact worker get_apple_compilers_os_version() {weval {portconfigure::get_apple_compilers_os_version}} list
    fact worker get_min_command_line(clang) {weval {portconfigure::get_min_command_line clang}}
    fact worker get_min_clang() {weval {portconfigure::get_min_clang}}
    fact worker get_min_gcc() {weval {portconfigure::get_min_gcc}}
    fact worker compiler.command_line_tools_version(clang) {weval {compiler.command_line_tools_version clang}}
    fact worker find_developer_tool(clang) {weval {portconfigure::find_developer_tool clang}}
    fact worker configure_get_compiler(cc,clang) {weval {portconfigure::configure_get_compiler cc clang}}
    fact worker configure_get_compiler(cxx,clang) {weval {portconfigure::configure_get_compiler cxx clang}}
    fact worker get_clang_compilers() {weval {portconfigure::get_clang_compilers}} list
    catch {mportclose $mport}
}
catch {file delete -force $portdir}

# ---------------------------------------------------------------- emit --

set finished [clock format [clock seconds] -gmt 1 -format %Y-%m-%dT%H:%M:%SZ]
set sections [list]
set order [list]
foreach f $facts {
    lassign $f section name value err
    if {$section ni $order} {lappend order $section}
    dict lappend bysection $section "    [jstr $name]: {\"value\": $value, \"error\": $err}"
}
set body [list]
foreach section $order {
    lappend body "  [jstr $section]: {\n[join [dict get $bysection $section] ,\n]\n  }"
}
set fd [open $outfile w]
fconfigure $fd -encoding utf-8
puts $fd "\{\n  \"probe_started\": [jstr $started],\n  \"probe_finished\": [jstr $finished],\n[join $body ,\n]\n\}"
close $fd
