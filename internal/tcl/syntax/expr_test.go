package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func render(src []byte, e Expr) string {
	switch e := e.(type) {
	case Number, Text, Bareword:
		return ExprSpan(e).Text(src)
	case Variable:
		return "$" + e.Name.Text(src)
	case Call:
		return e.CmdSub.Span.Text(src)
	case Unary:
		return "(" + e.Op + " " + render(src, e.X) + ")"
	case Binary:
		return "(" + e.Op + " " + render(src, e.L) + " " + render(src, e.R) + ")"
	case Group:
		return "(group " + render(src, e.X) + ")"
	}
	return "?"
}

func parseExpr(t *testing.T, source string) ([]byte, Expr) {
	t.Helper()
	src := []byte(source)
	e, errs := ParseExpr(src, span(0, len(src)))
	require.Empty(t, errs, source)
	return src, e
}

// The tree gives a reader the shape of a condition: which operand a
// comparison holds, in Tcl's precedence, with every span the source's.
func TestParseExprShapesPrecedenceAndSpans(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		`${os.platform} eq "darwin" && ${os.major} < 18 || ![variant_isset x]`: `(|| (&& (eq $os.platform "darwin") (< $os.major 18)) (! [variant_isset x]))`,
		"\n    ${os.major} >= 17 \\\n && $x\n":                                 `(&& (>= $os.major 17) $x)`,
		`(${a} eq "x" && ${b}) || [f]`:                                         `(|| (group (&& (eq $a "x") $b)) [f])`,
		`${perl5.variant} eq {} && ${perl5.require_variant}`:                   `(&& (eq $perl5.variant {}) $perl5.require_variant)`,
		`${os.major} in {21 22}`:                                               `(in $os.major {21 22})`,
		`${os.major} >= 17 + 5 * 2`:                                            `(>= $os.major (+ 17 (* 5 2)))`,
		`-1 < ${x} && !!$y`:                                                    `(&& (< (- 1) $x) (! (! $y)))`,
		`true && ${x} ne no`:                                                   `(&& true (ne $x no))`,
		`[vercmp ${xcodeversion} >= 12.0] && ${os.major} <= 10.15`:             `(&& [vercmp ${xcodeversion} >= 12.0] (<= $os.major 10.15))`,
		`$x(i) == 0x1f`:                                                        `(== $x 0x1f)`,
	} {
		src, e := parseExpr(t, source)
		require.Equal(t, want, render(src, e), source)
	}
	src, e := parseExpr(t, ` ${os.major} < 18 `)
	require.Equal(t, "${os.major} < 18", ExprSpan(e).Text(src), "a node's span is its own text")
	binary := e.(Binary)
	require.Equal(t, "18", ExprSpan(binary.R).Text(src))
	require.True(t, ComparisonOps[binary.Op])
}

func TestExprVariablesReachQuotedAndSubstitutedReadsButNotBracedText(t *testing.T) {
	t.Parallel()
	src, e := parseExpr(t, `${a} eq "x${b}" && [vercmp ${c} [lindex $d 0]] && {${e}} ne $f(i)`)
	var names []string
	for _, read := range ExprVariables(src, e) {
		names = append(names, read.Name.Text(src))
	}
	require.Equal(t, []string{"a", "b", "c", "d", "f"}, names, "braced text reads nothing; an indexed variable is still a read")
}

func TestParseExprRefusesWhatItDoesNotModelAndWhatTclWouldReject(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]ErrorType{
		`${a} ? 1 : 0`:                   ExprUnsupported,
		`int(${a}) > 1`:                  ExprUnsupported,
		`${a} & 1`:                       ExprUnsupported,
		`${a} ** 2`:                      ExprUnsupported,
		`${a} << 1`:                      ExprUnsupported,
		`~${a}`:                          ExprUnsupported,
		`[fortran_variant_name] eq "" ]`: ExprUnexpected,
		`${a} eq`:                        ExprUnexpected,
		`foo eq "x"`:                     ExprUnexpected,
		``:                               ExprUnexpected,
		`(${a}`:                          ExprUnexpected,
		`${a} eq "unterminated`:          UntermQuote,
		`[variant_isset x`:               UntermCmdSub,
	} {
		src := []byte(source)
		_, errs := ParseExpr(src, span(0, len(src)))
		require.NotEmpty(t, errs, source)
		require.Equal(t, want, errs[0].Type, source)
	}
}
