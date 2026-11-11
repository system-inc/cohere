package native

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/format/printing"
)

// TestUpdateTargetsAndTopLevelAwaitMatchTheFork holds the 29 inputs stage 1's tsprinter found cohere and Prettier
// 3.9.6 disagree on over tsc's conformance corpus (#0yhe787), and their neighbours, to the fork's recorded
// answers: the same output, or a refusal on both sides.
//
// Two families. An update of something that cannot be assigned, `1++`, which typescript-estree's syntax check
// refuses and TypeScript's parser accepts; its neighbours are the targets that can be assigned, which must still
// print. And `await (x)` in a file with no import or export, which Prettier reads as a module and so as an await
// operator; its neighbours are the files where it stays a call, one that only commonjs reads and one where the
// await is inside a function that is not async.
//
// Each input is also formatted from a tree parsed with TypeScript's own module detection, the tree the fix
// engine's guard hands the formatter, and must print the same: the handed tree must not bring the call back.
func TestUpdateTargetsAndTopLevelAwaitMatchTheFork(test *testing.T) {
	test.Parallel()
	sources := []string{
		// The corpus's 29, as stage 1 pinned them (tsc-upstream-differences.json at adamic eb3750d1).
		"!!await (((((((p)))))))", "1 ++", "(1)++", "(1)--", "++(1)", "--(1)", "(1 + 2)++", "(1 + 2)--", "++(1 + 2)",
		"--(1 + 2)", "(x + x)++", "(x + x)--", "++(x + x)", "--(x + x)", "await(Promise.resolve(1))",
		"!!await (((((((p)))))));", "1 ++;", "(1)++;", "(1)--;", "++(1);", "--(1);", "(1 + 2)++;", "(1 + 2)--;",
		"++(1 + 2);", "--(1 + 2);", "(x + x)++;", "(x + x)--;", "++(x + x);", "--(x + x);",
		// Targets that can be assigned, which must still print.
		"x++;", "--x;", "(x)++;", "((x))--;", "a.b++;", "a[0]--;", "(a.b)++;", "x!++;", "(x as any)++;",
		"(x satisfies number)--;", "(<any>x)++;", "this.x++;", "a.b.c[d]++;",
		// Targets that cannot, beyond the corpus's.
		"this++;", "f()++;", "a?.b++;", "(a?.b)--;", "++x++;", "function f() {\n  return 1++;\n}",
		"for (;;) {\n  --(a + b);\n}", "class A {\n  m() {\n    this++;\n  }\n}", "const y = !1++;",
		// The await family's neighbours.
		"await(x);", "await (x) + 1;", "const value = await(Promise.resolve(1));",
		"async function f() {\n  await(x);\n}",
		"function f() {\n  return await(x);\n}", "var await = 1;\nawait(x);", "import { x } from \"y\";\nawait(x);",
		"export {};\nawait(x);", "await;", "await(1)++;",
	}
	options := format_options.Default()
	oracle := oracle_testing.Open(test, test.Name()).Engine(options)
	formatter := Formatter{Options: options}
	compared, refusedByBoth, refusedAsUpdates := 0, 0, 0
	for _, fileName := range []string{"probe.ts", "probe.tsx"} {
		for _, source := range sources {
			text := source
			if !strings.HasSuffix(text, ";") && !strings.HasSuffix(text, "}") {
				text += ";"
			}
			text += "\n"
			expected, oracleError := oracle.Format(fileName, text)
			actual, nativeError := formatter.Format(fileName, text)
			switch {
			case oracleError != nil && nativeError != nil:
				refusedByBoth++
				// The refusal must read as a file that does not parse, which the format phase skips, and not as a
				// broken formatter.
				if strings.Contains(oracleError.Error(), "Invalid left-hand side expression in unary operation") {
					refusedAsUpdates++
					if !printing.IsSyntax(nativeError) {
						test.Errorf("%s %q: refused, but not as syntax: %v", fileName, text, nativeError)
					}
				}
			case oracleError != nil:
				test.Errorf(
					"%s %q: the fork refuses it (%v) and Formatter printed %q",
					fileName,
					text,
					oracleError,
					actual,
				)
			case nativeError != nil:
				test.Errorf(
					"%s %q: the fork prints %q and Formatter refused: %v",
					fileName,
					text,
					expected,
					nativeError,
				)
			default:
				compared++
				if actual != expected {
					test.Errorf("%s %q: the fork prints %q and Formatter %q", fileName, text, expected, actual)
				}
			}

			fromTree, treeError := formatter.FormatParsed(fileName, text, estree.ParseSourceFile(fileName, text))
			if fromTree != actual || (treeError == nil) != (nativeError == nil) {
				test.Errorf(
					"%s %q: from the guard's tree Formatter printed %q (%v), and parsing alone %q (%v)",
					fileName,
					text,
					fromTree,
					treeError,
					actual,
					nativeError,
				)
			}
		}
	}
	// Both answers must occur, so a side that refuses everything, or nothing, can't read as agreement.
	if compared == 0 || refusedAsUpdates == 0 {
		test.Errorf("%d compared and %d refused as updates: one kind of answer is missing", compared, refusedAsUpdates)
	}
	test.Logf(
		"%d inputs: %d printed alike, %d refused by both, %d of them as an update of something that cannot be assigned",
		2*len(sources),
		compared,
		refusedByBoth,
		refusedAsUpdates,
	)
}
