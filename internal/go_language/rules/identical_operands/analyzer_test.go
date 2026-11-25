package identical_operands

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa4000"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// upstreamPackages are staticcheck's own fixtures for SA4000, copied unchanged from its testdata/go1.0, go1.18 and
// go1.22 into testdata/src under directories of the same names, each holding a package of one name, so the version
// folder is the first element of each import path. The versions set the language of staticcheck's runs, which SA4000
// never reads beyond what the files need, generics and math/rand/v2, which the toolchain's own version allows. The
// go1.0 package imports "C", so loading it runs cgo, whose generated `0 == 0` the original spares.
var upstreamPackages = []string{
	"go1.0/CheckLhsRhsIdentical", "go1.18/CheckLhsRhsIdentical", "go1.22/CheckLhsRhsIdentical",
}

// staticcheck's fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// operator, every kind of type the check tells floats apart from, the spared random calls, the compile-time assertion
// of comparability and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4000.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, []string{"parity"})...)
}

// The differences fixture holds the expressions where the two could most plausibly part: a comment inside an operand,
// type parameters of structs, and line directives in a file generated and one not. It records no difference, since
// the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4000.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// literalSpelling is two spellings of literals of one kind, and whether literalsPrintSame should settle from them
// alone whether the two print the same.
type literalSpelling struct {
	kind      token.Token
	left      string
	right     string
	isSettled bool
}

// Wherever literalsPrintSame settles two literals by their spellings, its answer is whether gofmt's printer, which
// Render is, prints them the same, and the spellings it leaves open are the ones the printer may make one. Each is
// parsed, as the scanner hands a literal over, so a raw string's carriage return is already gone. The spellings sit
// in strings, so gofmt over this file can't normalize them into one another as it would in a fixture.
func TestLiteralSpellingsSettleAsThePrinterPrints(test *testing.T) {
	test.Parallel()
	spellings := []literalSpelling{
		// One spelling prints one text.
		{token.INT, "0", "0", true},
		{token.FLOAT, "1.5", "1.5", true},
		{token.IMAG, "1i", "1i", true},
		{token.CHAR, "'a'", "'a'", true},
		{token.STRING, "`a`", "`a`", true},
		{token.STRING, "`a\r\nb`", "`a\nb`", true},
		// One value spelled apart beyond the case of a letter prints apart.
		{token.INT, "0x1", "1", true},
		{token.INT, "07", "0o7", true},
		{token.INT, "1_000", "1000", true},
		{token.FLOAT, "1.0", "1.", true},
		{token.FLOAT, "1e3", "1000.", true},
		{token.CHAR, "'a'", "'\\x61'", true},
		{token.CHAR, "'a'", "'\\u0061'", true},
		{token.STRING, `"a"`, "`a`", true},
		{token.STRING, "`A`", "`a`", true},
		// Apart only in the case of a letter, which the printer may lower or keep, and an imaginary number, whose
		// leading zeros the printer drops: left to the printer.
		{token.INT, "0X1F", "0x1F", false},
		{token.INT, "0B101", "0b101", false},
		{token.INT, "0O17", "0o17", false},
		{token.INT, "0xa", "0xA", false},
		{token.FLOAT, "1E3", "1e3", false},
		{token.FLOAT, "0X1P4", "0x1p4", false},
		{token.FLOAT, "0x1P4", "0x1p4", false},
		{token.IMAG, "01i", "1i", false},
		{token.IMAG, "0i", "00i", false},
		{token.IMAG, "1i", "1.0i", false},
	}
	for _, spelling := range spellings {
		left := parsedLiteral(test, spelling.kind, spelling.left)
		right := parsedLiteral(test, spelling.kind, spelling.right)
		same, isSettled := literalsPrintSame(left, right)
		if printedSame := printedLiteral(test, left) == printedLiteral(test, right); isSettled && same != printedSame {
			test.Errorf("%s and %s: settled as printing the same %t, but the printer says %t", spelling.left,
				spelling.right, same, printedSame)
		}
		// The spellings that settle it still do, so the shortcut keeps saving the printing it was written to save.
		if isSettled != spelling.isSettled {
			test.Errorf("%s and %s: settled %t, want %t", spelling.left, spelling.right, isSettled, spelling.isSettled)
		}
	}
}

// parsedLiteral is spelling parsed as a literal of kind, failing the test if it is anything else.
func parsedLiteral(test *testing.T, kind token.Token, spelling string) *ast.BasicLit {
	test.Helper()
	expression, parseError := parser.ParseExpr(spelling)
	if parseError != nil {
		test.Fatalf("parsing %s: %v", spelling, parseError)
	}
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != kind {
		test.Fatalf("%s parsed as %T, want a literal of kind %s", spelling, expression, kind)
	}
	return literal
}

// printedLiteral is literal as gofmt's printer writes it, as Render prints a literal operand.
func printedLiteral(test *testing.T, literal *ast.BasicLit) string {
	test.Helper()
	printed := &bytes.Buffer{}
	if printError := format.Node(printed, token.NewFileSet(), literal); printError != nil {
		test.Fatalf("printing %s: %v", literal.Value, printError)
	}
	return printed.String()
}
