package adamic

import (
	"os"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// Resolve by registration so these fixtures also run on the pre-change engine,
// where no rule owns this hole and the empty subject reports no findings.
func indexedAccessSubject() rule.Rule {
	for _, registration := range rule.Registered() {
		if registration.Rule.Name == "adamic/no-indexed-access-write" {
			return registration.Rule
		}
	}
	return rule.Rule{Name: "adamic/no-indexed-access-write", Run: func(rule.Context, any) rule.Listeners { return nil }}
}

func TestIndexedAccessWrite(t *testing.T) {
	t.Parallel()
	refused, err := os.ReadFile("testdata/indexed-access/refused.ts")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := os.ReadFile("testdata/indexed-access/accepted.ts")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"runtime probe": string(refused),
		"original": `function test<T extends { readonly value: number }>(): T | undefined {
 const value: T['value'] = 2;
 console.log(String(value));
 return undefined;
}
test<{ readonly value: 1 }>();`,
		"assignment": `function make<T extends { readonly value: number }>(source: T): T['value'] {
 let value: T['value'] = source.value;
 value = 2;
 return value;
}`,
		"return":      `function make<T extends { readonly value: number }>(): T['value'] { return 2; }`,
		"nested":      `function make<T extends { readonly inner: { readonly value: number } }>(): T['inner']['value'] { const value: T['inner']['value'] = 2; return value; }`,
		"nested slot": `function make<T extends { readonly value: number }>(): { readonly value: T['value'] } { return { value: 2 }; }`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if count := typeErrorCount(t, source); count != 0 {
				t.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			result := runAdamic(t, indexedAccessSubject(), source)
			rule_testing.ExpectFindings(t, result, "indexedAccessWrite")
			expectSpans(t, source, result, "2")
			if len(result.Diagnostics) == 1 {
				message := result.Diagnostics[0].Message.Description
				for _, text := range []string{"T[", "2", "number"} {
					if !strings.Contains(message, text) {
						t.Errorf("message %q lacks %q", message, text)
					}
				}
			}
		})
	}
	for name, source := range map[string]string{
		"read from T":        string(accepted),
		"nested slot from T": `function make<T extends { readonly value: number }>(source: T): { readonly value: T['value'] } { return source; }`,
		"generic key":        `function make<T extends { readonly value: number }, K extends 'value'>(source: T, key: K): T[K] { const value: T[K] = source[key]; return value; }`,
		"nested read":        `function make<T extends { readonly inner: { readonly value: number } }>(source: T): T['inner']['value'] { const value: T['inner']['value'] = source.inner.value; return value; }`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if count := typeErrorCount(t, source); count != 0 {
				t.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			rule_testing.ExpectClean(t, runAdamic(t, indexedAccessSubject(), source))
			// Plant a concrete source into the same slot: each clean fixture must expose a refusal.
			plant := strings.NewReplacer("source.value", "2", "source[key]", "2", "source.inner.value", "2", "return source;", "return { value: 2 };").Replace(source)
			result := runAdamic(t, indexedAccessSubject(), plant)
			rule_testing.ExpectFindings(t, result, "indexedAccessWrite")
			expectSpans(t, plant, result, "2")
		})
	}
}

// Both spread copies and contextual methods can move a concrete value into an
// indexed generic slot. Each refused file is also a tsc-accepted Node failure.
func TestIndexedAccessWriteEdges(t *testing.T) {
	t.Parallel()
	for name, span := range map[string]string{
		"spread": "source", "spread-override": "2", "method-return": "get", "method-argument": "put",
		"method-reused": "result", "method-mixed-return": "get",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bytes, err := os.ReadFile("testdata/indexed-access/" + name + ".ts")
			if err != nil {
				t.Fatal(err)
			}
			source := string(bytes)
			if count := typeErrorCount(t, source); count != 0 {
				t.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			result := runAdamic(t, indexedAccessSubject(), source)
			rule_testing.ExpectFindings(t, result, "indexedAccessWrite")
			expectSpans(t, source, result, span)
		})
	}
}

func TestIndexedAccessWriteEdgesStayClean(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		bytes, err := os.ReadFile("testdata/indexed-access/" + name + ".ts")
		if err != nil {
			t.Fatal(err)
		}
		return string(bytes)
	}
	for name, fixture := range map[string]struct{ source, old, plant, span string }{
		"plain block method":  {read("accepted-method"), "source.value", "2", "get"},
		"expression arrow":    {read("accepted-arrow"), "source.value", "2", "() => 2"},
		"function expression": {read("accepted-function"), "source.value", "2", "function () { return 2; }"},
		"spread from T": {
			`function make<T extends { readonly value: number }>(source: T): { readonly value: T['value'] } { return { ...source }; }`,
			"source: T", "source: { readonly value: number }", "source",
		},
		"overridden spread": {
			`function make<T extends { readonly value: number }>(source: T, other: { readonly value: number }): { readonly value: T['value'] } { return { ...other, value: source.value }; }`,
			"source.value", "2", "2",
		},
		"contextual method read": {
			`function make<T extends { readonly value: number }>(source: T): { get(): T['value'] } { return { get() { const ignored = () => 2; return source.value; } }; }`,
			"source.value", "2", "get",
		},
		"method accepts constraint": {
			`function make<T extends { readonly value: number }>(source: T): { put(value: number): T['value'] } { return { put(value: number): T['value'] { return source.value; } }; }`,
			"put(value: number): T['value'] { return source.value; }", "put(value: T['value']): T['value'] { return value; }", "put",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if count := typeErrorCount(t, fixture.source); count != 0 {
				t.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			rule_testing.ExpectClean(t, runAdamic(t, indexedAccessSubject(), fixture.source))
			plant := strings.Replace(fixture.source, fixture.old, fixture.plant, 1)
			if count := typeErrorCount(t, plant); count != 0 {
				t.Fatalf("plant tsgo diagnostics: %d, want 0", count)
			}
			result := runAdamic(t, indexedAccessSubject(), plant)
			rule_testing.ExpectFindings(t, result, "indexedAccessWrite")
			expectSpans(t, plant, result, fixture.span)
		})
	}
}
