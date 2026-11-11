package native

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// TestEmptyTextAndNumericSeparatorsMatchTheFork holds the nine inputs @system_adamic's JSON audit found
// cohere and Prettier 3.9.6 disagree on (#w6vtsrn), and their neighbours, to the fork's recorded answers:
// the same output, or a refusal on both sides.
//
// Empty or whitespace-only text is core.js's to answer, before any parser, so it is asked of every
// language. A numeric separator is Babel's to judge, so the JSON cases run under both of JSON's parsers,
// json (probe.json) and json-stringify (package.json), well-formed separators beside malformed ones.
func TestEmptyTextAndNumericSeparatorsMatchTheFork(test *testing.T) {
	test.Parallel()
	type input struct{ fileName, text string }
	var inputs []input
	for _, fileName := range []string{"probe.json", "package.json", "probe.ts", "probe.tsx", "probe.js", "probe.md",
		"probe.css", "probe.graphql", "probe.yaml"} {
		for _, text := range []string{"", " ", "\n", "\n\n\t \n", "\u00a0\u2028\ufeff", "\ufeff", "\ufeff\n"} {
			inputs = append(inputs, input{fileName, text})
		}
	}
	for _, fileName := range []string{"probe.json", "package.json"} {
		for _, text := range []string{
			// The audit's three, refused by Prettier.
			"[1__0]", "[1_]", "[0x_1]",
			// Malformed elsewhere in a literal.
			"[1_.5]", "[1._5]", "[1_e5]", "[1e_5]", "[1e5_]", "[.5_]", "[0x1_]", "[0x1__f]", "[0b_1]", "[0o_7]",
			"[0_1]", "[1_0n]",
			// Well-formed, which Prettier accepts or refuses on its own terms; the recorded answer says which.
			"[1_0]", "[1_000_000]", "[1_0.5_5]", "[1e1_0]", "[1e+1_0]", "[1e+_10]", "[0x1_f]", "[0xa_b]",
			"[0b1_0]", "[0o7_1]", "[.5_5]", "{\"a\": 1_0}", "{1_0: 1}",
		} {
			inputs = append(inputs, input{fileName, text + "\n"})
		}
	}

	options := format_options.Default()
	oracle := oracle_testing.Open(test, test.Name()).Engine(options)
	formatter := Formatter{Options: options}
	compared, refusedByBoth := 0, 0
	for _, each := range inputs {
		expected, oracleError := oracle.Format(each.fileName, each.text)
		actual, nativeError := formatter.Format(each.fileName, each.text)
		switch {
		case oracleError != nil && nativeError != nil:
			refusedByBoth++
		case oracleError != nil:
			test.Errorf(
				"%s %q: the fork refuses it (%v) and Formatter printed %q",
				each.fileName,
				each.text,
				oracleError,
				actual,
			)
		case nativeError != nil:
			test.Errorf(
				"%s %q: the fork prints %q and Formatter refused: %v",
				each.fileName,
				each.text,
				expected,
				nativeError,
			)
		default:
			compared++
			if actual != expected {
				test.Errorf("%s %q: the fork prints %q and Formatter %q", each.fileName, each.text, expected, actual)
			}
		}
	}
	// Both answers must occur, so a side that refuses everything, or nothing, can't read as agreement.
	if compared == 0 || refusedByBoth == 0 {
		test.Errorf("%d compared and %d refused by both: one kind of answer is missing", compared, refusedByBoth)
	}
	test.Logf("%d inputs: %d printed alike, %d refused by both", len(inputs), compared, refusedByBoth)
}

// TestModuleAndCommonJsTypeScriptMatchTheFork holds `.mts` and `.cts` to the fork's recorded answer for the
// same text under the same name (#9v2x63d). Prettier gives both its typescript parser, and typescript-estree
// parses them without JSX, so the `<number>a` cast reads as a cast; upstream's /\.ts$/ misses both names, so
// the arrow's lone type parameter keeps its `<T,>`. `.ts` is the control that drops the comma, and `.tsx` the
// one that refuses the cast, on both sides. A printer must be registered for every way a file reaches one:
// its own parse (Register), the program's tree (RegisterParsed) and an embed that names it (RegisterDoc).
func TestModuleAndCommonJsTypeScriptMatchTheFork(test *testing.T) {
	test.Parallel()
	text := "import  {join}  from \"node:path\"\nconst a  =   1\nconst b = <number>a\n" +
		"const identity = <T>(value: T) => value\nexport default join(String(b), String(identity(a)))\n"
	options := format_options.Default()
	oracle := oracle_testing.Open(test, test.Name()).Engine(options)
	formatter := Formatter{Options: options}
	printed := map[string]string{}
	for _, fileName := range []string{"probe.ts", "probe.mts", "probe.cts", "probe.tsx"} {
		expected, oracleError := oracle.Format(fileName, text)
		actual, nativeError := formatter.Format(fileName, text)
		if fileName == "probe.tsx" {
			if oracleError == nil || nativeError == nil {
				test.Errorf("%s: want the cast refused under JSX by both, the fork says %v and Formatter %v",
					fileName, oracleError, nativeError)
			}
			continue
		}
		if oracleError != nil {
			test.Fatalf("%s: the fork refused it: %v", fileName, oracleError)
		}
		if nativeError != nil || actual != expected {
			test.Errorf("%s: the fork prints %q and Formatter %q, %v", fileName, expected, actual, nativeError)
		}
		printed[fileName] = actual

		fromTree, treeError := formatter.FormatParsed(fileName, text, estree.ParseSourceFile(fileName, text))
		if treeError != nil || fromTree != expected {
			test.Errorf("%s: from the program's tree, the fork prints %q and Formatter %q, %v",
				fileName, expected, fromTree, treeError)
		}
		// Without these two the tree is parsed again and the embed formatted as text, which print the same,
		// so their presence is asked of the registry.
		extension := filepath.Ext(fileName)
		mutex.RLock()
		_, takesTree := parsedPrinters[extension]
		_, embeds := docPrinters[extension]
		mutex.RUnlock()
		if !takesTree || !embeds {
			test.Errorf("%s: takes a tree %v, embeds as a doc %v, want both", fileName, takesTree, embeds)
		}
	}
	if printed["probe.mts"] == printed["probe.ts"] || printed["probe.cts"] == printed["probe.ts"] {
		test.Errorf("`.mts` or `.cts` printed as `.ts` does, so the name never reached the printer: %q", printed)
	}
}
