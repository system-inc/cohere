package nexus

import (
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const correctnessNoCallerDataMutationFile = "/repository/source/CorrectnessNoCallerDataMutation.ts"

// correctnessNoCallerDataMutationHostFile stands in for a host's types: a declaration file, which is
// where the DOM's, a package's and `@types/*` declarations live. The fixture program's lib is ES2022,
// so the DOM itself is not there to write to.
const correctnessNoCallerDataMutationHostFile = "/repository/source/Host.d.ts"

var correctnessNoCallerDataMutationHost = strings.Join([]string{
	"interface CanvasContextInterface { fillStyle: string; lineWidth: number }",
	"interface PortInterface { onmessage: ((data: unknown) => void) | null }",
	"interface HostElementInterface { style: { color: string }; dataset: Record<string, string>; classNames: string[] }",
	"",
}, "\n")

var correctnessNoCallerDataMutationPrelude = strings.Join([]string{
	"interface UsageInterface { inputTokens: number; outputTokens: number; dollars: number }",
	"interface DiffInterface { path: string }",
	"interface MessageInterface { createdAt: number; body: string }",
	"interface EntryInterface { status: string; position: number; child?: EntryInterface }",
	"interface HubInterface { watchersByPath: Map<string, number>; seen: Set<string>; debounceTimer: number | null }",
	"interface RequestInterface { headers: Record<string, string> }",
	"interface PanelInterface { canvas: CanvasContextInterface; label: string }",
	"declare function use(value: unknown): void;",
	"",
}, "\n")

func correctnessNoCallerDataMutationSource(lines ...string) string {
	return correctnessNoCallerDataMutationPrelude + strings.Join(lines, "\n") + "\n"
}

func correctnessNoCallerDataMutationRun(t *testing.T, sourceText string) rule_testing.Result {
	t.Helper()
	return rule_testing.RunTypedFiles(t, CorrectnessNoCallerDataMutation, map[string]string{
		correctnessNoCallerDataMutationFile:     sourceText,
		correctnessNoCallerDataMutationHostFile: correctnessNoCallerDataMutationHost,
	}, correctnessNoCallerDataMutationFile)
}

// correctnessNoCallerDataMutationExpect asserts the reported text in source order, and that no
// finding carries a fix.
func correctnessNoCallerDataMutationExpect(t *testing.T, result rule_testing.Result, want ...string) {
	t.Helper()
	// The shared assertion by message id, which proves the rule can fire; the checks below hold each
	// finding's exact text.
	if len(want) > 0 {
		wantIds := make([]string, len(want))
		for index := range wantIds {
			wantIds[index] = correctnessNoCallerDataMutationId
		}
		rule_testing.ExpectFindings(t, result, wantIds...)
	}
	diagnostics := append(result.Diagnostics[:0:0], result.Diagnostics...)
	sort.SliceStable(diagnostics, func(left int, right int) bool {
		return diagnostics[left].Range.Pos() < diagnostics[right].Range.Pos()
	})
	text := result.SourceFile.Text()
	var got []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Message.Id != correctnessNoCallerDataMutationId {
			t.Fatalf("unexpected message id %q", diagnostic.Message.Id)
		}
		if len(diagnostic.Fixes) != 0 || len(diagnostic.Suggestions) != 0 {
			t.Fatalf("expected no fix and no suggestion, got %d and %d", len(diagnostic.Fixes), len(diagnostic.Suggestions))
		}
		got = append(got, text[diagnostic.Range.Pos():diagnostic.Range.End()])
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d findings %q, got %d %q", len(want), want, len(got), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("finding %d is %q, want %q", index, got[index], want[index])
		}
	}
}

// correctnessNoCallerDataMutationAddUsage models `addUsage` in ahra's `modules/os/AhraOsKingdom.ts` on
// 2026-10-03, one of the writes Kirk's ruling names. `fixed` returns the sum instead of writing it
// into the caller's total.
func correctnessNoCallerDataMutationAddUsage(fixed bool) string {
	if fixed {
		return correctnessNoCallerDataMutationSource(
			"declare function priceUsage(usage: UsageInterface): number;",
			"export function addUsage(total: UsageInterface, from: UsageInterface): UsageInterface {",
			"    const sum = { inputTokens: total.inputTokens + from.inputTokens, outputTokens: total.outputTokens + from.outputTokens, dollars: 0 };",
			"    return { ...sum, dollars: priceUsage(sum) };",
			"}",
		)
	}
	return correctnessNoCallerDataMutationSource(
		"declare function priceUsage(usage: UsageInterface): number;",
		"export function addUsage(into: UsageInterface, from: UsageInterface): void {",
		"    into.inputTokens += from.inputTokens;",
		"    into.outputTokens += from.outputTokens;",
		"    into.dollars = priceUsage(into);",
		"}",
	)
}

func TestCorrectnessNoCallerDataMutationFiresOnAddUsage(t *testing.T) {
	t.Parallel()

	result := correctnessNoCallerDataMutationRun(t, correctnessNoCallerDataMutationAddUsage(false))
	correctnessNoCallerDataMutationExpect(t, result, "into.inputTokens", "into.outputTokens", "into.dollars")
}

func TestCorrectnessNoCallerDataMutationStaysSilentOnFixedAddUsage(t *testing.T) {
	t.Parallel()

	rule_testing.ExpectClean(t, correctnessNoCallerDataMutationRun(t, correctnessNoCallerDataMutationAddUsage(true)))
}

func TestCorrectnessNoCallerDataMutationFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  []string
	}{
		{"an accumulator array pushed", []string{
			"export function checkFile(diffs: DiffInterface[], path: string): void {",
			"    diffs.push({ path });",
			"}",
		}, []string{"diffs.push"}},
		{"a caller's list sorted and returned", []string{
			"export function mergeOldestFirst(messages: MessageInterface[]): MessageInterface[] {",
			"    return messages.sort(function(left, right) { return left.createdAt - right.createdAt; });",
			"}",
		}, []string{"messages.sort"}},
		{"a record's key written through a property", []string{
			"export function sign(request: RequestInterface, host: string): void {",
			"    request.headers['host'] = host;",
			"}",
		}, []string{"request.headers['host']"}},
		{"a computed key into a record parameter", []string{
			"export function flatten(object: Record<string, unknown>, result: Record<string, unknown> = {}): Record<string, unknown> {",
			"    for(const [key, value] of Object.entries(object)) result[key] = value;",
			"    return result;",
			"}",
		}, []string{"result[key]"}},
		{"a key of a Partial Record, from FinanceQuickBooksEnricher's pushSample", []string{
			"type OutcomeType = 'Agrees' | 'Skip';",
			"export function pushSample(samples: Partial<Record<OutcomeType, DiffInterface[]>>, outcome: OutcomeType, diff: DiffInterface): void {",
			"    const bucket = samples[outcome] ?? [];",
			"    bucket.push(diff);",
			"    samples[outcome] = bucket;",
			"}",
		}, []string{"samples[outcome]"}},
		{"an element of an array parameter", []string{
			"export function parseInto(out: (string | null)[], offset: number): void {",
			"    out[offset] = null;",
			"}",
		}, []string{"out[offset]"}},
		{"updates and a delete", []string{
			"export function step(entry: EntryInterface, cache: Record<string, number>): void {",
			"    entry.position++;",
			"    --entry.position;",
			"    delete cache[entry.status];",
			"}",
		}, []string{"entry.position", "entry.position", "cache[entry.status]"}},
		{"a destructured parameter", []string{
			"export function settle({ entry }: { entry: EntryInterface }): void {",
			"    entry.status = 'done';",
			"}",
		}, []string{"entry.status"}},
		{"collections reached through a state hub", []string{
			"export function teardown(hub: HubInterface, path: string): void {",
			"    hub.watchersByPath.delete(path);",
			"    hub.seen.add(path);",
			"    hub.watchersByPath.clear();",
			"    hub.debounceTimer = null;",
			"}",
		}, []string{"hub.watchersByPath.delete", "hub.seen.add", "hub.watchersByPath.clear", "hub.debounceTimer"}},
		{"a Set and a Map passed down a recursion", []string{
			"export function collect(name: string, seen: Set<string>, depths: Map<string, number>): void {",
			"    seen.add(name);",
			"    depths.set(name, seen.size);",
			"}",
		}, []string{"seen.add", "depths.set"}},
		{"through a non-null assertion, in a class method", []string{
			"export class Walker {",
			"    visit(entry: EntryInterface): void {",
			"        entry.child!.status = 'seen';",
			"    }",
			"}",
		}, []string{"entry.child!.status"}},
		{"a project class's field", []string{
			"export class ConnectionClass { heartbeat: number | null = null; }",
			"export function arm(connection: ConnectionClass): void {",
			"    connection.heartbeat = 1;",
			"}",
		}, []string{"connection.heartbeat"}},
		{"a project property reached past a host object's", []string{
			"export function rename(panel: PanelInterface): void {",
			"    panel.canvas.fillStyle = 'red';",
			"    panel.label = 'Panel';",
			"}",
		}, []string{"panel.label"}},
		{"a local helper's parameter", []string{
			"export function count(rows: string[]): Record<string, number> {",
			"    const totals: Record<string, number> = {};",
			"    const note = function(bucket: Record<string, number>, key: string) { bucket[key] = (bucket[key] ?? 0) + 1; };",
			"    for(const row of rows) note(totals, row);",
			"    return totals;",
			"}",
		}, []string{"bucket[key]"}},
		{"a mutating call whose result is used", []string{
			"export function append(diffs: DiffInterface[]): number {",
			"    return diffs.push({ path: '' });",
			"}",
		}, []string{"diffs.push"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := correctnessNoCallerDataMutationRun(t, correctnessNoCallerDataMutationSource(testCase.lines...))
			correctnessNoCallerDataMutationExpect(t, result, testCase.want...)
		})
	}
}

func TestCorrectnessNoCallerDataMutationStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
	}{
		{"a host object's property, declared in a declaration file", []string{
			"export function paint(context: CanvasContextInterface, port: PortInterface): void {",
			"    context.fillStyle = 'red';",
			"    context.lineWidth += 1;",
			"    port.onmessage = null;",
			"}",
		}},
		{"a host object reached from project data", []string{
			"export function paint(panel: PanelInterface, element: HostElementInterface): void {",
			"    panel.canvas.fillStyle = 'red';",
			"    element.style.color = 'blue';",
			"    element.dataset['state'] = 'open';",
			"    element.classNames.push('open');",
			"}",
		}},
		{"a default-library object's property", []string{
			"export function rewind(pattern: RegExp, error: Error): void {",
			"    pattern.lastIndex = 0;",
			"    error.message = 'wrapped: ' + error.message;",
			"}",
		}},
		{"a default-library typed array", []string{
			"export function write(buffer: Uint8Array, offset: number): void {",
			"    buffer[offset] = 0;",
			"    buffer.fill(0);",
			"}",
		}},
		{"a forEach callback's element", []string{
			"export function settleAll(entries: EntryInterface[]): void {",
			"    entries.forEach(function(entry) { entry.status = 'done'; });",
			"    entries.forEach((entry) => { entry.position = 0; });",
			"}",
		}},
		{"a reduce accumulator", []string{
			"export function group(diffs: DiffInterface[]): Record<string, DiffInterface[]> {",
			"    return diffs.reduce(function(groups: Record<string, DiffInterface[]>, diff) {",
			"        (groups[diff.path] ??= []).push(diff);",
			"        groups[diff.path] = groups[diff.path] ?? [];",
			"        return groups;",
			"    }, {});",
			"}",
		}},
		{"a callback handed to new", []string{
			"declare class RunnerClass { constructor(step: (entry: EntryInterface) => void) }",
			"export function run(): RunnerClass {",
			"    return new RunnerClass(function(entry) { entry.status = 'done'; });",
			"}",
		}},
		{"a function invoked on the spot", []string{
			"export function settle(): EntryInterface {",
			"    const holder = { status: '', position: 0 };",
			"    (function(entry: EntryInterface) { entry.status = 'done'; })(holder);",
			"    return holder;",
			"}",
		}},
		{"a parameter reassigned to a copy first", []string{
			"export function settle(entry: EntryInterface): EntryInterface {",
			"    entry = { ...entry };",
			"    entry.status = 'done';",
			"    return entry;",
			"}",
		}},
		{"a parameter reassigned in a default", []string{
			"export function settle(entry: EntryInterface, unused = (entry = { ...entry })): EntryInterface {",
			"    use(unused);",
			"    entry.status = 'done';",
			"    return entry;",
			"}",
		}},
		{"a project class's setter", []string{
			"export class GaugeClass {",
			"    #value = 0;",
			"    set value(next: number) { this.#value = Math.max(0, next); }",
			"    get value(): number { return this.#value; }",
			"}",
			"export function reset(gauge: GaugeClass): void {",
			"    gauge.value = 0;",
			"}",
		}},
		{"a local value, not a parameter", []string{
			"export function build(): DiffInterface[] {",
			"    const diffs: DiffInterface[] = [];",
			"    diffs.push({ path: '' });",
			"    const entry = { status: '', position: 0 };",
			"    entry.status = 'done';",
			"    return diffs;",
			"}",
		}},
		{"a copy sorted, not the caller's list", []string{
			"export function oldestFirst(messages: MessageInterface[]): MessageInterface[] {",
			"    return messages.slice().sort(function(left, right) { return left.createdAt - right.createdAt; });",
			"}",
			"export function oldestFirstSpread(messages: MessageInterface[]): MessageInterface[] {",
			"    return [...messages].sort(function(left, right) { return left.createdAt - right.createdAt; });",
			"}",
		}},
		{"reads of a parameter", []string{
			"export function describe(entry: EntryInterface, diffs: DiffInterface[]): string {",
			"    use(diffs.length);",
			"    use(diffs.includes(diffs[0]!));",
			"    return entry.status;",
			"}",
		}},
		{"a write through this", []string{
			"export class Counter {",
			"    count = 0;",
			"    add(amount: number): void { this.count += amount; }",
			"}",
		}},
		{"a write through a cast", []string{
			"export function force(entry: EntryInterface): void {",
			"    (entry as { status: string }).status = 'done';",
			"}",
		}},
		{"a parameter typed any", []string{
			"export function anything(value: any): void {",
			"    value.status = 'done';",
			"    value.items.push(1);",
			"}",
		}},
		{"a project type whose push is its own", []string{
			"interface RecorderInterface { push(value: string): void; set(key: string): void }",
			"export function record(recorder: RecorderInterface): void {",
			"    recorder.push('a');",
			"    recorder.set('b');",
			"}",
		}},
		{"a parameter shadowed by a local", []string{
			"export function settle(entry: EntryInterface): void {",
			"    use(entry);",
			"    {",
			"        const entry = { status: '', position: 0 };",
			"        entry.status = 'done';",
			"    }",
			"}",
		}},
		{"an array's length, which the default library declares", []string{
			"export function truncate(diffs: DiffInterface[]): void {",
			"    diffs.length = 0;",
			"}",
		}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rule_testing.ExpectClean(t, correctnessNoCallerDataMutationRun(t, correctnessNoCallerDataMutationSource(testCase.lines...)))
		})
	}
}
