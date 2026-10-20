// Command legacymacos checks a legacy build of dockhand, one for macOS 10.13
// to 11, against the symbols those releases' libraries export.
//
// dyld binds every symbol a Go binary imports when it loads it, so one a
// release's libraries lack stops the binary before main. Go 1.25 and later
// import SecTrustCopyCertificateChain, which macOS 12 added; build.sh
// builds with x509.patch, which takes Go 1.24's calls back, and runs this
// on what it built. It reports any import imports.txt doesn't list, which
// is how a Go release or a change to dockhand that imports something newer
// is found before someone's Mac finds it. It is a developer tool,
// independent of the dockhand CLI.
package main
