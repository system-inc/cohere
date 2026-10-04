package nexus

import (
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

const correctnessNoImportCycleLoadTimeReadId = "loadTimeReadInImportCycle"

// correctnessNoImportCycleLoadTimeReadText is the rule's message, whose wording lives in
// `policy/messages/correctness-no-import-cycle-load-time-read.json`.
var correctnessNoImportCycleLoadTimeReadText = policy.MessageOf("nexus/correctness-no-import-cycle-load-time-read", correctnessNoImportCycleLoadTimeReadId)

// CorrectnessNoImportCycleLoadTimeRead reports a binding read while its module loads, when the binding
// is declared in a module in the same runtime import cycle as the reader.
//
//	invalid: // A.ts imports B.ts, B.ts imports A.ts
//	         import { RateLimiterModuleErrors } from './B';
//	         export const BaseErrorIdentifiers = { ...RateLimiterModuleErrors };
//	invalid: import { Base } from './B'; export class Derived extends Base {}
//	invalid: import * as b from './B'; export const limit = b.Limit * 2;
//	valid:   import { RateLimiterModuleErrors } from './B'; export function all() { return { ...RateLimiterModuleErrors }; }
//	valid:   import { helper } from './B'; export const value = helper(); // `function helper` is hoisted
//	valid:   import type { Shape } from './B'; // no runtime edge, so no cycle
//
// # Where it came from
//
// `libraries/structure/libraries/nexus/source/protocols/base/errors/BaseErrorIdentifiers.ts:62` in
// ahra spreads `...RateLimiterModuleErrors` into `BaseErrorIdentifiers` at module top level.
// `RateLimiterModuleErrors.ts` imports `BaseErrorIdentifierKeys` back from `BaseErrorIdentifiers.ts`,
// and `isBaseErrorData` from `BaseError.ts`, which imports `BaseErrorIdentifiers.ts` too, so the three
// files are one cycle. Whichever of them is loaded first decides whether the spread works. Loaded
// through `BaseErrorIdentifiers.ts`, the rate limiter file runs first and the spread finds it ready.
// Loaded through `RateLimiterModuleErrors.ts` (or `BaseError.ts`), the identifiers file runs while
// the rate limiter file is still waiting on it, and the spread throws `Cannot access
// 'RateLimiterModuleErrors' before initialization`. Nothing in either file changes for that to
// happen; one new import elsewhere in the program is enough. Found by the JS catalogue pass of the
// new-rules sweep (`#tevhg3f`, after Biome's `noImportCycles`, refined), built as `#c8armec`.
//
// # Why a cycle alone is not the finding
//
// A cycle is a fact, and on its own a harmless one: most cycles only use each other's bindings inside
// functions, which run after every module has loaded. The research counted 19 cycles in ahra and one
// read at load. So the rule reports the read, never the cycle: a binding read while the reading
// module is still loading, declared in another module of the same cycle. Whether the read throws
// today depends on which module of the cycle the program enters first, which no single file shows and
// any import can change, so it is reported as the latent crash it is.
//
// # Which imports are runtime edges
//
// The cycle is computed over the edges that execute, and each doubt removes an edge, because a
// missing edge can only hide a cycle while an extra one can invent a cycle that never runs. An edge is:
//
//   - `import './x'`, which runs the module for its effects and is never elided.
//   - `import { a } from './x'` (or a default or namespace import), when the file reads one of its
//     bindings as a value and the binding resolves to a value that is not a `const enum`. This is
//     TypeScript's own elision rule, read through the checker: an import used only in types is
//     dropped by tsc, SWC and esbuild alike. Under `verbatimModuleSyntax` or Node's type stripping
//     more imports survive, so this edge set is the smallest any of those runtimes keeps.
//   - `export * from './x'` and `export * as ns from './x'`, which TypeScript never elides.
//   - `export { a } from './x'`, when a specifier that is not `type` resolves to a value.
//
// Never an edge: `import type` and `export type`, an import whose bindings are all `type` specifiers,
// a dynamic `import()` (it runs later, asynchronously), `require()` and `import x = require()` (not
// followed: a missed cycle, not a false one), an import that resolves outside the project or into a
// declaration file, and any import of a `'use server'` module. That last one is a Next.js boundary:
// a client module importing a server action gets a reference, not the module, so a cycle through it
// may exist in neither the server bundle nor the client bundle.
//
// # What counts as a read while loading
//
// Every identifier the module's own evaluation runs, found by symbol identity rather than by
// spelling, so a local of the same name shadowing the import is never mistaken for it: top-level
// statements and expressions, `export default <expression>`, a class's `extends` clause, decorators
// on a class, `static` field initializers and `static {}` blocks, enum member initializers, and the
// body of a non-ambient `namespace`. `typeof X` counts, since `typeof` throws on a binding still in
// its temporal dead zone. A namespace import is read through its member: `ns.X` reads `X`.
//
// Code that runs later is not a read while loading: function, method, accessor and constructor
// bodies, arrow functions, parameter defaults, and instance field initializers, which run at
// construction. Neither are type positions, `export { X }` (it forwards the binding, it does not
// read it), or anything `declare`d.
//
// # Which bindings can be read too early
//
// The imported binding is resolved through every re-export to the declaration that creates it, and
// the cycle check is made against that declaration's file, because an ES module re-export is a live
// view of the original binding, not a copy. Reported: `const`, `let` and `var` (a `var` reads as
// `undefined` rather than throwing, which is the same bug, quieter), a `class`, a non-`const` `enum`,
// and `export default <expression>`. Not reported: a `function` declaration, which is hoisted and
// callable before its module runs (what the function does when called is not followed, so a hoisted
// function reading its own module's `const` at load is a missed finding), a `const enum`, which a
// compiler may inline, and an ambient declaration, which no module initializes.
//
// # Cost
//
// The import graph is a fact about the whole program, so it is built once per run, the first time a
// file asks, under a package lock, as `consistency-require-constant-casing` builds its importer index.
// Resolving every import is cheap. The checker is asked about references only inside the cycles found
// by the cheaper, syntactic graph, so the expensive half runs on a few dozen files rather than on the
// program. The rule reads other files, so it declares `ReadsOtherFiles` and the findings cache never
// replays it.
//
// # No fix
//
// Breaking the cycle is the fix, and where to cut it is a design decision: move the shared piece to a
// file both sides import, move the read into a function, or drop the back import. A fixer would pick
// one blindly.
var CorrectnessNoImportCycleLoadTimeRead = rule.Rule{
	Name: "nexus/correctness-no-import-cycle-load-time-read",

	// Symbol identity is what tells an import from a shadowing local, a value use from a type use,
	// and an import's binding from the declaration a chain of re-exports finally lands on.
	NeedsTypeChecker: true,

	// The graph reads every file's imports and their resolutions, this file's among them.
	ProgramReads: rule.ReadsModuleResolution | rule.ReadsOtherFiles,

	// Only a file inside a runtime import cycle can hold a finding, and most files are in none.
	NoListener: rule.NoListenerDeclinesIrrelevantFiles,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.TypeChecker == nil || ctx.Program == nil || ctx.SourceFile == nil {
			return nil
		}
		graph := correctnessNoImportCycleLoadTimeReadGraphFor(ctx)
		if _, inCycle := graph.component[ctx.SourceFile.Path()]; !inCycle {
			return nil
		}
		return rule.Listeners{
			ast.KindSourceFile: func(sourceFile *ast.Node) {
				correctnessNoImportCycleLoadTimeReadScanFile(ctx, graph)
			},
		}
	},
}

// correctnessNoImportCycleLoadTimeReadGraph is the program's runtime import graph, kept only for the
// files inside a cycle.
type correctnessNoImportCycleLoadTimeReadGraph struct {
	// component numbers each file's strongly connected component. A file in no cycle is absent.
	component map[tspath.Path]int
	// edges are the runtime edges between files of the same component, for naming the cycle.
	edges map[tspath.Path][]tspath.Path
	// fileNames spells each path the way the program does, for the message.
	fileNames map[tspath.Path]string
}

// correctnessNoImportCycleLoadTimeReadCache holds one graph per program, for the reason
// `boundary_no_project_theme_value.go` gives: the file cache is per file, the program does not exist
// at registration, and the graph is a fact about the whole run.
var correctnessNoImportCycleLoadTimeReadCache struct {
	sync.Mutex
	program rule.ProgramIdentity
	graph   *correctnessNoImportCycleLoadTimeReadGraph
}

// correctnessNoImportCycleLoadTimeReadGraphFor returns the run's graph, building it on the first ask.
//
// The build asks this file's checker about other files' references. That is the checker's ordinary
// work (resolving any import already reaches into another file) and every query only reads the
// program and writes the asking checker's own tables, which this goroutine holds exclusively.
func correctnessNoImportCycleLoadTimeReadGraphFor(ctx rule.Context) *correctnessNoImportCycleLoadTimeReadGraph {
	correctnessNoImportCycleLoadTimeReadCache.Lock()
	defer correctnessNoImportCycleLoadTimeReadCache.Unlock()
	if correctnessNoImportCycleLoadTimeReadCache.program == ctx.Program.Identity() && correctnessNoImportCycleLoadTimeReadCache.graph != nil {
		return correctnessNoImportCycleLoadTimeReadCache.graph
	}
	graph := correctnessNoImportCycleLoadTimeReadBuildGraph(ctx.Program, ctx.TypeChecker)
	correctnessNoImportCycleLoadTimeReadCache.program = ctx.Program.Identity()
	correctnessNoImportCycleLoadTimeReadCache.graph = graph
	return graph
}

// correctnessNoImportCycleLoadTimeReadCandidate is one import or re-export statement and the project
// file it resolves to.
type correctnessNoImportCycleLoadTimeReadCandidate struct {
	statement *ast.Node
	target    tspath.Path
}

// correctnessNoImportCycleLoadTimeReadBuildGraph builds the graph in two passes.
//
// The first is syntactic: every import and re-export that is not written `type`, resolved. Its cycles
// are a superset of the runtime cycles, because it can only hold more edges. The second asks the
// checker which of the edges inside those cycles survive elision, and computes the cycles again over
// the survivors. Only a file in a cycle of the first pass is ever handed to the checker.
func correctnessNoImportCycleLoadTimeReadBuildGraph(program rule.Program, typeChecker *checker.Checker) *correctnessNoImportCycleLoadTimeReadGraph {
	graph := &correctnessNoImportCycleLoadTimeReadGraph{
		component: map[tspath.Path]int{},
		edges:     map[tspath.Path][]tspath.Path{},
		fileNames: map[tspath.Path]string{},
	}
	byPath := map[tspath.Path]*ast.SourceFile{}
	var paths []tspath.Path
	for _, sourceFile := range program.SourceFiles() {
		if sourceFile == nil || sourceFile.IsDeclarationFile || strings.Contains(sourceFile.FileName(), "/node_modules/") {
			continue
		}
		byPath[sourceFile.Path()] = sourceFile
		paths = append(paths, sourceFile.Path())
	}
	sort.Slice(paths, func(left int, right int) bool { return paths[left] < paths[right] })

	candidates := map[tspath.Path][]correctnessNoImportCycleLoadTimeReadCandidate{}
	for _, importerPath := range paths {
		importer := byPath[importerPath]
		for _, statement := range importer.Statements.Nodes {
			var specifier *ast.Node
			switch statement.Kind {
			case ast.KindImportDeclaration:
				declaration := statement.AsImportDeclaration()
				if declaration.ImportClause != nil && declaration.ImportClause.IsTypeOnly() {
					continue
				}
				specifier = declaration.ModuleSpecifier
			case ast.KindExportDeclaration:
				declaration := statement.AsExportDeclaration()
				if declaration.IsTypeOnly {
					continue
				}
				specifier = declaration.ModuleSpecifier
			default:
				continue
			}
			if specifier == nil || !ast.IsStringLiteralLike(specifier) {
				continue
			}
			resolved := program.ResolveModule(importer, specifier)
			if !resolved.IsResolved() {
				continue
			}
			target := program.GetSourceFileForResolvedModule(resolved.ResolvedFileName)
			if target == nil || target == importer || byPath[target.Path()] == nil || correctnessNoImportCycleLoadTimeReadIsServerActions(target) {
				continue
			}
			candidates[importerPath] = append(candidates[importerPath], correctnessNoImportCycleLoadTimeReadCandidate{statement: statement, target: target.Path()})
		}
	}

	superset := correctnessNoImportCycleLoadTimeReadComponents(paths, func(from tspath.Path) []tspath.Path {
		var targets []tspath.Path
		for _, candidate := range candidates[from] {
			targets = append(targets, candidate.target)
		}
		return targets
	})

	verified := map[tspath.Path][]tspath.Path{}
	for _, importerPath := range paths {
		importerComponent, inCycle := superset[importerPath]
		if !inCycle {
			continue
		}
		var referenced map[*ast.Symbol]bool
		for _, candidate := range candidates[importerPath] {
			if targetComponent, targetInCycle := superset[candidate.target]; !targetInCycle || targetComponent != importerComponent {
				continue
			}
			if referenced == nil {
				referenced = correctnessNoImportCycleLoadTimeReadReferencedImports(typeChecker, byPath[importerPath])
			}
			if correctnessNoImportCycleLoadTimeReadIsRuntimeEdge(typeChecker, candidate.statement, referenced) {
				verified[importerPath] = append(verified[importerPath], candidate.target)
			}
		}
	}

	graph.component = correctnessNoImportCycleLoadTimeReadComponents(paths, func(from tspath.Path) []tspath.Path {
		return verified[from]
	})
	for from, targets := range verified {
		for _, target := range targets {
			if graph.component[from] == graph.component[target] {
				if _, inCycle := graph.component[from]; inCycle {
					graph.edges[from] = append(graph.edges[from], target)
				}
			}
		}
	}
	for filePath := range graph.component {
		graph.fileNames[filePath] = byPath[filePath].FileName()
	}
	return graph
}

// correctnessNoImportCycleLoadTimeReadIsServerActions reports whether a file opens with
// `'use server'`, the directive that turns an import of it from client code into a reference.
func correctnessNoImportCycleLoadTimeReadIsServerActions(sourceFile *ast.SourceFile) bool {
	for _, statement := range sourceFile.Statements.Nodes {
		if !ast.IsPrologueDirective(statement) {
			return false
		}
		if statement.AsExpressionStatement().Expression.Text() == "use server" {
			return true
		}
	}
	return false
}

// correctnessNoImportCycleLoadTimeReadIsRuntimeEdge reports whether one import or re-export statement
// survives elision. See the doc comment's list.
func correctnessNoImportCycleLoadTimeReadIsRuntimeEdge(typeChecker *checker.Checker, statement *ast.Node, referenced map[*ast.Symbol]bool) bool {
	switch statement.Kind {
	case ast.KindImportDeclaration:
		if statement.AsImportDeclaration().ImportClause == nil {
			return true
		}
		for _, name := range correctnessNoImportCycleLoadTimeReadBindingNames(statement) {
			alias := typeChecker.GetSymbolAtLocation(name.name)
			if alias != nil && referenced[alias] && correctnessNoImportCycleLoadTimeReadIsValueAlias(typeChecker, alias) {
				return true
			}
		}
		return false
	case ast.KindExportDeclaration:
		exportClause := statement.AsExportDeclaration().ExportClause
		if exportClause == nil || ast.IsNamespaceExport(exportClause) {
			return true
		}
		for _, element := range exportClause.AsNamedExports().Elements.Nodes {
			if element.IsTypeOnly() {
				continue
			}
			if correctnessNoImportCycleLoadTimeReadIsValueAlias(typeChecker, element.Symbol()) {
				return true
			}
		}
	}
	return false
}

// correctnessNoImportCycleLoadTimeReadIsValueAlias reports whether an import or export alias lands on
// a runtime value: not a type, not reached through a `type` import or export, not a `const enum`.
func correctnessNoImportCycleLoadTimeReadIsValueAlias(typeChecker *checker.Checker, alias *ast.Symbol) bool {
	if alias == nil || typeChecker.GetTypeOnlyAliasDeclaration(alias) != nil {
		return false
	}
	target := typeChecker.GetAliasedSymbol(alias)
	if target == nil || typeChecker.IsUnknownSymbol(target) {
		return false
	}
	return target.Flags&ast.SymbolFlagsValue != 0 && target.Flags&ast.SymbolFlagsConstEnum == 0
}

// correctnessNoImportCycleLoadTimeReadBindingName is one local name an import binds.
type correctnessNoImportCycleLoadTimeReadBindingName struct {
	name      *ast.Node
	namespace bool
}

// correctnessNoImportCycleLoadTimeReadBindingNames returns the local names of an import's bindings,
// leaving out `type` specifiers and every name of an `import type`.
func correctnessNoImportCycleLoadTimeReadBindingNames(statement *ast.Node) []correctnessNoImportCycleLoadTimeReadBindingName {
	declaration := statement.AsImportDeclaration()
	if declaration.ImportClause == nil || declaration.ImportClause.IsTypeOnly() {
		return nil
	}
	bindings := imports.BindingsOf(statement)
	var names []correctnessNoImportCycleLoadTimeReadBindingName
	if bindings.Default != nil {
		names = append(names, correctnessNoImportCycleLoadTimeReadBindingName{name: bindings.Default})
	}
	if bindings.Namespace != nil && bindings.Namespace.Name() != nil {
		names = append(names, correctnessNoImportCycleLoadTimeReadBindingName{name: bindings.Namespace.Name(), namespace: true})
	}
	for _, specifier := range bindings.Named {
		if !specifier.IsTypeOnly() && specifier.Name() != nil {
			names = append(names, correctnessNoImportCycleLoadTimeReadBindingName{name: specifier.Name()})
		}
	}
	return names
}

// correctnessNoImportCycleLoadTimeReadReferencedImports returns the import aliases of one file that it
// reads as a value anywhere, at load or later: the references that keep an import from being elided.
func correctnessNoImportCycleLoadTimeReadReferencedImports(typeChecker *checker.Checker, sourceFile *ast.SourceFile) map[*ast.Symbol]bool {
	referenced := map[*ast.Symbol]bool{}
	names := map[string]bool{}
	for _, statement := range sourceFile.Statements.Nodes {
		if statement.Kind != ast.KindImportDeclaration {
			continue
		}
		for _, name := range correctnessNoImportCycleLoadTimeReadBindingNames(statement) {
			names[name.name.Text()] = true
		}
	}
	if len(names) == 0 {
		return referenced
	}
	correctnessNoImportCycleLoadTimeReadWalkValueIdentifiers(sourceFile.AsNode(), false, func(identifier *ast.Node) {
		if !names[identifier.Text()] {
			return
		}
		if symbol := correctnessNoImportCycleLoadTimeReadReferencedSymbol(typeChecker, identifier); symbol != nil {
			referenced[symbol] = true
		}
	})
	return referenced
}

// correctnessNoImportCycleLoadTimeReadReferencedSymbol resolves an identifier in a value position to
// the symbol it reads. A shorthand property `{ X }` names a property and reads a value, and the value
// is the one wanted.
func correctnessNoImportCycleLoadTimeReadReferencedSymbol(typeChecker *checker.Checker, identifier *ast.Node) *ast.Symbol {
	if parent := identifier.Parent; parent != nil && ast.IsShorthandPropertyAssignment(parent) && parent.Name() == identifier {
		return typeChecker.GetShorthandAssignmentValueSymbol(parent)
	}
	return typeChecker.GetSymbolAtLocation(identifier)
}

// correctnessNoImportCycleLoadTimeReadWalkValueIdentifiers calls visit on each identifier in an
// expression position under node: not in a type, not in an import or export clause, not under
// `declare`. With loadOnly, it also stops at code that runs after the module has loaded.
func correctnessNoImportCycleLoadTimeReadWalkValueIdentifiers(node *ast.Node, loadOnly bool, visit func(identifier *ast.Node)) {
	var walk func(node *ast.Node) bool
	walk = func(node *ast.Node) bool {
		if node == nil || node.Flags&ast.NodeFlagsAmbient != 0 {
			return false
		}
		switch node.Kind {
		case ast.KindIdentifier:
			// `</Name>` closes the element `<Name>` already read; it is not a second read.
			if !ast.IsJsxClosingElement(node.Parent) {
				visit(node)
			}
			return false
		case ast.KindImportDeclaration, ast.KindImportEqualsDeclaration, ast.KindExportDeclaration,
			ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindTypeParameter:
			return false
		case ast.KindEnumDeclaration:
			if ast.IsEnumConst(node) {
				return false
			}
		case ast.KindHeritageClause:
			// `extends Base` on a class evaluates Base when the class is defined. `implements` and an
			// interface's `extends` are types.
			heritage := node.AsHeritageClause()
			if heritage.Token == ast.KindExtendsKeyword && ast.IsClassLike(node.Parent) {
				for _, element := range heritage.Types.Nodes {
					walk(element.AsExpressionWithTypeArguments().Expression)
				}
			}
			return false
		case ast.KindExpressionWithTypeArguments:
			// Outside a heritage clause this is an instantiation expression, `make<string>`, which reads
			// `make`. The type arguments are types.
			walk(node.AsExpressionWithTypeArguments().Expression)
			return false
		case ast.KindPropertyDeclaration:
			// An instance field's initializer runs at construction, a static one when the class is
			// defined.
			if loadOnly && !ast.HasStaticModifier(node) {
				return false
			}
		}
		if loadOnly && ast.IsFunctionLike(node) {
			return false
		}
		if ast.IsTypeNode(node) {
			return false
		}
		node.ForEachChild(walk)
		return false
	}
	node.ForEachChild(walk)
}

// correctnessNoImportCycleLoadTimeReadImportBinding is one of the scanned file's import bindings.
type correctnessNoImportCycleLoadTimeReadImportBinding struct {
	alias     *ast.Symbol
	namespace bool
}

func correctnessNoImportCycleLoadTimeReadScanFile(ctx rule.Context, graph *correctnessNoImportCycleLoadTimeReadGraph) {
	readerPath := ctx.SourceFile.Path()
	bindingsByName := map[string][]correctnessNoImportCycleLoadTimeReadImportBinding{}
	for _, statement := range ctx.SourceFile.Statements.Nodes {
		if statement.Kind != ast.KindImportDeclaration {
			continue
		}
		for _, name := range correctnessNoImportCycleLoadTimeReadBindingNames(statement) {
			if alias := ctx.TypeChecker.GetSymbolAtLocation(name.name); alias != nil {
				bindingsByName[name.name.Text()] = append(bindingsByName[name.name.Text()], correctnessNoImportCycleLoadTimeReadImportBinding{alias: alias, namespace: name.namespace})
			}
		}
	}
	if len(bindingsByName) == 0 {
		return
	}

	correctnessNoImportCycleLoadTimeReadWalkValueIdentifiers(ctx.SourceFile.AsNode(), true, func(identifier *ast.Node) {
		bindings := bindingsByName[identifier.Text()]
		if len(bindings) == 0 {
			return
		}
		symbol := correctnessNoImportCycleLoadTimeReadReferencedSymbol(ctx.TypeChecker, identifier)
		for _, binding := range bindings {
			if binding.alias != symbol {
				continue
			}
			read, target := identifier, (*ast.Symbol)(nil)
			if binding.namespace {
				// The namespace object is complete as soon as its module is linked; reading one of its
				// members is what reaches the binding.
				access := identifier.Parent
				if access == nil || !ast.IsPropertyAccessExpression(access) || access.Expression() != identifier {
					return
				}
				member := ctx.TypeChecker.GetSymbolAtLocation(access.Name())
				if member == nil {
					return
				}
				read, target = access, ctx.TypeChecker.SkipAlias(member)
			} else {
				target = ctx.TypeChecker.GetAliasedSymbol(binding.alias)
			}
			declaration := correctnessNoImportCycleLoadTimeReadUninitializedDeclaration(ctx.TypeChecker, target)
			if declaration == nil {
				return
			}
			declaringFile := ast.GetSourceFileOfNode(declaration)
			if declaringFile == nil || declaringFile.Path() == readerPath {
				return
			}
			if declaringComponent, inCycle := graph.component[declaringFile.Path()]; !inCycle || declaringComponent != graph.component[readerPath] {
				return
			}
			ctx.ReportNode(read, rule.Message{
				Id: correctnessNoImportCycleLoadTimeReadId,
				Description: correctnessNoImportCycleLoadTimeReadText.Render(map[string]string{
					"name":  target.Name,
					"file":  path.Base(declaringFile.FileName()),
					"cycle": graph.cycleThrough(readerPath, declaringFile.Path()),
				}),
			})
			return
		}
	})
}

// correctnessNoImportCycleLoadTimeReadUninitializedDeclaration returns the declaration that creates a
// binding when its module runs, or nil for one that exists before: a hoisted function, a `const
// enum`, an ambient declaration, or anything the checker could not resolve.
func correctnessNoImportCycleLoadTimeReadUninitializedDeclaration(typeChecker *checker.Checker, target *ast.Symbol) *ast.Node {
	if target == nil || typeChecker.IsUnknownSymbol(target) || target.ValueDeclaration == nil {
		return nil
	}
	declaration := target.ValueDeclaration
	if declaration.Flags&ast.NodeFlagsAmbient != 0 {
		return nil
	}
	switch declaration.Kind {
	case ast.KindVariableDeclaration, ast.KindBindingElement, ast.KindClassDeclaration, ast.KindClassExpression:
		return declaration
	case ast.KindEnumDeclaration:
		if !ast.IsEnumConst(declaration) {
			return declaration
		}
	case ast.KindExportAssignment:
		if !declaration.AsExportAssignment().IsExportEquals {
			return declaration
		}
	}
	return nil
}

// cycleThrough names one cycle through both files, `A.ts → B.ts → A.ts`, from the shortest path each
// way between them.
func (graph *correctnessNoImportCycleLoadTimeReadGraph) cycleThrough(reader tspath.Path, declaring tspath.Path) string {
	there := graph.shortestPath(reader, declaring)
	back := graph.shortestPath(declaring, reader)
	if len(there) == 0 || len(back) == 0 {
		return "an import cycle"
	}
	var names []string
	for _, step := range append(there, back[1:]...) {
		names = append(names, path.Base(graph.fileNames[step]))
	}
	return strings.Join(names, " → ")
}

// shortestPath returns the files from one to the other along runtime edges, both ends included.
func (graph *correctnessNoImportCycleLoadTimeReadGraph) shortestPath(from tspath.Path, to tspath.Path) []tspath.Path {
	previous := map[tspath.Path]tspath.Path{from: from}
	queue := []tspath.Path{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == to {
			var steps []tspath.Path
			for step := to; step != from; step = previous[step] {
				steps = append(steps, step)
			}
			steps = append(steps, from)
			for left, right := 0, len(steps)-1; left < right; left, right = left+1, right-1 {
				steps[left], steps[right] = steps[right], steps[left]
			}
			return steps
		}
		for _, next := range graph.edges[current] {
			if _, seen := previous[next]; !seen {
				previous[next] = current
				queue = append(queue, next)
			}
		}
	}
	return nil
}

// correctnessNoImportCycleLoadTimeReadComponents runs Tarjan's algorithm and numbers every strongly
// connected component of two or more files. A file in no such component is left out.
func correctnessNoImportCycleLoadTimeReadComponents(paths []tspath.Path, edgesOf func(from tspath.Path) []tspath.Path) map[tspath.Path]int {
	index := map[tspath.Path]int{}
	lowLink := map[tspath.Path]int{}
	onStack := map[tspath.Path]bool{}
	var stack []tspath.Path
	components := map[tspath.Path]int{}
	nextIndex, nextComponent := 0, 0

	var connect func(node tspath.Path)
	connect = func(node tspath.Path) {
		index[node], lowLink[node] = nextIndex, nextIndex
		nextIndex++
		stack = append(stack, node)
		onStack[node] = true
		for _, next := range edgesOf(node) {
			if _, visited := index[next]; !visited {
				connect(next)
				lowLink[node] = min(lowLink[node], lowLink[next])
			} else if onStack[next] {
				lowLink[node] = min(lowLink[node], index[next])
			}
		}
		if lowLink[node] != index[node] {
			return
		}
		var members []tspath.Path
		for {
			member := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[member] = false
			members = append(members, member)
			if member == node {
				break
			}
		}
		if len(members) < 2 {
			return
		}
		for _, member := range members {
			components[member] = nextComponent
		}
		nextComponent++
	}
	for _, node := range paths {
		if _, visited := index[node]; !visited {
			connect(node)
		}
	}
	return components
}
