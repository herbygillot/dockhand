package observe

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"slices"
	"strconv"
)

// archRead names the variables whose value is the build architecture.
func archRead(name string) bool {
	switch name {
	case "build_arch", "configure.build_arch", "os.arch":
		return true
	}
	return false
}

type platformNeeds struct {
	majors     map[int]bool
	operands   []string
	arch       bool
	exhaustive bool
}

func scanPlatformNeeds(src []byte) (platformNeeds, error) {
	n := platformNeeds{majors: map[int]bool{}}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return n, fmt.Errorf("%w: invalid context syntax", portfile.ErrUnsupported)
	}
	if err := unmodeledReads(src, script); err != nil {
		return n, err
	}
	scanner := &platformScanner{src: src, needs: &n}
	for _, cmd := range script.Direct() {
		if err := scanner.command(cmd); err != nil {
			return n, err
		}
	}
	slices.Sort(n.operands)
	return n, nil
}

// platformScanner walks a Portfile for what selects a platform: a Darwin
// major compared in an expression yields boundaries to model both sides
// of; a Darwin major read any other way, or compared to something that is
// not a number, a variable, or an option, means the contexts cannot be
// enumerated from the source; a read of the architecture makes the
// contexts architecture-dependent.
type platformScanner struct {
	src   []byte
	needs *platformNeeds
}

func (s *platformScanner) command(cmd syntax.Command) error {
	name, _ := cmd.Name(s.src)
	if name == "supported_archs" && len(cmd.Words) > 1 {
		s.needs.arch = true
	}
	windows := cmd.Expressions(s.src)
	for _, window := range windows {
		if e, errs := syntax.ParseExpr(s.src, window); len(errs) == 0 {
			if err := s.expression(e); err != nil {
				return err
			}
			continue
		}
		// Not an expression this parser models. A Darwin major inside it may
		// be selecting between contexts in a way that cannot be enumerated,
		// so it is refused rather than mistaken for a harmless read; any
		// other read still counts.
		if script, errs := syntax.ParseScript(s.src, window); len(errs) == 0 {
			for nested := range script.Commands(s.src, func(syntax.Command) bool { return true }) {
				for _, read := range nested.Reads(s.src) {
					if read.Name.Text(s.src) == "os.major" {
						return fmt.Errorf("%w: Darwin major in an expression form that is not modeled", ErrInconclusive)
					}
				}
			}
			for _, nested := range script.Direct() {
				if err := s.command(nested); err != nil {
					return err
				}
			}
		}
	}
	_, bodies, _ := cmd.Control(s.src)
	for _, word := range cmd.Words {
		isBody := false
		for _, body := range bodies {
			isBody = isBody || body.Span == word.Span
		}
		if isBody {
			if err := s.script(word); err != nil {
				return err
			}
			continue
		}
		if word.Covered(windows) {
			continue
		}
		if err := s.segments(word.Segments); err != nil {
			return err
		}
		// A braced word of any other command may be a script run later, a
		// subport's or a variant's declarations; walk it as one.
		if err := s.script(word); err != nil {
			return err
		}
	}
	return nil
}

func (s *platformScanner) script(word syntax.Word) error {
	script, ok := word.BracedScript(s.src)
	if !ok {
		return nil
	}
	for _, cmd := range script.Direct() {
		if err := s.command(cmd); err != nil {
			return err
		}
	}
	return nil
}

func (s *platformScanner) segments(segments []syntax.Segment) error {
	for _, segment := range segments {
		switch value := segment.(type) {
		case syntax.VarSub:
			s.read(value.Name.Text(s.src))
		case syntax.Quoted:
			if err := s.segments(value.Segments); err != nil {
				return err
			}
		case syntax.CmdSub:
			for _, cmd := range value.Script.Direct() {
				if err := s.command(cmd); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// read is a Darwin major or architecture read outside a recognized
// comparison.
func (s *platformScanner) read(name string) {
	if name == "os.major" {
		s.needs.exhaustive = true
	}
	if archRead(name) {
		s.needs.arch = true
	}
}

func isDarwinMajor(src []byte, e syntax.Expr) bool {
	v, ok := e.(syntax.Variable)
	return ok && !v.HasIndex && v.Name.Text(src) == "os.major"
}

func (s *platformScanner) expression(e syntax.Expr) (err error) {
	syntax.WalkExpr(e, func(node syntax.Expr) bool {
		if err != nil {
			return false
		}
		switch node := node.(type) {
		case syntax.Binary:
			if !syntax.ComparisonOps[node.Op] || node.Op == "in" || node.Op == "ni" {
				return true
			}
			left, right := isDarwinMajor(s.src, node.L), isDarwinMajor(s.src, node.R)
			if left == right {
				return true
			}
			other := node.R
			if right {
				other = node.L
			}
			err = s.boundary(other)
			return false
		case syntax.Variable:
			s.read(node.Name.Text(s.src))
		case syntax.Text:
			err = s.segments(node.Word.Segments)
		case syntax.Call:
			for _, cmd := range node.Script.Direct() {
				if err = s.command(cmd); err != nil {
					return false
				}
			}
		}
		return true
	})
	return err
}

// boundary takes the operand a Darwin major is compared against.
func (s *platformScanner) boundary(other syntax.Expr) error {
	switch other := other.(type) {
	case syntax.Number:
		if v, err := strconv.Atoi(other.Span.Text(s.src)); err == nil {
			return addBoundary(s.needs.majors, v)
		}
	case syntax.Variable:
		if !other.HasIndex {
			s.operand(other.Name.Text(s.src))
			return nil
		}
	case syntax.Call:
		if commands := other.Script.Direct(); len(commands) == 1 {
			if name, _ := commands[0].Name(s.src); name == "option" {
				if args, ok := commands[0].LiteralArgs(s.src); ok && len(args) == 1 {
					s.operand("option:" + args[0])
					return nil
				}
			}
		}
	}
	return fmt.Errorf("%w: unresolved Darwin comparison", ErrInconclusive)
}

func (s *platformScanner) operand(name string) {
	if !slices.Contains(s.needs.operands, name) {
		s.needs.operands = append(s.needs.operands, name)
	}
}

func addBoundary(majors map[int]bool, n int) error {
	if n < 8 || n > 1000 {
		return fmt.Errorf("%w: unsupported Darwin boundary %d", ErrInconclusive, n)
	}
	for _, v := range []int{n - 1, n, n + 1} {
		if v >= 8 {
			majors[v] = true
		}
	}
	return nil
}

func profilesForBoundaries(majors map[int]bool, archDependent bool, native record.Platform) ([]record.Platform, error) {
	result := []record.Platform{native}
	if native.OS != "darwin" && (archDependent || len(majors) > 0) {
		return nil, fmt.Errorf("%w: alternate platforms require Darwin modeling", ErrInconclusive)
	}
	current, _ := strconv.Atoi(native.Version)
	appendProfile := func(major int, arch string) {
		p := record.Platform{OS: native.OS, Version: strconv.Itoa(major), Architecture: arch}
		if !slices.Contains(result, p) {
			result = append(result, p)
		}
	}
	if archDependent {
		for _, arch := range []string{"arm64", "x86_64"} {
			if current >= 20 || arch != "arm64" {
				appendProfile(current, arch)
			}
		}
	}
	// A boundary's neighbors can be a Darwin that never shipped, 26 among
	// them, since the numbers are not consecutive; no Mac describes one.
	var versions []int
	for major := range majors {
		if _, err := macos.ProductForDarwin(major); err == nil && major <= current {
			versions = append(versions, major)
		}
	}
	slices.Sort(versions)
	for _, major := range versions {
		appendProfile(major, "x86_64")
		if archDependent && major >= 20 {
			appendProfile(major, "arm64")
		}
	}
	return result, nil
}
