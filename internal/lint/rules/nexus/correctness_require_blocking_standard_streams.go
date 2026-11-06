package nexus

import (
	"crypto/sha256"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

const correctnessRequireBlockingStandardStreamsId = "exitBeforeBlockingStandardStreams"

// The rule's message and the options it picks from, whose wording lives in
// `policy/messages/correctness-require-blocking-standard-streams.json`.
var (
	correctnessRequireBlockingStandardStreamsText = policy.MessageOf(
		"nexus/correctness-require-blocking-standard-streams",
		correctnessRequireBlockingStandardStreamsId,
	)
	correctnessRequireBlockingStandardStreamsShebang = correctnessRequireBlockingStandardStreamsText.Option(
		"processReason",
		"shebang",
	)
	correctnessRequireBlockingStandardStreamsUnimported = correctnessRequireBlockingStandardStreamsText.Option(
		"processReason",
		"unimported",
	)
	correctnessRequireBlockingStandardStreamsOneExit = correctnessRequireBlockingStandardStreamsText.Option(
		"exitCount",
		"one",
	)
	correctnessRequireBlockingStandardStreamsSeveralExits = correctnessRequireBlockingStandardStreamsText.Option(
		"exitCount",
		"several",
	)
)

// CorrectnessRequireBlockingStandardStreams reports a file that runs as a process and can reach
// `process.exit()` after a write to stdout or stderr without having called Nexus's
// `blockStandardStreams()` first.
//
//	invalid: #!/usr/bin/env tsx
//	         async function main() { if(!name) { console.error('usage'); process.exit(1); } }
//	         main().catch(function(error) { console.error(error); process.exit(1); });
//	invalid: // a forked worker no file imports
//	         if(!specFilePath) { console.error('usage'); process.exit(2); }
//	valid:   async function main() { blockStandardStreams(); ... } main().catch(...);
//	valid:   blockStandardStreams(); if(!specFilePath) { console.error('usage'); process.exit(2); }
//	valid:   runCommandLineInterface(new AhraCommandLineInterface());
//	valid:   // a library file other files import, whatever its functions do
//
// # Where it came from
//
// Task `#gjs78wz`, ruled by `@system_cohere`. Node writes to a pipe asynchronously on POSIX, and
// `process.exit()` does not wait for the queue: measured on Node 24 on macOS, 1 MiB written and then
// `process.exit(0)`, piped into a slow reader, delivered 65,536 bytes, exit status 0, no error.
// `nexus/correctness-no-process-exit-after-output` found the hazard per exit, 1,012 times on ahra, and
// each fix is a `process.exitCode` and a `return` placed by hand. Nexus's `source/system/StandardStreams.ts`
// fixes every exit of a process at once by making stdout and stderr blocking, and
// `runCommandLineInterface` calls it before any command runs, so the per-exit rule is registered off and
// the check moves here: to the entry, where the one fix belongs. The real sites were the fifteen
// entries in the task's `ExitEntries.tsv` that block nothing, among them `ahra facets`
// (`modules/facets/FacetsCommandLineInterface.ts`, its own `main().catch(...)` with seven exits), the
// forked `modules/data/DataConversionShardWorker.ts`, Structure's spawned `StructureDoctor.ts`,
// `StructureAnalyzer.ts` and `StructureLintEngineParity.ts`, and the `main().catch(...)` scripts under
// `modules/phi/social/scripts/`.
//
// # What makes a file an entry
//
// What the file proves about itself, and one fact about the program:
//
//   - **A `#!` line.** The author declares the file executable. It is an entry even when other files
//     import it too, because run directly it is the process's first module.
//   - **Nothing in the program imports it.** Then its top-level code only ever runs as the first
//     module of a process: a script run by path, a forked or spawned child, or a file a tool loads by
//     convention. An import is any `import` or `export ... from` not written `type`, an
//     `import x = require()`, and a dynamic `import()` or `require()` of a string literal.
//
// A file without a `#!` that something imports is declined whole: its top-level code may run inside
// another entry's process after that entry blocked, and the AST of this file cannot say which. Names
// play no part: `main` is not special, and neither is `CommandLineInterface` in a file name.
//
// # What the process runs
//
// The code that runs because the file was loaded, followed by symbol inside this file: the module's
// top level, every function of this file it calls (by an identifier that resolves to a function
// declaration or a `const` bound to a function), every immediately invoked function, and every
// function handed to a call as an argument (`main().catch(function(error) { ... })`,
// `process.on('SIGINT', onInterrupt)`), transitively. An exported function no load-time code reaches
// is not counted: it runs in whichever process calls it. A call into another file is not followed;
// an exit there is a missed finding here.
//
// # The exit
//
// The condition `nexus/correctness-no-process-exit-after-output` reports, reused from it: a call of
// `NodeJS.Process.exit` reached, on some path through its function's control-flow graph, after a
// write to stdout or stderr in the same function. A write in one function and an exit in another
// (`showHelp(); process.exit(0);`) is a missed finding, as it is there.
//
// # Blocking first
//
// A file whose code, and the code of every file it imports, transitively, never reaches Nexus's
// `source/system/StandardStreams.ts` cannot block (the program index below decides that once). Every
// exit the process runs is then reported as soon as one qualifies.
//
// A file that can reach it is walked in order, and anything that may block ends the walk's path:
//
//   - **A call that may block**: one resolving by symbol to Nexus's `blockStandardStreams` (by its
//     declaration's name and file, `source/system/StandardStreams.ts`), or to any function whose body
//     may call one, transitively and by symbol (`runCommandLineInterface` is one, and so is a `main`
//     whose first statement blocks). A callee the checker cannot resolve, a `declare`d function or a
//     value of unknown origin called in a file that can reach the streams, a dynamic `import()`, and
//     `require()` all count as blocking, and so does handing a function that may block to any call.
//     A call into a file that cannot reach the streams never blocks, whatever it is.
//   - **An `await`**, a `for await` and an `await using`, in a file that blocks anywhere: the code
//     after it runs on a later turn, after anything else the file starts.
//   - **A callback or a generator**, in such a file, is taken to start blocked: it may run after any
//     later statement.
//   - **The files it imports.** Each runs its top-level code before this file's body. If one that can
//     reach the streams makes a call at load that may block, the whole file is taken as blocked.
//
// Each of these can only cost a finding. So a file that blocks first is silent, and a file that
// blocks too late is reported when an exit is reached before the block on some path: a usage guard
// that prints and exits above `runCommandLineInterface(...)`, or a `main` that writes and exits before
// its own `blockStandardStreams()`.
//
// Two things are not seen, and either could make a finding false. `blockStandardStreams` stored in a
// variable, a property or a registry and called through it: proving no such store exists would mean
// reading every file's references, and on ahra both Nexus functions are only ever called directly.
// And code the program does not hold that runs first in the process, a `node --import` preload that
// blocks: ahra's entries run under `tsx`, whose loader does not.
//
// # The finding
//
// One per entry, at its first qualifying exit in source order, naming how many there are. The fix is
// one call at the entry, not one per exit.
//
// # Cost
//
// The program index (which files are imported, which can reach the streams) is built once per run,
// under a package lock, as `correctness-no-import-cycle-load-time-read` builds its graph: one
// resolution per import specifier and no checker. The rule reads other files, so it declares
// `ReadsOtherFiles`, and the findings cache replays it on a file while its bytes, its type fingerprint
// and the program fingerprint hold (#5yfbdse). A file that never spells `exit` is declined before the
// index is asked for.
//
// # No fix
//
// Where the call belongs is the author's choice: the first statement of the module, or of the `main`
// it runs, and above a guard that must not run unblocked. A fixer would pick one blindly.
var CorrectnessRequireBlockingStandardStreams = rule.Rule[rule.NoOptions]{
	Name: "nexus/correctness-require-blocking-standard-streams",

	// Symbol identity is what tells `NodeJS.Process.exit` and the global console from look-alikes,
	// Nexus's `blockStandardStreams` from any other function of that name, and a call of this file's
	// own function from a call of an import.
	NeedsTypeChecker: true,

	// The index reads every file's imports and their resolutions, this file's among them. The run's
	// directory, which the fingerprint names files relative to.
	ProgramReads: rule.ReadsCompilerOptions | rule.ReadsModuleResolution | rule.ReadsOtherFiles,

	// See correctnessRequireBlockingStandardStreamsFingerprint for what a file's verdict reads.
	ProgramFingerprint: correctnessRequireBlockingStandardStreamsFingerprint,

	// Only a file that calls `process.exit` can hold a finding, and most files do not.
	NoListener: rule.NoListenerDeclinesIrrelevantFiles,

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil || ruleContext.Program == nil || ruleContext.SourceFile == nil {
			return nil
		}
		// Every exit is a member named `exit`, so a file that never spells it holds none.
		if !strings.Contains(ruleContext.SourceFile.Text(), "exit") {
			return nil
		}
		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				correctnessRequireBlockingStandardStreamsScanFile(ruleContext)
			},
		}
	},
}

// correctnessRequireBlockingStandardStreamsDeclarationFileSuffixes are where Nexus declares
// `blockStandardStreams`: its source, and the declaration file a compiled copy would ship.
var correctnessRequireBlockingStandardStreamsDeclarationFileSuffixes = []string{
	"/source/system/StandardStreams.ts",
	"/source/system/StandardStreams.d.ts",
}

// correctnessRequireBlockingStandardStreamsIndex is what the rule knows about the whole program.
type correctnessRequireBlockingStandardStreamsIndex struct {
	// imported holds every file some program file imports.
	imported map[tspath.PathKey]bool
	// canBlock holds every file whose code, or the code of a file it imports, transitively, may call
	// `blockStandardStreams`: Nexus's declaring file, every file that reaches it, and every file that
	// loads a module by a computed `import()` or `require()`, which may be anything.
	canBlock map[tspath.PathKey]bool
	// edges are each file's imports, of every kind, to files in the program.
	edges map[tspath.PathKey][]tspath.PathKey
	// files are the program's files by path.
	files map[tspath.PathKey]*ast.SourceFile
}

// correctnessRequireBlockingStandardStreamsCache holds one index per program, for the reason
// `correctness_no_import_cycle_load_time_read.go` gives: the program does not exist at registration,
// and the index is a fact about the whole run.
var correctnessRequireBlockingStandardStreamsCache struct {
	sync.Mutex
	program rule.ProgramIdentity
	index   *correctnessRequireBlockingStandardStreamsIndex
}

func correctnessRequireBlockingStandardStreamsIndexFor(
	program rule.Program,
) *correctnessRequireBlockingStandardStreamsIndex {
	correctnessRequireBlockingStandardStreamsCache.Lock()
	defer correctnessRequireBlockingStandardStreamsCache.Unlock()
	if correctnessRequireBlockingStandardStreamsCache.program == program.Identity() &&
		correctnessRequireBlockingStandardStreamsCache.index != nil {
		return correctnessRequireBlockingStandardStreamsCache.index
	}
	index := correctnessRequireBlockingStandardStreamsBuildIndex(program)
	correctnessRequireBlockingStandardStreamsCache.program = program.Identity()
	correctnessRequireBlockingStandardStreamsCache.index = index
	return index
}

// correctnessRequireBlockingStandardStreamsFingerprint is the rule's program fingerprint (#5yfbdse): the
// files something in the program imports, by name, in a stable order.
//
// A file's verdict reads, beyond the file, whether any other file imports it, and, for the files it
// imports, transitively, their edges, whether each can reach the streams, and what their top-level code
// calls. Only the first lies outside the file's import closure. Every edge the index holds is a
// resolution from the program's own table, the table the type fingerprint's edges come from, so the
// files a file's imports reach are its closure, whose bytes and resolutions its type fingerprint holds;
// and whether one of them can reach the streams is decided by its own imports, inside that closure. So
// the imported set is the whole of it: an edit that makes a file imported or unimported moves this, and
// an edit that does neither leaves it, and the findings replay. The rule takes no options, so none reach
// it.
func correctnessRequireBlockingStandardStreamsFingerprint(
	program rule.Program,
	_ rule.Given[rule.NoOptions],
) [sha256.Size]byte {
	index := correctnessRequireBlockingStandardStreamsIndexFor(program)
	var imported []string
	for path := range index.imported {
		imported = append(imported, rule.FingerprintPath(program, index.files[path].FileName().AsString()))
	}
	slices.Sort(imported)
	hash := sha256.New()
	for _, name := range imported {
		hash.Write([]byte(name))
		hash.Write([]byte{0})
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], hash.Sum(nil))
	return fingerprint
}

// correctnessRequireBlockingStandardStreamsBuildIndex resolves every import of every source file once.
// Declaration files contribute no edges of their own (no package's types import Nexus), but may be
// the target of one, so a Nexus consumed as compiled code still seeds the closure.
func correctnessRequireBlockingStandardStreamsBuildIndex(
	program rule.Program,
) *correctnessRequireBlockingStandardStreamsIndex {
	index := &correctnessRequireBlockingStandardStreamsIndex{
		imported: map[tspath.PathKey]bool{},
		canBlock: map[tspath.PathKey]bool{},
		edges:    map[tspath.PathKey][]tspath.PathKey{},
		files:    map[tspath.PathKey]*ast.SourceFile{},
	}
	var seeds []tspath.PathKey
	for _, sourceFile := range program.SourceFiles() {
		if sourceFile == nil {
			continue
		}
		index.files[sourceFile.PathKey()] = sourceFile
		for _, suffix := range correctnessRequireBlockingStandardStreamsDeclarationFileSuffixes {
			if strings.HasSuffix(sourceFile.FileName().AsString(), suffix) {
				seeds = append(seeds, sourceFile.PathKey())
			}
		}
	}

	for _, importer := range program.SourceFiles() {
		if importer == nil || importer.IsDeclarationFile {
			continue
		}
		add := func(specifier *ast.Node) {
			if specifier == nil || !ast.IsStringLiteralLike(specifier) {
				return
			}
			resolved := program.ResolveModule(importer, specifier)
			if !resolved.IsResolved() {
				return
			}
			target := program.GetSourceFileForResolvedModule(resolved.ResolvedFileName)
			if target == nil || target == importer || index.files[target.PathKey()] == nil {
				return
			}
			index.edges[importer.PathKey()] = append(index.edges[importer.PathKey()], target.PathKey())
			index.imported[target.PathKey()] = true
		}
		for _, statement := range importer.Statements.Nodes {
			switch statement.Kind {
			case ast.KindImportDeclaration:
				declaration := statement.AsImportDeclaration()
				if declaration.ImportClause == nil || !declaration.ImportClause.IsTypeOnly() {
					add(declaration.ModuleSpecifier)
				}
			case ast.KindExportDeclaration:
				declaration := statement.AsExportDeclaration()
				if !declaration.IsTypeOnly {
					add(declaration.ModuleSpecifier)
				}
			case ast.KindImportEqualsDeclaration:
				declaration := statement.AsImportEqualsDeclaration()
				if !declaration.IsTypeOnly &&
					declaration.ModuleReference != nil &&
					ast.IsExternalModuleReference(declaration.ModuleReference) {
					add(declaration.ModuleReference.AsExternalModuleReference().Expression)
				}
			}
		}
		// A module loaded by a call: `import(...)` or `require(...)`. A computed specifier may load
		// anything, so the file is taken to reach the streams.
		text := importer.Text()
		if !strings.Contains(text, "import(") && !strings.Contains(text, "require(") {
			continue
		}
		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			if node.Kind == ast.KindCallExpression && (ast.IsImportCall(node) || ast.IsRequireCall(node, false)) {
				arguments := node.AsCallExpression().Arguments
				if arguments != nil && len(arguments.Nodes) > 0 && ast.IsStringLiteralLike(arguments.Nodes[0]) {
					add(arguments.Nodes[0])
				} else {
					seeds = append(seeds, importer.PathKey())
				}
			}
			node.ForEachChild(visit)
			return false
		}
		importer.AsNode().ForEachChild(visit)
	}

	importers := map[tspath.PathKey][]tspath.PathKey{}
	for from, targets := range index.edges {
		for _, target := range targets {
			importers[target] = append(importers[target], from)
		}
	}
	queue := append([]tspath.PathKey(nil), seeds...)
	for _, seed := range seeds {
		index.canBlock[seed] = true
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, importer := range importers[current] {
			if !index.canBlock[importer] {
				index.canBlock[importer] = true
				queue = append(queue, importer)
			}
		}
	}
	return index
}

// correctnessRequireBlockingStandardStreamsEventKind is what one recorded event is.
type correctnessRequireBlockingStandardStreamsEventKind uint8

const (
	correctnessRequireBlockingStandardStreamsWrite correctnessRequireBlockingStandardStreamsEventKind = iota
	correctnessRequireBlockingStandardStreamsExit
	correctnessRequireBlockingStandardStreamsCatchEntered
	correctnessRequireBlockingStandardStreamsCall
	correctnessRequireBlockingStandardStreamsBlock
)

// correctnessRequireBlockingStandardStreamsEvent is one thing the graph recorded. A call carries the
// functions of this file it starts: the ones it runs before it returns, and the ones it hands on.
type correctnessRequireBlockingStandardStreamsEvent struct {
	kind              correctnessRequireBlockingStandardStreamsEventKind
	node              *ast.Node
	runs              []*ast.Node
	handsOn           []*ast.Node
	argumentsMayBlock bool
}

func correctnessRequireBlockingStandardStreamsScanFile(ruleContext rule.Context) {
	sourceFile := ruleContext.SourceFile
	if !correctnessRequireBlockingStandardStreamsHasCandidate(ruleContext, sourceFile.AsNode()) {
		return
	}
	index := correctnessRequireBlockingStandardStreamsIndexFor(ruleContext.Program)
	reason := correctnessRequireBlockingStandardStreamsShebang
	if !strings.HasPrefix(sourceFile.Text(), "#!") {
		if index.imported[sourceFile.PathKey()] {
			return
		}
		reason = correctnessRequireBlockingStandardStreamsUnimported
	}
	analysis := &correctnessRequireBlockingStandardStreamsAnalysis{
		ctx:              ruleContext,
		index:            index,
		ordered:          index.canBlock[sourceFile.PathKey()],
		functionMayBlock: map[*ast.Node]uint8{},
	}
	if analysis.ordered && analysis.importsBlockAtLoad(sourceFile.PathKey()) {
		return
	}

	exits := analysis.reachedExits(sourceFile.AsNode())
	if len(exits) == 0 {
		return
	}
	sort.Slice(exits, func(left int, right int) bool { return exits[left].Pos() < exits[right].Pos() })
	count := correctnessRequireBlockingStandardStreamsOneExit
	values := map[string]string(nil)
	if len(exits) > 1 {
		count = correctnessRequireBlockingStandardStreamsSeveralExits
		values = map[string]string{"count": strconv.Itoa(len(exits))}
	}
	ruleContext.ReportNode(exits[0], rule.Message{
		Id:          correctnessRequireBlockingStandardStreamsId,
		Description: correctnessRequireBlockingStandardStreamsText.Render(values, reason, count),
	})
}

// correctnessRequireBlockingStandardStreamsHasCandidate says whether some function of the file, or
// its top level, holds both an exit and a write: the cheap precondition of any finding.
func correctnessRequireBlockingStandardStreamsHasCandidate(ruleContext rule.Context, sourceFile *ast.Node) bool {
	exits := map[*ast.Node]bool{}
	writes := map[*ast.Node]bool{}
	found := false
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if found {
			return true
		}
		if node.Kind == ast.KindCallExpression {
			if correctnessNoProcessExitAfterOutputIsExit(ruleContext, node) {
				root := control_flow_graph.RootOf(node)
				exits[root] = true
				found = writes[root]
			} else if correctnessNoProcessExitAfterOutputIsWrite(ruleContext, node) {
				root := control_flow_graph.RootOf(node)
				writes[root] = true
				found = exits[root]
			}
		}
		node.ForEachChild(visit)
		return found
	}
	sourceFile.ForEachChild(visit)
	return found
}

// correctnessRequireBlockingStandardStreamsIsFunctionWithBody says whether a node is a function whose
// body runs only when it is called.
func correctnessRequireBlockingStandardStreamsIsFunctionWithBody(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration,
		ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
		return node.Body() != nil
	}
	return false
}

// correctnessRequireBlockingStandardStreamsIsCallLike says whether a node runs code it names: a call,
// a `new`, or a tagged template.
func correctnessRequireBlockingStandardStreamsIsCallLike(node *ast.Node) bool {
	return node.Kind == ast.KindCallExpression ||
		node.Kind == ast.KindNewExpression ||
		node.Kind == ast.KindTaggedTemplateExpression
}

// correctnessRequireBlockingStandardStreamsIsNexusDeclaration says whether a declaration is Nexus's
// own `blockStandardStreams`: a function of that name in Nexus's `source/system/StandardStreams`.
func correctnessRequireBlockingStandardStreamsIsNexusDeclaration(declaration *ast.Node) bool {
	if declaration.Kind != ast.KindFunctionDeclaration ||
		declaration.Name() == nil ||
		declaration.Name().Text() != "blockStandardStreams" {
		return false
	}
	file := ast.GetSourceFileOfNode(declaration)
	for _, suffix := range correctnessRequireBlockingStandardStreamsDeclarationFileSuffixes {
		if file != nil && strings.HasSuffix(file.FileName().AsString(), suffix) {
			return true
		}
	}
	return false
}

// correctnessRequireBlockingStandardStreamsIsInRunOf says whether node runs when root runs, rather
// than when a function inside root is later called. A class's initializers and static blocks run
// with the root that declares the class, so they count.
func correctnessRequireBlockingStandardStreamsIsInRunOf(node *ast.Node, root *ast.Node) bool {
	for current := node.Parent; current != nil && current != root; current = current.Parent {
		if correctnessRequireBlockingStandardStreamsIsFunctionWithBody(current) {
			return false
		}
	}
	return true
}
