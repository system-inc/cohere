package adamic

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

func TestUnionWriteRefused(test *testing.T) {
	test.Parallel()
	for name, slot := range map[string]string{
		"rest-array-union-source":      "holder.items",
		"rest-object-union-source":     "holder.bag",
		"rest-array":                   "holder.items",
		"rest-array-leading":           "holder.items",
		"rest-object":                  "holder.bag",
		"rest-object-leading":          "holder.bag",
		"rest-array-tuple":             "holder.items",
		"rest-array-generic":           "holder.items",
		"rest-object-generic":          "holder.bag",
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
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			bytes, readError := os.ReadFile("testdata/union-write/" + name + ".ts")
			if readError != nil {
				test.Fatal(readError)
			}
			source := string(bytes)
			if count := typeErrorCount(test, source); count != 0 {
				test.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			result := runAdamic(test, UnionWrite, source)
			rule_testing.ExpectFindings(test, result, "unionWrite")
			expectSpans(test, source, result, slot)
			messageSlot := slot
			if strings.HasPrefix(name, "assign-") || name == "object-assign" {
				messageSlot += ".value"
			}
			if len(result.Diagnostics) == 1 &&
				!strings.Contains(result.Diagnostics[0].Message.Description, messageSlot) {
				test.Errorf("message %q does not name slot %s", result.Diagnostics[0].Message.Description, slot)
			}
		})
	}
}

// nodeRecording is one recorded node run of a program, as testdata/union-write/README.md says it was made:
// the program's sha256, node's exit status, its stdout lines, and the TypeError line node printed, if any.
type nodeRecording struct {
	source string
	exit   int
	stdout []string
	stderr []string
}

// throws reports whether the run ended in an uncaught TypeError.
func (recording nodeRecording) throws() bool {
	return recording.exit != 0 && len(recording.stderr) == 1 && strings.HasPrefix(recording.stderr[0], "TypeError: ")
}

// readNodeRecording reads the recording at path and holds it to program, the exact text node ran, so a
// program edited without being recorded again fails here instead of passing on a stale run.
func readNodeRecording(test *testing.T, path, program string) nodeRecording {
	test.Helper()
	bytes, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	recording := nodeRecording{exit: -1}
	for line := range strings.Lines(string(bytes)) {
		key, value, _ := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		switch key {
		case "source":
			recording.source = value
		case "exit":
			exit, parseError := strconv.Atoi(value)
			if parseError != nil {
				test.Fatalf("%s: exit %q: %v", path, value, parseError)
			}
			recording.exit = exit
		case "stdout":
			recording.stdout = append(recording.stdout, value)
		case "stderr":
			recording.stderr = append(recording.stderr, value)
		default:
			test.Fatalf("%s: unknown line %q", path, line)
		}
	}
	if recording.exit < 0 {
		test.Fatalf("%s: no exit line", path)
	}
	sum := sha256.Sum256([]byte(program))
	if recording.source != hex.EncodeToString(sum[:]) {
		test.Fatalf("%s records another program than the one beside it; record it again (README.md)", path)
	}
	return recording
}

// Every refused form is a Node failure, read from node's recorded run of the probe (README.md beside the
// probes says how it was made). Removing probe() is the control: the exact same program no longer throws,
// so a recording that always throws is caught. The rule must report every probe the recording shows
// throwing, so the rule's verdict and the runtime's agree probe by probe.
func TestUnionWriteRecordedProbes(test *testing.T) {
	test.Parallel()
	paths, globError := filepath.Glob("testdata/union-write/*.ts")
	if globError != nil {
		test.Fatal(globError)
	}
	if len(paths) != 54 {
		test.Fatalf("got %d probes, want 54", len(paths))
	}
	recordings, globError := filepath.Glob("testdata/union-write/*.out")
	if globError != nil {
		test.Fatal(globError)
	}
	if len(recordings) != 2*len(paths) {
		test.Fatalf("got %d recordings, want a run and a control for each of %d probes", len(recordings), len(paths))
	}
	for _, path := range paths {
		test.Run(filepath.Base(path), func(test *testing.T) {
			test.Parallel()
			bytes, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			source := string(bytes)
			control := strings.Replace(source, "\nprobe();", "", 1)
			if control == source {
				test.Fatal("probe control did not change the source")
			}
			stem := strings.TrimSuffix(path, ".ts")
			probe := readNodeRecording(test, stem+".out", source)
			if !probe.throws() {
				test.Fatalf("want Node TypeError, recorded exit %d: %q", probe.exit, probe.stderr)
			}
			if recorded := readNodeRecording(test, stem+".control.out", control); recorded.exit != 0 ||
				len(recorded.stderr) != 0 {
				test.Fatalf("control: recorded exit %d: %q", recorded.exit, recorded.stderr)
			}
			rule_testing.ExpectFindings(test, runAdamic(test, UnionWrite, source), "unionWrite")
		})
	}
}

func TestUnionWriteLegalNeighbours(test *testing.T) {
	test.Parallel()
	for name, fixture := range map[string]struct{ source, old, plant string }{
		"rest tuple-valued slot": {
			`function put(holder: {items: [number]} | {items: [1]}): void { const source: [1] = [1]; [...holder.items] = source; }`,
			`const source: [1] = [1]`,
			`const source: [2] = [2]`,
		},
		"rest array fits": {
			`function put(holder: {items: string[]} | {items: (string | number)[]}): void { [...holder.items] = ['text']; }`,
			`{items: string[]} |`,
			`{items: number[]} |`,
		},
		"rest array narrowed": {
			`type Holder = {kind: 's'; items: string[]} | {kind: 'n'; items: number[]}; function put(holder: Holder): void { if (holder.kind === 's') { [...holder.items] = ['text']; } }`,
			`holder.kind === 's'`,
			`true`,
		},
		"rest array plain": {
			`function put(holder: {items: string[]}): void { [...holder.items] = ['text']; }`,
			`holder: {items: string[]}`,
			`holder: {items: string[]} | {items: number[]}`,
		},
		"rest array leading fits": {
			`function put(holder: {items: string[]} | {items: (string | number)[]}): void { let first = ''; [first, ...holder.items] = ['a', 'text']; }`,
			`{items: string[]} |`,
			`{items: number[]} |`,
		},
		"rest array leading narrowed": {
			`type Holder = {kind: 's'; items: string[]} | {kind: 'n'; items: number[]}; function put(holder: Holder): void { if (holder.kind === 's') { let first = ''; [first, ...holder.items] = ['a', 'text']; } }`,
			`holder.kind === 's'`,
			`true`,
		},
		"rest array leading plain": {
			`function put(holder: {items: string[]}): void { let first = ''; [first, ...holder.items] = ['a', 'text']; }`,
			`holder: {items: string[]}`,
			`holder: {items: string[]} | {items: number[]}`,
		},
		"rest object fits": {
			`function put(holder: {bag: {text: string}} | {bag: {text: string | number}}): void { ({ ...holder.bag } = {text: 'ahra'}); }`,
			`{bag: {text: string}} |`,
			`{bag: {count: number}} |`,
		},
		"rest object narrowed": {
			`type Holder = {kind: 's'; bag: {text: string}} | {kind: 'n'; bag: {count: number}}; function put(holder: Holder): void { if (holder.kind === 's') { ({ ...holder.bag } = {text: 'ahra'}); } }`,
			`holder.kind === 's'`,
			`true`,
		},
		"rest object plain": {
			`function put(holder: {bag: {text: string}}): void { ({ ...holder.bag } = {text: 'ahra'}); }`,
			`holder: {bag: {text: string}}`,
			`holder: {bag: {text: string}} | {bag: {count: number}}`,
		},
		"rest tuple drops prefix": {
			`function put(holder: {items: number[]} | {items: (number | string)[]}): void { const source: [string, number, number] = ['text', 1, 2]; let first = ''; [first, ...holder.items] = source; }`,
			`{items: number[]} |`,
			`{items: string[]} |`,
		},
		"rest tuple empty tail": {
			`function put(holder: {items: number[]} | {items: string[]}): void { const source: [string] = ['text']; let first = ''; [first, ...holder.items] = source; }`,
			`[first, ...holder.items]`,
			`[...holder.items]`,
		},
		"rest union tuple": {
			`function put(holder: {items: number[]} | {items: (number | string)[]}, source: [string, number] | [boolean, number]): void { let first: unknown; [first, ...holder.items] = source; }`,
			`{items: number[]} |`,
			`{items: boolean[]} |`,
		},
		"rest object excludes prefix": {
			`function put(holder: {bag: {count: number}} | {bag: {count: number; text?: never}}): void { let first = ''; ({text: first, ...holder.bag} = {text: 'ahra', count: 1}); }`,
			`text: first, `,
			``,
		},
		"rest object union source": {
			`function put(holder: {bag: {text: string}} | {bag: {text: string | number}}, source: {omit: number; text: string} | {omit: boolean; text: string}): void { let first: unknown; ({omit: first, ...holder.bag} = source); }`,
			`{bag: {text: string}} |`,
			`{bag: {count: number}} |`,
		},
		"for of fits": {
			`function put(cell: {value: string | number} | {value: string | number}): void { for (cell.value of ['text']) {} }`,
			`{value: string | number} |`,
			`{value: number} |`,
		},
		"for of narrowed": {
			`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { for (cell.value of ['text']) {} } }`,
			`cell.kind === 's'`,
			`true`,
		},
		"for of overlapping": {
			`function put(cell: {value: string} | {value: string | number}): void { for (cell.value of ['text']) {} }`,
			`{value: string} |`,
			`{value: number} |`,
		},
		"for of typed number": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { for (cell.value of [source]) {} }`,
			`source: number`,
			`source: string`,
		},
		"for of void callback": {
			`function put(cell: {value: () => void} | {value: () => number}): void { for (cell.value of [() => 1]) {} }`,
			`() => 1`,
			`() => 'text'`,
		},
		"for of defined": {
			`function put(cell: {value: number | undefined} | {value: number}): void { for (cell.value of [1]) {} }`,
			`[1]`,
			`[undefined]`,
		},
		"for in fits": {
			`function put(cell: {value: string | number} | {value: string | number}): void { for (cell.value in {text: 1}) {} }`,
			`{value: string | number} |`,
			`{value: number} |`,
		},
		"for in narrowed": {
			`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { for (cell.value in {text: 1}) {} } }`,
			`cell.kind === 's'`,
			`true`,
		},
		"for in overlapping": {
			`function put(cell: {value: string} | {value: string | number}): void { for (cell.value in {text: 1}) {} }`,
			`{value: string} |`,
			`{value: number} |`,
		},
		"array pattern fits": {
			`function put(cell: {value: string | number} | {value: string | number}): void { [cell.value] = ['text']; }`,
			`{value: string | number} |`,
			`{value: number} |`,
		},
		"array pattern narrowed": {
			`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { [cell.value] = ['text']; } }`,
			`cell.kind === 's'`,
			`true`,
		},
		"array pattern overlapping": {
			`function put(cell: {value: string} | {value: string | number}): void { [cell.value] = ['text']; }`,
			`{value: string} |`,
			`{value: number} |`,
		},
		"array pattern typed number": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { [cell.value] = [source]; }`,
			`source: number`,
			`source: string`,
		},
		"array pattern void callback": {
			`function put(cell: {value: () => void} | {value: () => number}): void { [cell.value] = [() => 1]; }`,
			`() => 1`,
			`() => 'text'`,
		},
		"array pattern defined": {
			`function put(cell: {value: number | undefined} | {value: number}): void { [cell.value] = [1]; }`,
			`[1]`,
			`[undefined]`,
		},
		"object pattern fits": {
			`function put(cell: {value: string | number} | {value: string | number}): void { ({text: cell.value} = {text: 'text'}); }`,
			`{value: string | number} |`,
			`{value: number} |`,
		},
		"object pattern narrowed": {
			`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { ({text: cell.value} = {text: 'text'}); } }`,
			`cell.kind === 's'`,
			`true`,
		},
		"object pattern overlapping": {
			`function put(cell: {value: string} | {value: string | number}): void { ({text: cell.value} = {text: 'text'}); }`,
			`{value: string} |`,
			`{value: number} |`,
		},
		"object pattern typed number": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { ({text: cell.value} = {text: source}); }`,
			`source: number`,
			`source: string`,
		},
		"object pattern void callback": {
			`function put(cell: {value: () => void} | {value: () => number}): void { ({text: cell.value} = {text: () => 1}); }`,
			`() => 1`,
			`() => 'text'`,
		},
		"object pattern defined": {
			`function put(cell: {value: number | undefined} | {value: number}): void { ({text: cell.value} = {text: 1}); }`,
			`text: 1`,
			`text: undefined`,
		},
		"object assign fits": {
			`function put(cell: {value: string | number} | {value: string | number}): void { Object.assign(cell, {value: 'text'}); }`,
			`{value: string | number} |`,
			`{value: number} |`,
		},
		"object assign narrowed": {
			`type Cell = {kind: 's'; value: string} | {kind: 'n'; value: number}; function put(cell: Cell): void { if (cell.kind === 's') { Object.assign(cell, {value: 'text'}); } }`,
			`cell.kind === 's'`,
			`true`,
		},
		"object assign overlapping": {
			`function put(cell: {value: string} | {value: string | number}): void { Object.assign(cell, {value: 'text'}); }`,
			`{value: string} |`,
			`{value: number} |`,
		},
		"object assign typed number": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { Object.assign(cell, {value: source}); }`,
			`source: number`,
			`source: string`,
		},
		"object assign void callback": {
			`function put(cell: {value: () => void} | {value: () => number}): void { Object.assign(cell, {value: () => 1}); }`,
			`() => 1`,
			`() => 'text'`,
		},
		"object assign defined": {
			`function put(cell: {value: number | undefined} | {value: number}): void { Object.assign(cell, {value: 1}); }`,
			`value: 1`,
			`value: undefined`,
		},
		"assign plain": {
			`function put(cell: {value: string | number}): void { Object.assign(cell, {value: 'text'}); }`,
			`{value: string | number}`,
			`{value: number} | {value: string | number}`,
		},
		"assign unrelated": {
			`function put(cell: {value: string} | {value: number}): void { Object.assign(cell, {other: 'text'}); }`,
			`other:`,
			`value:`,
		},
		"assign shadow": {
			`const Object = {assign(target: unknown, source: unknown): void {}}; function put(cell: {value: string} | {value: number}): void { Object.assign(cell, {value: 'text'}); }`,
			`const Object = {assign(target: unknown, source: unknown): void {}};`,
			`export {};`,
		},
		"delete optional": {
			`function put(cell: {value?: string} | {value?: number}): void { delete cell.value; }`,
			`delete cell.value`,
			`cell.value = 'text'`,
		},
		"array matching quiet": {
			`function put(cell: {value: number} | {value: number | string}): void { const source: [string, number] = ['text', 1]; [,cell.value] = source; }`,
			`[,cell.value]`,
			`[cell.value]`,
		},
		"object matching quiet": {
			`function put(cell: {value: number} | {value: number | string}): void { const source = {a: 'text', b: 1}; ({b: cell.value} = source); }`,
			`({b:`,
			`({a:`,
		},
		"array default": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { [cell.value = 1] = [source]; }`,
			`source: number`,
			`source: string`,
		},
		"object default": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { ({text: cell.value = 1} = {text: source}); }`,
			`source: number`,
			`source: string`,
		},
		"nested array default": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { [[cell.value = 1]] = [[source]]; }`,
			`source: number`,
			`source: string`,
		},
		"nested object default": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { ({a: {text: cell.value = 1}} = {a: {text: source}}); }`,
			`source: number`,
			`source: string`,
		},
		"array element": {
			`function put(cell: {value: number} | {value: number | string}, source: number): void { [cell['value']] = [source]; }`,
			`source: number`,
			`source: string`,
		},
		"array default absent": {
			`function put(cell: {value: number} | {value: number | string}): void { [cell.value = 1] = []; }`,
			`= 1`,
			`= 'text'`,
		},
		"object default absent": {
			`function put(cell: {value: number} | {value: number | string}): void { ({text: cell.value = 1} = {}); }`,
			`= 1`,
			`= 'text'`,
		},
		"assign optional source": {
			`function put(cell: {value: number} | {value: number | string}, source: {value?: number}): void { Object.assign(cell, source); }`,
			`source: {value?: number}`,
			`source: {value?: string}`,
		},
		"setter write type": {
			`type Cell = { get value(): number; set value(v: string | number) } | { get value(): number; set value(v: string | number | boolean) }; function put(cell: Cell): void { cell.value = 'text'; }`,
			"set value(v: string | number)",
			"set value(v: number)",
		},
		"nested narrowing": {
			`type Cell = { inner: { kind: 's'; value: string } } | { inner: { kind: 'n'; value: number } }; function put(cell: Cell): void { if (cell.inner.kind === 's') { cell.inner.value = 'text'; } }`,
			"cell.inner.kind === 's'",
			"true",
		},
		"generic narrowing": {
			`type Cell = { kind: 's'; value: string } | { kind: 'n'; value: number }; function put<T extends Cell>(cell: T): void { if (cell.kind === 's') { cell.value = 'text'; } }`,
			"cell.kind === 's'",
			"true",
		},
		"read": {
			`function read(cell: { value: string } | { value: number }): boolean | string { return cell.value === 'text'; }`,
			"cell.value === 'text'",
			"cell.value = 'text'",
		},
		"narrowed": {
			`type Cell = { kind: 's'; value: string } | { kind: 'n'; value: number }; function put(cell: Cell): void { if (cell.kind === 's') { cell.value = 'text'; } }`,
			"cell.kind === 's'",
			"true",
		},
		"overlapping": {
			`function put(cell: { n: number } | { n: number | string }): void { cell.n = 1; }`,
			"cell.n = 1",
			"cell.n = 'text'",
		},
		"identical": {
			`function put(cell: { value: string | number } | { value: string | number }): void { cell.value = 'text'; }`,
			"{ value: string | number } |",
			"{ value: number } |",
		},
		"void callback": {
			`function put(cell: { f: () => void } | { f: () => number }): void { cell.f = () => 1; }`,
			"() => 1",
			"() => 'text'",
		},
		"defined value": {
			`function put(cell: { n: number | undefined } | { n: number }): void { cell.n = 1; }`,
			"cell.n = 1",
			"cell.n = undefined",
		},
		"plain": {
			`function put(cell: { n: number | string }): void { cell.n = 'text'; }`,
			"{ n: number | string }",
			"{ n: number } | { n: number | string }",
		},
		"logical fits": {
			`function put(cell: { n: number | undefined } | { n: 1 | undefined }): void { cell.n ??= 1; }`,
			"??= 1",
			"??= 2",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if count := typeErrorCount(test, fixture.source); count != 0 {
				test.Fatalf("tsgo diagnostics: %d, want 0", count)
			}
			rule_testing.ExpectClean(test, runAdamic(test, UnionWrite, fixture.source))
			plant := strings.Replace(fixture.source, fixture.old, fixture.plant, 1)
			if plant == fixture.source {
				test.Fatal("plant did not change source")
			}
			rule_testing.ExpectFindings(test, runAdamic(test, UnionWrite, plant), "unionWrite")
		})
	}
}

// The delete stays quiet at run time too, read from node's recorded runs (testdata/union-write-legal/README.md):
// the program prints 0, and its control, without clear(counter), prints 1, so the delete really ran.
func TestUnionWriteDeleteStaysQuiet(test *testing.T) {
	test.Parallel()
	bytes, readError := os.ReadFile("testdata/union-write-legal/delete.ts")
	if readError != nil {
		test.Fatal(readError)
	}
	source := string(bytes)
	if count := typeErrorCount(test, source); count != 0 {
		test.Fatalf("tsgo diagnostics: %d, want 0", count)
	}
	rule_testing.ExpectClean(test, runAdamic(test, UnionWrite, source))
	plant := strings.Replace(source, "delete cell.value", "cell.value = 'text'", 1)
	rule_testing.ExpectFindings(test, runAdamic(test, UnionWrite, plant), "unionWrite")
	recorded := func(path, program, want string) {
		recording := readNodeRecording(test, path, program)
		if recording.exit != 0 || len(recording.stderr) != 0 || strings.Join(recording.stdout, "\n") != want {
			test.Fatalf("%s: recorded exit %d, stdout %q, stderr %q; want exit 0 printing %s",
				path, recording.exit, recording.stdout, recording.stderr, want)
		}
	}
	recorded("testdata/union-write-legal/delete.out", source, "0")
	control := strings.Replace(source, "clear(counter);", "", 1)
	if control == source {
		test.Fatal("delete control did not change the source")
	}
	recorded("testdata/union-write-legal/delete.control.out", control, "1")
}
