package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/property"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/ecmascript/scope"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

const concurrencyNoCheckThenWriteId = "checkThenWrite"

// concurrencyNoCheckThenWriteText is the rule's message, whose wording lives in
// `policy/messages/concurrency-no-check-then-write.json`.
var concurrencyNoCheckThenWriteText = policy.MessageOf(
	"nexus/concurrency-no-check-then-write",
	concurrencyNoCheckThenWriteId,
)

// concurrencyNoCheckThenWriteMessage names the write that claims the name, so a reader of the finding
// on the check can find the second half of the race without reading the whole function.
func concurrencyNoCheckThenWriteMessage(write string) rule.Message {
	return rule.Message{
		Id:          concurrencyNoCheckThenWriteId,
		Description: concurrencyNoCheckThenWriteText.Render(map[string]string{"write": write}),
	}
}

// ConcurrencyNoCheckThenWrite reports a loop that searches for a free file name by checking whether
// each candidate exists, followed by a write that creates the chosen name without refusing a file that
// is already there. The check and the write are two steps, so two writers can both find the same
// name free, and the second write replaces the first's file.
//
//	invalid: while(true) { try { await access(join(directory, `${index}.png`)); index++; } catch { break; } }
//	         await writeFile(join(directory, `${index}.png`), bytes);
//	invalid: do { name = `${base}-${number}.png`; number++; } while(existsSync(join(directory, name)));
//	         copyFileSync(source, join(directory, name));
//	valid:   await writeFile(join(directory, `${index}.png`), bytes, { flag: 'wx' });   claims and writes at once
//	valid:   while(existsSync(lockPath)) { await delay(100); } writeFileSync(lockPath, '');   no counter
//
// # Where it came from
//
// `modules/intelligence/IntelligenceMediaGenerationApi.ts` in ahra, `persistArtifact` (fixed
// 2026-10-01): it found the next free `<index>.<ext>` with `access()` in a loop and then wrote that
// name with a plain `writeFile`, so two generations saving at once picked the same index and one image
// was lost. The fix, now at `:121`, writes with `{ flag: 'wx' }` and moves to the next index on
// `EEXIST`. Eight live copies of the before-shape were found by the new-rules sweep (`#tevhg3f`) and
// filed as `#9erqsas`: `modules/kling/KlingApi.ts:253` and `:383`, `modules/magnific/MagnificApi.ts:406`,
// `modules/openai/OpenAiApi.ts:309`, `modules/openai/OpenAiCommandLineInterface.ts:144`,
// `modules/art/ArtApi.ts:231`, `modules/art/ArtLibrary.ts:65` and
// `modules/phi/social/PhiSocialLibrary.ts:210`. Built as `#2xqtnzj`.
//
// # Every condition, each one required
//
//   - **A loop that stops on a free name.** Two shapes, nothing else:
//     the loop's condition is exactly `existsSync(path)` (a `while`, a `do ... while` or a `for`), so
//     it ends when the path does not exist; or the loop body holds, as one of its own statements, a
//     `try` with no `finally` whose `catch` is exactly `break;`, and whose `try` block opens with the
//     check (`await access(path)`, `await stat(path)`, `await lstat(path)` from the promise API, or
//     `accessSync`, `statSync`, `lstatSync`) followed by nothing but `++`, `--`, `+=` or `-=` on a
//     variable, so it ends exactly when the check throws. The check takes one argument: `statSync(path,
//     { throwIfNoEntry: false })` never throws, and a mode passed to `access` asks a different question.
//   - **The check is Node's.** Every callee resolves through the checker, aliases followed, to a
//     function declared in the module `fs`, `node:fs`, `fs/promises` or `node:fs/promises`, whether it
//     was reached through a namespace import, a named import or `fs.promises`. A project's own
//     `exists()` or `access()` never matches, whatever its name.
//   - **The checked path is built from a counter.** Some variable the path reads is changed inside the
//     loop by `++`, `--`, `+=` or `-=`, directly or through a variable assigned (or declared) in the loop
//     from one that is. That is what makes the loop a search for a free name rather than a wait (a
//     `while(existsSync(lockPath))` that sleeps until a lock file goes away is a different bug class,
//     and is left alone).
//   - **A write after the loop creates that same path without exclusive create.** The write is in a
//     statement that runs after the loop in the same function, not inside a nested function and not
//     past an enclosing loop (where the next round would run the search again). It resolves to Node's
//     `writeFile`, `writeFileSync`, `copyFile`, `copyFileSync`, `rename`, `renameSync` or
//     `createWriteStream`, or to sharp's `toFile` (below). It is not exclusive only when the source
//     says so: no options, an encoding string, a callback, or an object literal whose `flag` (`flags`
//     for a stream) is absent or a string without `x`. A `copyFile` mode is not exclusive only when
//     absent. Anything the rule cannot read, a variable holding the options, a spread, a computed key,
//     leaves the write alone.
//   - **Same path, proven.** The write's path and the checked path are the same expression once
//     `const` locals are read through their initializers: identifiers resolving to the same binding,
//     string, template and number literals with the same text, `+`, and calls to Node's `path.join`
//     (resolved like the `fs` functions, pure, so the same arguments give the same path). Anything else
//     in the path, a property read, another call, makes the two unequal. A `const` is read through only
//     when it was declared after the loop, or in the loop body before the `try`; one declared earlier
//     is compared as itself, because its value was fixed before the counter moved.
//   - **The path did not move in between.** Every `let`, `var` or parameter the path reads is written
//     only by the function holding the loop (never from a nested function, which could run during an
//     `await`), and never between the end of the loop and the write, nor, when a `const` from the loop
//     body was read through, between that declaration and the check.
//
// # What it deliberately does not see
//
// Every gap is a missed finding, never a false one.
//
//   - A check followed by a write without a loop (`if(!existsSync(path)) writeFileSync(path, ...)`).
//     Measured by the research probe at 27 such sites on ahra, nearly all create-if-missing and cache
//     files, where the second writer would write the same bytes. The loop with a counter is what says
//     the name is being claimed.
//   - A write inside the loop, or in a callback after it, or a check that is not the loop's exit
//     (`if(!existsSync(path)) break;`, a `catch` that tests the error code before breaking).
//   - A path read through a property (`input.outputDirectory`), because anything else holding the
//     object can change it during the `await` between the check and the write, so the two paths cannot
//     be proven equal. That is how the incident's own before-file read its directory and extension,
//     so `persistArtifact` as it stood before the fix is a missed finding (a fixture says so); with
//     the fields read into locals it fires. Also a call other than `path.join` (`path.resolve` reads
//     the working directory, which can change), and a `const` declared before the loop and spelled
//     differently on the two sides.
//   - sharp's `toFile` on an instance in a file that does not import `sharp` itself. sharp's `toFile`
//     is matched only when the method resolves to a declaration in the very file the compiler resolved
//     this file's own `sharp` import to, a package the resolver identifies as `sharp`. That is identity
//     through the resolver and the checker, not a name, and it always overwrites: sharp has no
//     exclusive-create option.
//   - `open` with a write flag, `cp`, `symlink`, `link`, and every third-party writer other than sharp.
//
// # No fix
//
// The fix changes the shape of the loop, claim and write in one step and retry on `EEXIST`, and the
// write is often far from the check, past a network download (`KlingApi.ts:253`) or an encode
// (`PhiSocialLibrary.ts`). Moving it is the author's job. `#9erqsas` proposes one library helper for
// all eight sites rather than eight patched loops.
var ConcurrencyNoCheckThenWrite = rule.Rule[rule.NoOptions]{
	Name:             "nexus/concurrency-no-check-then-write",
	NeedsTypeChecker: true,
	// Where this file's `sharp` import resolves.
	ProgramReads: rule.ReadsModuleResolution,

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.TypeChecker == nil {
			// Every callee is identified through the checker. Without it the rule could only match
			// names, so it declines the file.
			return nil
		}
		check := func(node *ast.Node) {
			concurrencyNoCheckThenWriteCheckLoop(ruleContext, node)
		}
		return rule.Listeners{
			ast.KindWhileStatement: check,
			ast.KindDoStatement:    check,
			ast.KindForStatement:   check,
		}
	},
}

// concurrencyNoCheckThenWriteNodeModules are the module names Node's file system API is declared
// under, by `@types/node` from version 20 (`fs`, re-exported as `node:fs`) through 26 (`node:fs`,
// re-exported as `fs`), mapped to whether the module is the promise API.
var concurrencyNoCheckThenWriteNodeModules = map[string]bool{
	"fs":               false,
	"node:fs":          false,
	"fs/promises":      true,
	"node:fs/promises": true,
}

// concurrencyNoCheckThenWritePathModules are the module names Node's `path` is declared under.
var concurrencyNoCheckThenWritePathModules = map[string]bool{
	"path":      true,
	"node:path": true,
}

// concurrencyNoCheckThenWriteThrowingChecks are the checks that throw when the path does not exist,
// keyed by whether they come from the promise API (which must be awaited to throw inside the `try`).
var concurrencyNoCheckThenWriteThrowingChecks = map[bool]map[string]bool{
	false: {"accessSync": true, "statSync": true, "lstatSync": true},
	true:  {"access": true, "stat": true, "lstat": true},
}

// concurrencyNoCheckThenWriteWrite is one write the rule can read: where its destination path sits
// among the arguments, where its options sit, and which option key holds the open flag.
type concurrencyNoCheckThenWriteWrite struct {
	destinationIndex int
	optionsIndex     int
	flagKey          string
}

// concurrencyNoCheckThenWriteWrites are Node's writes that create or replace a file, keyed by name.
// `rename` takes no flag and always replaces. A `copyFile` mode is a number, read below as never
// provably non-exclusive unless it is absent, so it has no flag key.
var concurrencyNoCheckThenWriteWrites = map[string]concurrencyNoCheckThenWriteWrite{
	"writeFile":         {destinationIndex: 0, optionsIndex: 2, flagKey: "flag"},
	"writeFileSync":     {destinationIndex: 0, optionsIndex: 2, flagKey: "flag"},
	"copyFile":          {destinationIndex: 1, optionsIndex: 2},
	"copyFileSync":      {destinationIndex: 1, optionsIndex: 2},
	"rename":            {destinationIndex: 1, optionsIndex: -1},
	"renameSync":        {destinationIndex: 1, optionsIndex: -1},
	"createWriteStream": {destinationIndex: 0, optionsIndex: 1, flagKey: "flags"},
}

// concurrencyNoCheckThenWriteComparison is one attempt to prove a write's path equal to the checked
// path. It records the earliest loop-body `const` read through and every non-constant binding the two
// paths read, whose stability is checked once the shapes match.
type concurrencyNoCheckThenWriteComparison struct {
	bodyDeclarationStart int
	bindings             map[*ast.Symbol]bool
}

// concurrencyNoCheckThenWriteCheckLoop reports a loop that finds a free name and a later write that
// creates it without exclusive create.
func concurrencyNoCheckThenWriteCheckLoop(ruleContext rule.Context, loop *ast.Node) {
	analysis := &concurrencyNoCheckThenWriteLoop{
		ctx:      ruleContext,
		loop:     loop,
		function: scope.EnclosingFunctionLike(loop),
	}
	if !analysis.findCheck() {
		return
	}
	checkedPath := analysis.check.AsCallExpression().Arguments.Nodes[0]
	analysis.collectVarying()
	if !analysis.readsVarying(checkedPath) {
		return
	}
	for _, statement := range analysis.followingStatements() {
		write, destination := analysis.findWrite(statement, checkedPath)
		if write != nil {
			ruleContext.ReportNode(analysis.check, concurrencyNoCheckThenWriteMessage(destination))
			return
		}
	}
}

// concurrencyNoCheckThenWriteCounterOf returns the identifier an expression counts with (`x++`, `--x`,
// `x += n`, `x -= n`), or nil.
func concurrencyNoCheckThenWriteCounterOf(expression *ast.Node) *ast.Node {
	unwrapped := ast.SkipParentheses(expression)
	var operand *ast.Node
	switch unwrapped.Kind {
	case ast.KindPrefixUnaryExpression:
		if reference.IsUpdateOperator(unwrapped.AsPrefixUnaryExpression().Operator) {
			operand = unwrapped.AsPrefixUnaryExpression().Operand
		}
	case ast.KindPostfixUnaryExpression:
		if reference.IsUpdateOperator(unwrapped.AsPostfixUnaryExpression().Operator) {
			operand = unwrapped.AsPostfixUnaryExpression().Operand
		}
	case ast.KindBinaryExpression:
		switch unwrapped.AsBinaryExpression().OperatorToken.Kind {
		case ast.KindPlusEqualsToken, ast.KindMinusEqualsToken:
			operand = unwrapped.AsBinaryExpression().Left
		}
	}
	if operand == nil {
		return nil
	}
	operand = ast.SkipParentheses(operand)
	if operand.Kind != ast.KindIdentifier {
		return nil
	}
	return operand
}

// concurrencyNoCheckThenWriteHasOneArgument says whether a call passes exactly one argument, not
// spread.
func concurrencyNoCheckThenWriteHasOneArgument(call *ast.Node) bool {
	arguments := call.AsCallExpression().Arguments.Nodes
	return len(arguments) == 1 && arguments[0].Kind != ast.KindSpreadElement
}

// concurrencyNoCheckThenWriteDeclaringModule returns the name of the ambient module (`declare module
// 'node:fs'`) a declaration sits in, looking out through any namespace inside it, or "" when it is not
// in one.
func concurrencyNoCheckThenWriteDeclaringModule(declaration *ast.Node) string {
	for current := declaration.Parent; current != nil; current = current.Parent {
		if current.Kind == ast.KindModuleDeclaration {
			name := current.AsModuleDeclaration().Name()
			if name != nil && name.Kind == ast.KindStringLiteral {
				return name.Text()
			}
		}
	}
	return ""
}

// concurrencyNoCheckThenWriteDeclaredIn says whether a symbol is a function every declaration of which
// sits in one of the named ambient modules, and returns that module.
func concurrencyNoCheckThenWriteDeclaredIn(symbol *ast.Symbol, modules map[string]bool) (string, bool) {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return "", false
	}
	module := ""
	for _, declaration := range symbol.Declarations {
		if declaration.Kind != ast.KindFunctionDeclaration && declaration.Kind != ast.KindMethodSignature {
			return "", false
		}
		declared := concurrencyNoCheckThenWriteDeclaringModule(declaration)
		if _, known := modules[declared]; !known || (module != "" && declared != module) {
			return "", false
		}
		module = declared
	}
	return module, true
}

// forEachOwnNode visits a subtree in source order without entering nested functions, whose code runs
// on its own schedule.
func concurrencyNoCheckThenWriteForEachOwnNode(root *ast.Node, visitor func(node *ast.Node)) {
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) || node.Kind == ast.KindClassStaticBlockDeclaration {
			return false
		}
		visitor(node)
		node.ForEachChild(visit)
		return false
	}
	visit(root)
}

// concurrencyNoCheckThenWriteOptionsNotExclusive says whether an options argument provably leaves the
// write non-exclusive. A callback in the options position (`writeFile(path, data, done)`) passes no
// options. Anything the source does not settle answers false, which leaves the write alone.
func concurrencyNoCheckThenWriteOptionsNotExclusive(options *ast.Node, flagKey string) bool {
	unwrapped := ast.SkipParentheses(options)
	switch unwrapped.Kind {
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return true
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		// An encoding. `copyFile` and `rename` never take a string here.
		return flagKey != ""
	case ast.KindObjectLiteralExpression:
		if flagKey == "" {
			return false
		}
		for _, member := range unwrapped.AsObjectLiteralExpression().Properties.Nodes {
			if member.Kind != ast.KindPropertyAssignment {
				// A spread, a shorthand or a method could carry the flag in a way the source does
				// not show.
				return false
			}
			key, static := property.Name(member.Name(), property.Static)
			if !static {
				return false
			}
			if key != flagKey {
				continue
			}
			value := ast.SkipParentheses(member.AsPropertyAssignment().Initializer)
			if value.Kind != ast.KindStringLiteral && value.Kind != ast.KindNoSubstitutionTemplateLiteral {
				return false
			}
			if strings.Contains(value.Text(), "x") {
				return false
			}
		}
		return true
	}
	return false
}

// concurrencyNoCheckThenWriteIsConstant says whether a binding can never be reassigned: a `const`, an
// import, a function, a class or an enum. Everything else is a `let`, a `var` or a parameter, whose
// writes are checked.
func concurrencyNoCheckThenWriteIsConstant(symbol *ast.Symbol) bool {
	declaration := symbol.ValueDeclaration
	if declaration == nil {
		return false
	}
	switch declaration.Kind {
	case ast.KindVariableDeclaration:
		return ast.IsVarConst(declaration)
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindEnumDeclaration,
		ast.KindImportSpecifier, ast.KindImportClause, ast.KindNamespaceImport:
		return true
	}
	return false
}

// concurrencyNoCheckThenWriteSharpFiles is the files the compiler resolved this file's imports of the package
// `sharp` to, once per file. It reads the program, so it lives beside the rule that declares that read:
// internal/guard counts a rule's reads in the file that defines it.
func concurrencyNoCheckThenWriteSharpFiles(ruleContext rule.Context) map[string]bool {
	return rule.Cached(ruleContext.FileCache, "concurrencyNoCheckThenWriteSharpFiles", func() map[string]bool {
		files := map[string]bool{}
		if ruleContext.Program == nil {
			return files
		}
		for _, specifier := range ruleContext.SourceFile.Imports() {
			resolved := ruleContext.Program.ResolveModule(ruleContext.SourceFile, specifier)
			if resolved.IsResolved() && resolved.IsExternalLibraryImport && resolved.PackageName == "sharp" {
				files[resolved.ResolvedFileName] = true
			}
		}
		return files
	})
}
