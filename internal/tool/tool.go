// Package tool is where dockhand meets the programs it drives: git,
// gh, tart, tar and the rest. It names them, finds them, and runs them.
//
// One Finder resolves every tool. It is built at the composition root
// and handed to each component that execs, so doctor's assessment and
// the working code run the identical lookup and cannot disagree about
// what the machine has. There is no package-level finder to reach for:
// a component that needs one is given one, and a test builds its own
// over a fake lookup.
//
// Output and Run are the two shapes of a one-shot command. Output is
// for a tool whose stdout is data and whose stderr is the story of a
// failure, which is every wrapper that words an error. Run is one
// merged transcript with the exec error as it came, which is what tart
// needs: its diagnostics land on either stream, and its callers read
// the output after a non-zero exit. IsTerminal is the one terminal
// question, answered by the kernel rather than guessed from a file
// mode.
//
// What is not here is any judgment about a tool's output. A wrapper
// that knows git's exit codes or gh's grammar lives with the package
// that speaks to that tool; this package only gets the bytes back.
package tool

// Tool names an external binary dockhand drives. Its value is what
// the Finder looks for: a bare name searched for on PATH, or — for
// Tar — the path it is pinned to.
type Tool string

// The tools dockhand knows. A value is also text: doctor renders it as
// the tool's name, and a miss reads "<name> not found on PATH". Tar is
// declared in tar.go with the reasoning its pin carries.
const (
	Git        Tool = "git"
	Gh         Tool = "gh"
	Tart       Tool = "tart"
	Curl       Tool = "curl"
	Tclsh      Tool = "tclsh"
	PortTclsh  Tool = "port-tclsh"
	Go2Port    Tool = "go2port"
	Cargo2Port Tool = "cargo2port"
)
