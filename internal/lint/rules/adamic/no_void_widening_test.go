package adamic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// Resolve by registration so the identical fixtures run on the old engine.
func voidWideningSubject() rule.Rule {
	for _, registration := range rule.Registered() {
		if registration.Rule.Name == "adamic/no-void-widening" {
			return registration.Rule
		}
	}
	return rule.Rule{Name: "adamic/no-void-widening", Run: func(rule.Context, any) rule.Listeners { return nil }}
}

func TestNoVoidWidening(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/void-widening/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 32 {
		t.Fatalf("got %d fixtures, want 32", len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			contents, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(contents)
			if count := typeErrorCount(t, source); count != 0 {
				t.Fatalf("type diagnostics: %d, want 0", count)
			}
			result := runAdamic(t, voidWideningSubject(), source)
			if strings.HasPrefix(filepath.Base(file), "legal-") {
				rule_testing.ExpectClean(t, result)
				// Plant the same runtime hole in every quiet fixture. Its assertion must detect it.
				source += "\n{ const make: () => void = () => 'x'; const wider: () => number | void = make; const next = wider(); if (next) console.log(next.toFixed(1)); }\n"
				rule_testing.ExpectFindings(t, runAdamic(t, voidWideningSubject(), source), "voidWidening")
				return
			}
			rule_testing.ExpectFindings(t, result, "voidWidening")
			span := "make"
			switch filepath.Base(file) {
			case "array.ts", "object.ts", "tuple.ts", "map.ts", "promise-any.ts", "promise-union.ts":
				span = "narrow"
			case "returned.ts":
				span = "wrap"
			case "received.ts":
				span = "consume"
			}
			expectSpans(t, source, result, span)
			if len(result.Diagnostics) == 1 {
				text := result.Diagnostics[0].Message.Description
				for _, part := range []string{span, "void", "return"} {
					if !strings.Contains(text, part) {
						t.Errorf("message %q lacks %q", text, part)
					}
				}
			}
		})
	}
}
