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
		"for-of-target":                "cell.value",
		"for-in-target":                "cell.value",
		"array-pattern":                "cell.value",
		"object-pattern":               "cell.value",
		"object-assign":                "cell",
		"nested-array-pattern":         "cell.value",
		"nested-object-pattern":        "cell.value",
		"array-default":                "cell.value",
		"object-default":               "cell.value",
		"for-of-iterable":              "cell.value",
		"array-matching-element":       "cell.value",
		"object-matching-property":     "cell.value",
		"assign-multiple-sources":      "cell",
		"assign-other-member":          "cell",
		"assign-generic":               "cell",
		"assign-nested-target":         "cell.inner",
		"pattern-generic":              "cell.value",
		"loop-nested-target":           "cell.inner.value",
		"array-default-source":         "cell.value",
		"object-default-source":        "cell.value",
		"assign-union-source":          "cell",
		"nested-array-default-source":  "cell.value",
		"nested-object-default-source": "cell.value",
		"for-of-element":               "cell['value']",
		"generic-key":                  "cell[key]",
		"array-literal":                "cells[0]",
		"setter":                       "cell.value",
		"symbol":                       "cell[key]",
		"union-key":                    "cell[key]",
		"generic-array":                "cells[index]",
		"prefix":                       "cell.value",

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
			messageSlot := slot
			if strings.HasPrefix(name, "assign-") || name == "object-assign" {
				messageSlot += ".value"
			}
			if len(result.Diagnostics) == 1 && !strings.Contains(result.Diagnostics[0].Message.Description, messageSlot) {
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
	if len(paths) != 45 {
		t.Fatalf("got %d probes, want 45", len(paths))
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
		"for of fits":                  {`function put(cell: {value: string | number} | {value: string | number}): void { for (cell.value of ['text']) {} }`, `{value: string | number} |`, `{value: number} |`},
		"for of narrowed":              {`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { for (cell.value of ['text']) {} } }`, `cell.kind === 's'`, `true`},
		"for of overlapping":           {`function put(cell: {value: string} | {value: string | number}): void { for (cell.value of ['text']) {} }`, `{value: string} |`, `{value: number} |`},
		"for of typed number":          {`function put(cell: {value: number} | {value: number | string}, source: number): void { for (cell.value of [source]) {} }`, `source: number`, `source: string`},
		"for of void callback":         {`function put(cell: {value: () => void} | {value: () => number}): void { for (cell.value of [() => 1]) {} }`, `() => 1`, `() => 'text'`},
		"for of defined":               {`function put(cell: {value: number | undefined} | {value: number}): void { for (cell.value of [1]) {} }`, `[1]`, `[undefined]`},
		"for in fits":                  {`function put(cell: {value: string | number} | {value: string | number}): void { for (cell.value in {text: 1}) {} }`, `{value: string | number} |`, `{value: number} |`},
		"for in narrowed":              {`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { for (cell.value in {text: 1}) {} } }`, `cell.kind === 's'`, `true`},
		"for in overlapping":           {`function put(cell: {value: string} | {value: string | number}): void { for (cell.value in {text: 1}) {} }`, `{value: string} |`, `{value: number} |`},
		"array pattern fits":           {`function put(cell: {value: string | number} | {value: string | number}): void { [cell.value] = ['text']; }`, `{value: string | number} |`, `{value: number} |`},
		"array pattern narrowed":       {`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { [cell.value] = ['text']; } }`, `cell.kind === 's'`, `true`},
		"array pattern overlapping":    {`function put(cell: {value: string} | {value: string | number}): void { [cell.value] = ['text']; }`, `{value: string} |`, `{value: number} |`},
		"array pattern typed number":   {`function put(cell: {value: number} | {value: number | string}, source: number): void { [cell.value] = [source]; }`, `source: number`, `source: string`},
		"array pattern void callback":  {`function put(cell: {value: () => void} | {value: () => number}): void { [cell.value] = [() => 1]; }`, `() => 1`, `() => 'text'`},
		"array pattern defined":        {`function put(cell: {value: number | undefined} | {value: number}): void { [cell.value] = [1]; }`, `[1]`, `[undefined]`},
		"object pattern fits":          {`function put(cell: {value: string | number} | {value: string | number}): void { ({text: cell.value} = {text: 'text'}); }`, `{value: string | number} |`, `{value: number} |`},
		"object pattern narrowed":      {`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { ({text: cell.value} = {text: 'text'}); } }`, `cell.kind === 's'`, `true`},
		"object pattern overlapping":   {`function put(cell: {value: string} | {value: string | number}): void { ({text: cell.value} = {text: 'text'}); }`, `{value: string} |`, `{value: number} |`},
		"object pattern typed number":  {`function put(cell: {value: number} | {value: number | string}, source: number): void { ({text: cell.value} = {text: source}); }`, `source: number`, `source: string`},
		"object pattern void callback": {`function put(cell: {value: () => void} | {value: () => number}): void { ({text: cell.value} = {text: () => 1}); }`, `() => 1`, `() => 'text'`},
		"object pattern defined":       {`function put(cell: {value: number | undefined} | {value: number}): void { ({text: cell.value} = {text: 1}); }`, `text: 1`, `text: undefined`},
		"object assign fits":           {`function put(cell: {value: string | number} | {value: string | number}): void { Object.assign(cell, {value: 'text'}); }`, `{value: string | number} |`, `{value: number} |`},
		"object assign narrowed":       {`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { Object.assign(cell, {value: 'text'}); } }`, `cell.kind === 's'`, `true`},
		"object assign overlapping":    {`function put(cell: {value: string} | {value: string | number}): void { Object.assign(cell, {value: 'text'}); }`, `{value: string} |`, `{value: number} |`},
		"object assign typed number":   {`function put(cell: {value: number} | {value: number | string}, source: number): void { Object.assign(cell, {value: source}); }`, `source: number`, `source: string`},
		"object assign void callback":  {`function put(cell: {value: () => void} | {value: () => number}): void { Object.assign(cell, {value: () => 1}); }`, `() => 1`, `() => 'text'`},
		"object assign defined":        {`function put(cell: {value: number | undefined} | {value: number}): void { Object.assign(cell, {value: 1}); }`, `value: 1`, `value: undefined`},
		"assign plain":                 {`function put(cell: {value: string | number}): void { Object.assign(cell, {value: 'text'}); }`, `{value: string | number}`, `{value: number} | {value: string | number}`},
		"assign unrelated":             {`function put(cell: {value: string} | {value: number}): void { Object.assign(cell, {other: 'text'}); }`, `other:`, `value:`},
		"assign shadow":                {`const Object = {assign(target: unknown, source: unknown): void {}}; function put(cell: {value: string} | {value: number}): void { Object.assign(cell, {value: 'text'}); }`, `const Object = {assign(target: unknown, source: unknown): void {}};`, `export {};`},
		"delete optional":              {`function put(cell: {value?: string} | {value?: number}): void { delete cell.value; }`, `delete cell.value`, `cell.value = 'text'`},
		"array matching quiet":         {`function put(cell: {value: number} | {value: number | string}): void { const source: [string, number] = ['text', 1]; [,cell.value] = source; }`, `[,cell.value]`, `[cell.value]`},
		"object matching quiet":        {`function put(cell: {value: number} | {value: number | string}): void { const source = {a: 'text', b: 1}; ({b: cell.value} = source); }`, `({b:`, `({a:`},
		"array default":                {`function put(cell: {value: number} | {value: number | string}, source: number): void { [cell.value = 1] = [source]; }`, `source: number`, `source: string`},
		"object default":               {`function put(cell: {value: number} | {value: number | string}, source: number): void { ({text: cell.value = 1} = {text: source}); }`, `source: number`, `source: string`},
		"nested array default":         {`function put(cell: {value: number} | {value: number | string}, source: number): void { [[cell.value = 1]] = [[source]]; }`, `source: number`, `source: string`},
		"nested object default":        {`function put(cell: {value: number} | {value: number | string}, source: number): void { ({a: {text: cell.value = 1}} = {a: {text: source}}); }`, `source: number`, `source: string`},
		"array element":                {`function put(cell: {value: number} | {value: number | string}, source: number): void { [cell['value']] = [source]; }`, `source: number`, `source: string`},
		"array default absent":         {`function put(cell: {value: number} | {value: number | string}): void { [cell.value = 1] = []; }`, `= 1`, `= 'text'`},
		"object default absent":        {`function put(cell: {value: number} | {value: number | string}): void { ({text: cell.value = 1} = {}); }`, `= 1`, `= 'text'`},
		"assign optional source":       {`function put(cell: {value: number} | {value: number | string}, source: {value?: number}): void { Object.assign(cell, source); }`, `source: {value?: number}`, `source: {value?: string}`},
		"setter write type":            {`type Cell = { get value(): number; set value(v: string | number) } | { get value(): number; set value(v: string | number | boolean) }; function put(cell: Cell): void { cell.value = 'text'; }`, "set value(v: string | number)", "set value(v: number)"},
		"nested narrowing":             {`type Cell = { inner: { kind: 's'; value: string } } | { inner: { kind: 'n'; value: number } }; function put(cell: Cell): void { if (cell.inner.kind === 's') { cell.inner.value = 'text'; } }`, "cell.inner.kind === 's'", "true"},
		"generic narrowing":            {`type Cell = { kind: 's'; value: string } | { kind: 'n'; value: number }; function put<T extends Cell>(cell: T): void { if (cell.kind === 's') { cell.value = 'text'; } }`, "cell.kind === 's'", "true"},
		"read":                         {`function read(cell: { value: string } | { value: number }): boolean | string { return cell.value === 'text'; }`, "cell.value === 'text'", "cell.value = 'text'"},
		"narrowed":                     {`type Cell = { kind: 's'; value: string } | { kind: 'n'; value: number }; function put(cell: Cell): void { if (cell.kind === 's') { cell.value = 'text'; } }`, "cell.kind === 's'", "true"},
		"overlapping":                  {`function put(cell: { n: number } | { n: number | string }): void { cell.n = 1; }`, "cell.n = 1", "cell.n = 'text'"},
		"identical":                    {`function put(cell: { value: string | number } | { value: string | number }): void { cell.value = 'text'; }`, "{ value: string | number } |", "{ value: number } |"},
		"void callback":                {`function put(cell: { f: () => void } | { f: () => number }): void { cell.f = () => 1; }`, "() => 1", "() => 'text'"},
		"defined value":                {`function put(cell: { n: number | undefined } | { n: number }): void { cell.n = 1; }`, "cell.n = 1", "cell.n = undefined"},
		"plain":                        {`function put(cell: { n: number | string }): void { cell.n = 'text'; }`, "{ n: number | string }", "{ n: number } | { n: number | string }"},
		"logical fits":                 {`function put(cell: { n: number | undefined } | { n: 1 | undefined }): void { cell.n ??= 1; }`, "??= 1", "??= 2"},
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

func TestUnionWriteDeleteStaysQuiet(t *testing.T) {
	t.Parallel()
	bytes, err := os.ReadFile("testdata/union-write-legal/delete.ts")
	if err != nil {
		t.Fatal(err)
	}
	source := string(bytes)
	if count := typeErrorCount(t, source); count != 0 {
		t.Fatalf("tsgo diagnostics: %d, want 0", count)
	}
	rule_testing.ExpectClean(t, runAdamic(t, unionWriteSubject(), source))
	plant := strings.Replace(source, "delete cell.value", "cell.value = 'text'", 1)
	rule_testing.ExpectFindings(t, runAdamic(t, unionWriteSubject(), plant), "unionWrite")
	run := func(source string) string {
		file := filepath.Join(t.TempDir(), "probe.mts")
		if err := os.WriteFile(file, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command("node", file).CombinedOutput()
		if err != nil {
			t.Fatalf("Node: %v: %s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	if got := run(source); got != "0" {
		t.Fatalf("delete: got %q, want 0", got)
	}
	if got := run(strings.Replace(source, "clear(counter);", "", 1)); got != "1" {
		t.Fatalf("delete control: got %q, want 1", got)
	}
}
