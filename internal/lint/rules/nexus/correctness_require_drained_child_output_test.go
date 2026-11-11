package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const correctnessRequireDrainedChildOutputFile = "/repository/source/CorrectnessRequireDrainedChildOutput.ts"

const correctnessRequireDrainedChildOutputNodeFile = "/repository/source/node.d.ts"

func correctnessRequireDrainedChildOutputSource(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// correctnessRequireDrainedChildOutputNodeTypes is the slice of `@types/node` the rule resolves
// against, in the layout of 26.6.5 (ahra's): `once` is declared in a namespace inside `declare module
// "node:events"`, the child's streams are Readables from `node:stream`, `spawn` takes its options as
// a parameter named `options` with or without `args` before it, and `"child_process"` and `"events"`
// re-export the `node:` modules.
var correctnessRequireDrainedChildOutputNodeTypes = correctnessRequireDrainedChildOutputSource(
	`declare module "node:events" {`,
	"    class EventEmitter {",
	"        on(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        once(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        addListener(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        prependListener(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        prependOnceListener(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        off(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        removeListener(eventName: string | symbol, listener: (...args: any[]) => void): this;",
	"        removeAllListeners(eventName?: string | symbol): this;",
	"        setMaxListeners(count: number): this;",
	"    }",
	"    namespace EventEmitter {",
	"        function once(emitter: EventEmitter, eventName: string | symbol): Promise<any[]>;",
	"    }",
	"    export = EventEmitter;",
	"}",
	`declare module "events" {`,
	`    import events = require("node:events");`,
	"    export = events;",
	"}",
	`declare module "node:stream" {`,
	`    import { EventEmitter } from "node:events";`,
	"    class Readable extends EventEmitter {",
	"        setEncoding(encoding: string): this;",
	"        pause(): this;",
	"        resume(): this;",
	"        read(): unknown;",
	"        pipe<T>(destination: T): T;",
	"        [Symbol.asyncIterator](): AsyncIterableIterator<unknown>;",
	"    }",
	"    class Writable extends EventEmitter { write(chunk: unknown): boolean }",
	"}",
	`declare module "node:readline" {`,
	`    import { Readable } from "node:stream";`,
	"    interface Interface { close(): void }",
	"    function createInterface(options: { input: Readable }): Interface;",
	"}",
	`declare module "node:child_process" {`,
	`    import { EventEmitter } from "node:events";`,
	`    import { Readable, Writable } from "node:stream";`,
	"    class ChildProcess extends EventEmitter {",
	"        stdin: Writable;",
	"        stdout: Readable;",
	"        stderr: Readable;",
	"        stdio: unknown[];",
	"        pid?: number;",
	"        kill(signal?: string): boolean;",
	"        unref(): void;",
	"    }",
	"    type StdioOption = 'pipe' | 'overlapped' | 'ignore' | 'inherit' | number | null | undefined | Writable;",
	"    interface SpawnOptions { stdio?: 'pipe' | 'ignore' | 'inherit' | StdioOption[]; cwd?: string }",
	"    function spawn(command: string, options?: SpawnOptions): ChildProcess;",
	"    function spawn(command: string, args: readonly string[], options?: SpawnOptions): ChildProcess;",
	"    function fork(modulePath: string, args?: readonly string[], options?: SpawnOptions): ChildProcess;",
	"    function exec(command: string, callback?: (error: Error | null, stdout: string) => void): ChildProcess;",
	"    function execFile(",
	"        file: string,",
	"        args: readonly string[],",
	"        callback: (error: Error | null, stdout: string) => void,",
	"    ): ChildProcess;",
	"    function spawnSync(command: string, args?: readonly string[]): { stdout: string };",
	"}",
	`declare module "child_process" {`,
	`    export * from "node:child_process";`,
	"}",
)

// correctnessRequireDrainedChildOutputPrelude imports the modules the ways ahra does and declares the
// names the cases use, so the checker resolves each one.
var correctnessRequireDrainedChildOutputPrelude = correctnessRequireDrainedChildOutputSource(
	"import * as NodeChildProcess from 'node:child_process';",
	"import { spawn } from 'node:child_process';",
	"import { once } from 'node:events';",
	"import * as NodeReadline from 'node:readline';",
	"declare const verbose: boolean;",
	"declare const chunks: unknown[];",
	"declare const logStream: NodeJS.WritableStream;",
	"declare function read(chunk: unknown): void;",
	"declare function done(): void;",
	"declare function wire(child: NodeChildProcess.ChildProcess): void;",
	"declare namespace NodeJS { interface WritableStream { write(chunk: unknown): boolean } }",
	"",
)

func correctnessRequireDrainedChildOutputRun(test *testing.T, sourceText string) rule_testing.Result {
	test.Helper()
	return rule_testing.RunTypedFiles(test, CorrectnessRequireDrainedChildOutput, map[string]string{
		correctnessRequireDrainedChildOutputFile:     sourceText,
		correctnessRequireDrainedChildOutputNodeFile: correctnessRequireDrainedChildOutputNodeTypes,
	}, correctnessRequireDrainedChildOutputFile)
}

// correctnessRequireDrainedChildOutputFinding is one expected finding: the wait it points at, and
// the stream and event its message names.
type correctnessRequireDrainedChildOutputFinding struct {
	span   string
	stream string
	event  string
}

// correctnessRequireDrainedChildOutputExpect asserts one finding per wanted entry, in source order and
// stdout before stderr at one wait, each on its wait with the message naming its stream. The harness
// trims the subject file before writing it, so spans are read from the program's own text.
func correctnessRequireDrainedChildOutputExpect(
	test *testing.T,
	result rule_testing.Result,
	want []correctnessRequireDrainedChildOutputFinding,
) {
	test.Helper()
	if len(want) == 0 {
		rule_testing.ExpectClean(test, result)
		return
	}
	wantIds := make([]string, 0, len(want))
	for range want {
		wantIds = append(wantIds, correctnessRequireDrainedChildOutputId)
	}
	rule_testing.ExpectFindings(test, result, wantIds...)
	text := result.SourceFile.Text()
	for index, diagnostic := range result.Diagnostics {
		reported := text[diagnostic.Range.Pos():diagnostic.Range.End()]
		if reported != want[index].span {
			test.Fatalf("finding %d points at\n%s\nwant\n%s", index, reported, want[index].span)
		}
		wantMessage := correctnessRequireDrainedChildOutputMessage(want[index].stream, want[index].event)
		if diagnostic.Message.Description != wantMessage.Description {
			test.Fatalf("finding %d message is %q, want %q", index, diagnostic.Message.Description,
				wantMessage.Description)
		}
		if len(diagnostic.Fixes) != 0 || len(diagnostic.Suggestions) != 0 {
			test.Fatalf(
				"expected no fix and no suggestion, got %d and %d",
				len(diagnostic.Fixes),
				len(diagnostic.Suggestions),
			)
		}
	}
}

func TestCorrectnessRequireDrainedChildOutputFires(test *testing.T) {
	test.Parallel()

	type finding = correctnessRequireDrainedChildOutputFinding
	cases := []struct {
		name  string
		lines []string
		want  []finding
	}{
		// The Servers.app shape: the child is waited on and its output never read.
		{"waited on without reading", []string{
			"const child = spawn('ps', ['-eo', 'pid']);",
			"await once(child, 'close');",
		}, []finding{
			{"once(child, 'close')", "stdout", "close"},
			{"once(child, 'close')", "stderr", "close"},
		}},
		{"a drain attached only inside an if", []string{
			"const child = spawn('ps', ['-eo', 'pid'], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"if(verbose) {",
			"    child.stdout.on('data', read);",
			"}",
			"await once(child, 'close');",
		}, []finding{{"once(child, 'close')", "stdout", "close"}}},
		{"stdout drained and stderr a pipe nothing reads", []string{
			"const child = spawn('ps', ['-eo', 'pid']);",
			"child.stdout.on('data', function(chunk) { chunks.push(chunk); });",
			"await once(child, 'close');",
		}, []finding{{"once(child, 'close')", "stderr", "close"}}},
		{"a drain attached after the awaited close", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'inherit'] });",
			"await once(child, 'close');",
			"child.stdout.on('data', read);",
		}, []finding{{"once(child, 'close')", "stdout", "close"}}},
		{"setEncoding alone", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'inherit'] });",
			"child.stdout.setEncoding('utf8').on('end', done);",
			"await once(child, 'exit');",
		}, []finding{{"once(child, 'exit')", "stdout", "exit"}}},
		{"an awaited promise around a close listener", []string{
			"const child = spawn('ps');",
			"await new Promise(function(resolve) { child.on('close', resolve); });",
		}, []finding{
			{"child.on('close', resolve)", "stdout", "close"},
			{"child.on('close', resolve)", "stderr", "close"},
		}},
		// The shape of the promise wrappers in ahra: stdout collected, stderr left as a pipe.
		{"spawned inside a promise that resolves on close", []string{
			"export function run(): Promise<string> {",
			"    return new Promise(function(resolve) {",
			"        const child = spawn('ps', ['-eo', 'pid']);",
			"        let output = '';",
			"        child.stdout.on('data', function(chunk) { output += String(chunk); });",
			"        child.on('close', function() { resolve(output); });",
			"    });",
			"}",
		}, []finding{{"child.on('close', function() { resolve(output); })", "stderr", "close"}}},
		{"only an exit listener, through a namespace import", []string{
			"const child = NodeChildProcess.spawn('ps', [], { stdio: 'pipe' });",
			"child.once('exit', done);",
		}, []finding{
			{"child.once('exit', done)", "stdout", "exit"},
			{"child.once('exit', done)", "stderr", "exit"},
		}},
		{"a renamed import from the bare module name", []string{
			"import { spawn as start } from 'child_process';",
			"import { once as onceFrom } from 'events';",
			"const child = start('ps', { stdio: ['ignore', undefined] });",
			"await onceFrom(child, 'close');",
		}, []finding{
			{"onceFrom(child, 'close')", "stdout", "close"},
			{"onceFrom(child, 'close')", "stderr", "close"},
		}},
		{"spawn destructured from the namespace", []string{
			"const { spawn: launch } = NodeChildProcess;",
			"const child = launch('ps', [], { stdio: ['ignore', 'ignore', 'pipe'] });",
			"child.stdout.on('data', read);",
			"await once(child, 'close');",
		}, []finding{{"once(child, 'close')", "stderr", "close"}}},
		{"drained on one branch of a ternary", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"const reader = verbose ? child.stdout.on('data', read) : null;",
			"await once(child, 'close');",
			"console.log(reader);",
		}, []finding{{"once(child, 'close')", "stdout", "close"}}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			sourceText := correctnessRequireDrainedChildOutputPrelude +
				correctnessRequireDrainedChildOutputSource(testCase.lines...)
			result := correctnessRequireDrainedChildOutputRun(test, sourceText)
			correctnessRequireDrainedChildOutputExpect(test, result, testCase.want)
		})
	}
}

func TestCorrectnessRequireDrainedChildOutputStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name  string
		lines []string
	}{
		{"stdout read before the close and stderr inherited", []string{
			"const child = spawn('ps', ['-eo', 'pid'], { stdio: ['ignore', 'pipe', 'inherit'] });",
			"child.stdout.on('data', chunk => chunks.push(chunk));",
			"await once(child, 'close');",
		}},
		{"both streams read", []string{
			"const child = spawn('ps');",
			"child.stdout.on('data', read);",
			"child.stderr.pipe(logStream);",
			"await once(child, 'close');",
		}},
		{"stdio ignore, inherit and an array of neither", []string{
			"const ignored = spawn('ps', [], { stdio: 'ignore' });",
			"await once(ignored, 'close');",
			"const inherited = spawn('ps', [], { stdio: 'inherit' });",
			"await once(inherited, 'close');",
			"const mixed = spawn('ps', [], { stdio: ['ignore', 'inherit', 'inherit'] });",
			"await once(mixed, 'close');",
		}},
		{"stdio file descriptors and a stream", []string{
			"const numbered = spawn('ps', [], { stdio: [0, 1, 2] });",
			"await once(numbered, 'close');",
			"const streamed = spawn('ps', [], { stdio: ['ignore', logStream as never, 'ignore'] });",
			"await once(streamed, 'close');",
		}},
		{"options the rule cannot read", []string{
			"declare const options: NodeChildProcess.SpawnOptions;",
			"declare const stdio: 'pipe' | 'ignore';",
			"const held = spawn('ps', [], options);",
			"await once(held, 'close');",
			"const spread = spawn('ps', [], { ...options });",
			"await once(spread, 'close');",
			"const chosen = spawn('ps', [], { stdio });",
			"await once(chosen, 'close');",
		}},
		{"execFile with a callback and spawnSync, which buffer", []string{
			"NodeChildProcess.execFile('ps', [], (error, stdout) => {});",
			"NodeChildProcess.spawnSync('ps');",
			"const child = NodeChildProcess.execFile('ps', [], done);",
			"await once(child, 'close');",
		}},
		{"pipe, resume, read and readline", []string{
			"const piped = spawn('ps');",
			"piped.stdout.pipe(logStream);",
			"piped.stderr.resume();",
			"await once(piped, 'close');",
			"const lines = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"const reader = NodeReadline.createInterface({ input: lines.stdout });",
			"await once(lines, 'close');",
			"reader.close();",
		}},
		{"an async iteration started before the wait", []string{
			"export async function list(): Promise<void> {",
			"    const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"    for await (const chunk of child.stdout) {",
			"        read(chunk);",
			"    }",
			"    await once(child, 'close');",
			"}",
		}},
		{"drained on both branches", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"if(verbose) {",
			"    child.stdout.on('data', read);",
			"}",
			"else {",
			"    child.stdout.resume();",
			"}",
			"await once(child, 'close');",
		}},
		{"a drain after the close listener in the same run", []string{
			"export function run(): Promise<void> {",
			"    return new Promise(function(resolve) {",
			"        const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"        child.on('close', resolve);",
			"        child.stdout.on('data', read);",
			"    });",
			"}",
		}},
		{"a drain and a wait inside one awaited executor", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"await new Promise(function(resolve) {",
			"    child.stdout.on('data', read);",
			"    child.on('close', resolve);",
			"});",
		}},
		{"a drain inside a callback that may run any time", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"child.on('spawn', function() { child.stdout.on('data', read); });",
			"await once(child, 'close');",
		}},
		{"never waited on", []string{
			"const child = spawn('ps');",
			"child.unref();",
			"const other = spawn('ps');",
			"await once(other, 'spawn');",
		}},
		{"handed to a function or returned", []string{
			"const wired = spawn('ps');",
			"wire(wired);",
			"await once(wired, 'close');",
			"export function start(): NodeChildProcess.ChildProcess {",
			"    const child = spawn('ps');",
			"    child.on('close', done);",
			"    return child;",
			"}",
		}},
		{"stdio read as an array", []string{
			"const child = spawn('ps');",
			"const output = child.stdio[1];",
			"await once(child, 'close');",
			"console.log(output);",
		}},
		{"a neutral read of a stream, then a drain", []string{
			"const child = spawn('ps', [], { stdio: ['ignore', 'pipe', 'ignore'] });",
			"if(!child.stdout) throw new Error('no stdout');",
			"child.stdout.on('end', done).on('data', read);",
			"await once(child, 'close');",
		}},
		{"a local function named spawn", []string{
			"function spawnLocal(command: string): NodeChildProcess.ChildProcess { return wireAndReturn(command); }",
			"declare function wireAndReturn(command: string): NodeChildProcess.ChildProcess;",
			"const child = spawnLocal('ps');",
			"await once(child, 'close');",
		}},
		{"fork, which inherits its output", []string{
			"const child = NodeChildProcess.fork('worker.js');",
			"await once(child, 'exit');",
		}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			sourceText := correctnessRequireDrainedChildOutputPrelude +
				correctnessRequireDrainedChildOutputSource(testCase.lines...)
			result := correctnessRequireDrainedChildOutputRun(test, sourceText)
			correctnessRequireDrainedChildOutputExpect(test, result, nil)
		})
	}
}

// With no checker the rule registers nothing: a name match alone would report every `spawn`.
func TestCorrectnessRequireDrainedChildOutputDeclinesWithoutAChecker(test *testing.T) {
	test.Parallel()

	sourceText := correctnessRequireDrainedChildOutputPrelude +
		"const child = spawn('ps');\nawait once(child, 'close');\n"
	result := rule_testing.Run(
		test,
		CorrectnessRequireDrainedChildOutput,
		correctnessRequireDrainedChildOutputFile,
		sourceText,
	)
	rule_testing.ExpectClean(test, result)
}
