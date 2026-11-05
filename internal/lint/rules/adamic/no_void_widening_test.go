package adamic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

func TestNoVoidWidening(test *testing.T) {
	test.Parallel()
	files, globError := filepath.Glob("testdata/void-widening/*.ts")
	if globError != nil {
		test.Fatal(globError)
	}
	if len(files) != 59 {
		test.Fatalf("got %d fixtures, want 59", len(files))
	}
	for _, file := range files {
		test.Run(filepath.Base(file), func(test *testing.T) {
			test.Parallel()
			contents, readError := os.ReadFile(file)
			if readError != nil {
				test.Fatal(readError)
			}
			source := string(contents)
			if count := typeErrorCount(test, source); count != 0 {
				test.Fatalf("type diagnostics: %d, want 0", count)
			}
			result := runAdamic(test, NoVoidWidening, source)
			if strings.HasPrefix(filepath.Base(file), "legal-") {
				rule_testing.ExpectClean(test, result)
				// Plant the same runtime hole in every quiet fixture. Its assertion must detect it.
				source += "\n{ const make: () => void = () => 'x'; const wider: () => number | void = make; " +
					"const next = wider(); if (next) console.log(next.toFixed(1)); }\n"
				rule_testing.ExpectFindings(test, runAdamic(test, NoVoidWidening, source), "voidWidening")
				return
			}
			rule_testing.ExpectFindings(test, result, "voidWidening")
			span := "make"
			switch filepath.Base(file) {
			case "array.ts", "object.ts", "tuple.ts", "map.ts", "promise-any.ts", "promise-union.ts",
				"mutable-promise.ts", "promise-union-origin.ts":
				span = "narrow"
			case "promise-body-call.ts":
				span = "make()"
			case "call-result.ts":
				span = "makeCallback()"
			case "returned.ts":
				span = "wrap"
			case "received.ts":
				span = "consume"
			}
			expectSpans(test, source, result, span)
			if len(result.Diagnostics) == 1 {
				text := result.Diagnostics[0].Message.Description
				for _, part := range []string{span, "void", "return"} {
					if !strings.Contains(text, part) {
						test.Errorf("message %q lacks %q", text, part)
					}
				}
			}
		})
	}
}
