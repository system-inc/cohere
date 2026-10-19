package adamic

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// Resolving by name lets the same fixtures expose the missing rule on origin/main.
func unionWriteSubject() rule.Rule {
	for _, registration := range rule.Registered() {
		if registration.Rule.Name == "adamic/union-write" {
			return registration.Rule
		}
	}
	return rule.Rule{Name: "adamic/union-write", Run: func(rule.Context, any) rule.Listeners { return nil }}
}

func TestUnionWriteRefused(t *testing.T) {
	t.Parallel()
	for name, slot := range map[string]string{
		"generic-key":   "cell[key]",
		"array-literal": "cells[0]",
		"setter":        "cell.value",
		"symbol":        "cell[key]",
		"union-key":     "cell[key]",
		"generic-array": "cells[index]",
		"prefix":        "cell.value",

		"property":       "cell.value",
		"other-member":   "cell.value",
		"discriminant":   "shape.kind",
		"optional":       "record.label",
		"array":          "cells[index]",
		"tuple":          "cells[0]",
		"computed":       "cell[key]",
		"nested":         "cell.inner.value",
		"nested-element": "cell.inner[0]",
		"generic":        "cell.value",
		"generic-nested": "cell.inner.value",
		"logical":        "cell.value",
		"compound":       "cell.value",
		"increment":      "cell.value",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bytes, err := os.ReadFile("testdata/union-write/" + name + ".ts")
			if err != nil {
				t.Fatal(err)
			}
			source := string(bytes)
			if count := typeErrorCount(t, source); count != 0 {
				t.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			result := runAdamic(t, unionWriteSubject(), source)
			rule_testing.ExpectFindings(t, result, "unionWrite")
			expectSpans(t, source, result, slot)
			if len(result.Diagnostics) == 1 && !strings.Contains(result.Diagnostics[0].Message.Description, slot) {
				t.Errorf("message %q does not name slot %s", result.Diagnostics[0].Message.Description, slot)
			}
		})
	}
}

// Every refused form is a Node failure. Removing probe() is the control: the
// exact same program no longer throws, so a runtime check that always fails is caught.
func TestUnionWriteNodeProbes(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("testdata/union-write/*.ts")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 21 {
		t.Fatalf("got %d probes, want 21", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			bytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			run := func(source string) ([]byte, error) {
				file := filepath.Join(t.TempDir(), "probe.mts")
				if err := os.WriteFile(file, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				return exec.Command("node", file).CombinedOutput()
			}
			output, err := run(string(bytes))
			if err == nil || !strings.Contains(string(output), "TypeError") {
				t.Fatalf("want Node TypeError, got %v: %s", err, output)
			}
			control := strings.Replace(string(bytes), "\nprobe();", "", 1)
			if control == string(bytes) {
				t.Fatal("probe control did not change the source")
			}
			if output, err := run(control); err != nil {
				t.Fatalf("control: %v: %s", err, output)
			}
		})
	}
}

func TestUnionWriteLegalNeighbours(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct{ source, old, plant string }{
		"setter write type": {`type Cell = { get value(): number; set value(v: string | number) } | { get value(): number; set value(v: string | number | boolean) }; function put(cell: Cell): void { cell.value = 'text'; }`, "set value(v: string | number)", "set value(v: number)"},
		"nested narrowing":  {`type Cell = { inner: { kind: 's'; value: string } } | { inner: { kind: 'n'; value: number } }; function put(cell: Cell): void { if (cell.inner.kind === 's') { cell.inner.value = 'text'; } }`, "cell.inner.kind === 's'", "true"},
		"generic narrowing": {`type Cell = { kind: 's'; value: string } | { kind: 'n'; value: number }; function put<T extends Cell>(cell: T): void { if (cell.kind === 's') { cell.value = 'text'; } }`, "cell.kind === 's'", "true"},
		"read":              {`function read(cell: { value: string } | { value: number }): boolean | string { return cell.value === 'text'; }`, "cell.value === 'text'", "cell.value = 'text'"},
		"narrowed":          {`type Cell = { kind: 's'; value: string } | { kind: 'n'; value: number }; function put(cell: Cell): void { if (cell.kind === 's') { cell.value = 'text'; } }`, "cell.kind === 's'", "true"},
		"overlapping":       {`function put(cell: { n: number } | { n: number | string }): void { cell.n = 1; }`, "cell.n = 1", "cell.n = 'text'"},
		"identical":         {`function put(cell: { value: string | number } | { value: string | number }): void { cell.value = 'text'; }`, "{ value: string | number } |", "{ value: number } |"},
		"void callback":     {`function put(cell: { f: () => void } | { f: () => number }): void { cell.f = () => 1; }`, "() => 1", "() => 'text'"},
		"defined value":     {`function put(cell: { n: number | undefined } | { n: number }): void { cell.n = 1; }`, "cell.n = 1", "cell.n = undefined"},
		"plain":             {`function put(cell: { n: number | string }): void { cell.n = 'text'; }`, "{ n: number | string }", "{ n: number } | { n: number | string }"},
		"logical fits":      {`function put(cell: { n: number | undefined } | { n: 1 | undefined }): void { cell.n ??= 1; }`, "??= 1", "??= 2"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if count := typeErrorCount(t, fixture.source); count != 0 {
				t.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			rule_testing.ExpectClean(t, runAdamic(t, unionWriteSubject(), fixture.source))
			plant := strings.Replace(fixture.source, fixture.old, fixture.plant, 1)
			if plant == fixture.source {
				t.Fatal("plant did not change source")
			}
			rule_testing.ExpectFindings(t, runAdamic(t, unionWriteSubject(), plant), "unionWrite")
		})
	}
}
