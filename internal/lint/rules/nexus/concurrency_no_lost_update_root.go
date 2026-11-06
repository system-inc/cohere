package nexus

import (
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
	"github.com/system-inc/cohere/internal/lint/ecmascript/property"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// concurrencyNoLostUpdateRoot is the analysis of one suspending function.
type concurrencyNoLostUpdateRoot struct {
	ruleContext rule.Context
	root        *ast.Node
	locals      map[*ast.Symbol]*concurrencyNoLostUpdateLocal

	graph            *control_flow_graph.Graph[concurrencyNoLostUpdateEvent]
	between          map[concurrencyNoLostUpdateBetweenKey]bool
	writtenPaths     map[*ast.Node]*concurrencyNoLostUpdatePath
	localMemo        map[concurrencyNoLostUpdateLocalKey]concurrencyNoLostUpdateDependencies
	localInProgress  map[concurrencyNoLostUpdateLocalKey]bool
	nestedUsageCache map[*ast.Symbol]concurrencyNoLostUpdateNestedUsage
	storeAccesses    map[*ast.Node]*concurrencyNoLostUpdateStoreAccess
	stableBindings   map[*ast.Symbol]bool
}

// forEachOwnNode visits every node that runs in this root, never entering a nested one.
func (analysis *concurrencyNoLostUpdateRoot) forEachOwnNode(visitor func(node *ast.Node)) {
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if control_flow_graph.IsRoot(node) {
			return false
		}
		visitor(node)
		node.ForEachChild(visit)
		return false
	}
	analysis.root.ForEachChild(visit)
}

// collectLocal records a const whose value may carry the target's prior value: one named by an
// identifier with an initializer, or each property of an object pattern destructuring a static path.
func (analysis *concurrencyNoLostUpdateRoot) collectLocal(declaration *ast.Node) {
	if !ast.IsVarConst(declaration) {
		return
	}
	variable := declaration.AsVariableDeclaration()
	if variable.Initializer == nil {
		return
	}
	name := variable.Name()
	if name == nil {
		return
	}
	if name.Kind == ast.KindIdentifier {
		if symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(name); symbol != nil {
			analysis.locals[symbol] = &concurrencyNoLostUpdateLocal{name: name, initializer: variable.Initializer}
		}
		return
	}
	if name.Kind != ast.KindObjectBindingPattern {
		return
	}
	source, isPath := analysis.pathOf(variable.Initializer)
	if !isPath {
		return
	}
	pattern := name.AsBindingPattern()
	if pattern.Elements == nil {
		return
	}
	for _, element := range pattern.Elements.Nodes {
		binding := element.AsBindingElement()
		if binding == nil || binding.DotDotDotToken != nil {
			continue
		}
		elementName := binding.Name()
		if elementName == nil || elementName.Kind != ast.KindIdentifier {
			continue
		}
		propertyName := elementName.Text()
		if binding.PropertyName != nil {
			text, isStatic := property.Name(binding.PropertyName,
				property.Named|property.Quoted|property.Numeric|property.Computed)
			if !isStatic {
				continue
			}
			propertyName = text
		}
		symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(elementName)
		if symbol == nil {
			continue
		}
		elementPath := concurrencyNoLostUpdatePath{
			root:   source.root,
			isThis: source.isThis,
			names:  append(append([]string{}, source.names...), propertyName),
		}
		analysis.locals[symbol] = &concurrencyNoLostUpdateLocal{name: elementName, elementPath: &elementPath}
	}
}

// resolve returns the symbol an identifier occurrence reads, seeing through a shorthand property.
func (analysis *concurrencyNoLostUpdateRoot) resolve(identifier *ast.Node) *ast.Symbol {
	if parent := identifier.Parent; parent != nil && parent.Kind == ast.KindShorthandPropertyAssignment {
		if symbol := analysis.ruleContext.TypeChecker.GetShorthandAssignmentValueSymbol(parent); symbol != nil {
			return symbol
		}
	}
	return analysis.ruleContext.TypeChecker.GetSymbolAtLocation(identifier)
}

// pathOf reads an expression as a variable or a static member path, through parentheses and type
// assertions.
func (analysis *concurrencyNoLostUpdateRoot) pathOf(node *ast.Node) (concurrencyNoLostUpdatePath, bool) {
	unwrapped := concurrencyNoLostUpdateSkipTransparent(node)
	if unwrapped == nil {
		return concurrencyNoLostUpdatePath{}, false
	}
	switch unwrapped.Kind {
	case ast.KindIdentifier:
		symbol := analysis.resolve(unwrapped)
		if symbol == nil {
			return concurrencyNoLostUpdatePath{}, false
		}
		return concurrencyNoLostUpdatePath{root: symbol}, true
	case ast.KindThisKeyword:
		return concurrencyNoLostUpdatePath{isThis: true}, true
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		name, isStatic := property.AccessedName(unwrapped,
			property.Named|property.Quoted|property.Templated|property.Numeric|property.Private)
		if !isStatic {
			return concurrencyNoLostUpdatePath{}, false
		}
		var object *ast.Node
		if unwrapped.Kind == ast.KindPropertyAccessExpression {
			object = unwrapped.AsPropertyAccessExpression().Expression
		} else {
			object = unwrapped.AsElementAccessExpression().Expression
		}
		objectPath, isPath := analysis.pathOf(object)
		if !isPath {
			return concurrencyNoLostUpdatePath{}, false
		}
		objectPath.names = append(append([]string{}, objectPath.names...), name)
		return objectPath, true
	}
	return concurrencyNoLostUpdatePath{}, false
}

// compoundTargetOf reads a compound assignment's member target whose key is computed (`a[k].b += 1`)
// as its root plus one name that matches nothing.
//
// A compound assignment evaluates its target reference once and both reads and writes through it, so
// a computed key cannot name two different properties between the read and the write. That is what
// makes `state.byKey[key] += await next()` judgeable when `state.byKey[key] = state.byKey[key] +
// await next()` is not: there the two `key` reads are separate, and nothing here can show `key` held
// still. Only the root matters, for condition 4.
func (analysis *concurrencyNoLostUpdateRoot) compoundTargetOf(node *ast.Node) (concurrencyNoLostUpdatePath, bool) {
	current := concurrencyNoLostUpdateSkipTransparent(node)
	if current == nil ||
		(current.Kind != ast.KindPropertyAccessExpression && current.Kind != ast.KindElementAccessExpression) {
		return concurrencyNoLostUpdatePath{}, false
	}
	for current.Kind == ast.KindPropertyAccessExpression || current.Kind == ast.KindElementAccessExpression {
		if current.Kind == ast.KindPropertyAccessExpression {
			current = concurrencyNoLostUpdateSkipTransparent(current.AsPropertyAccessExpression().Expression)
		} else {
			current = concurrencyNoLostUpdateSkipTransparent(current.AsElementAccessExpression().Expression)
		}
		if current == nil {
			return concurrencyNoLostUpdatePath{}, false
		}
	}
	root, isPath := analysis.pathOf(current)
	if !isPath {
		return concurrencyNoLostUpdatePath{}, false
	}
	root.names = []string{concurrencyNoLostUpdateComputedName}
	return root, true
}

// targetIsShared is condition 4: someone other than this line can write the target while the
// function is suspended.
func (analysis *concurrencyNoLostUpdateRoot) targetIsShared(target concurrencyNoLostUpdatePath) bool {
	if target.isThis {
		return true
	}
	if target.store != "" && target.root == nil {
		// A store of free functions lives outside this call by construction.
		return true
	}
	declaration := target.root.ValueDeclaration
	if declaration == nil && len(target.root.Declarations) > 0 {
		declaration = target.root.Declarations[0]
	}
	if declaration == nil {
		return false
	}
	if !concurrencyNoLostUpdateWithin(declaration, analysis.root) {
		return true
	}
	usage := analysis.nestedUsage(target.root)
	// A method store's entry lives in the object its receiver holds, so it is judged as a member is.
	if len(target.names) == 0 && target.store == "" {
		// The binding itself. A parameter or a local is this call's own, so only a nested function
		// writing it can race this one.
		return usage.written
	}
	if declaration.Kind == ast.KindParameter && declaration.Parent == analysis.root {
		return true
	}
	return usage.referenced
}

// nestedUsage reports whether a function nested in this root references or writes a symbol.
func (analysis *concurrencyNoLostUpdateRoot) nestedUsage(symbol *ast.Symbol) concurrencyNoLostUpdateNestedUsage {
	if usage, cached := analysis.nestedUsageCache[symbol]; cached {
		return usage
	}
	usage := concurrencyNoLostUpdateNestedUsage{}
	var visit func(node *ast.Node, nested bool) bool
	visit = func(node *ast.Node, nested bool) bool {
		// Once the walk enters a nested function, everything below it is nested too.
		withinNested := nested || control_flow_graph.IsRoot(node)
		if withinNested && node.Kind == ast.KindIdentifier && analysis.resolve(node) == symbol {
			usage.referenced = true
			if reference.WritesToBinding(node) {
				usage.written = true
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			return visit(child, withinNested)
		})
		return false
	}
	analysis.root.ForEachChild(func(child *ast.Node) bool {
		return visit(child, false)
	})
	analysis.nestedUsageCache[symbol] = usage
	return usage
}

// losesUpdate is condition 5 and 6 for one candidate write.
func (analysis *concurrencyNoLostUpdateRoot) losesUpdate(
	assignment *ast.Node,
	target concurrencyNoLostUpdatePath,
) bool {
	binary := assignment.AsBinaryExpression()
	evaluator := concurrencyNoLostUpdateEvaluator{analysis: analysis, target: target, anchor: assignment}
	outcomes := evaluator.evaluate(binary.Right, false)
	if binary.OperatorToken.Kind == ast.KindEqualsToken {
		for _, outcome := range outcomes {
			if outcome.dependencies&concurrencyNoLostUpdateStaleModified != 0 {
				return true
			}
		}
		return false
	}
	// A compound assignment reads its target before the right-hand side runs, and always computes.
	for _, outcome := range outcomes {
		if outcome.during {
			return true
		}
		if outcome.dependencies&concurrencyNoLostUpdateStaleModified != 0 ||
			outcome.dependencies&concurrencyNoLostUpdateStaleVerbatim != 0 {
			return true
		}
	}
	return false
}

// localDependencies is what a const carries of the target at the end of its declaration.
func (analysis *concurrencyNoLostUpdateRoot) localDependencies(
	symbol *ast.Symbol,
	local *concurrencyNoLostUpdateLocal,
	target concurrencyNoLostUpdatePath,
) concurrencyNoLostUpdateDependencies {
	evaluator := concurrencyNoLostUpdateEvaluator{analysis: analysis, target: target, anchor: local.name}
	if local.elementPath != nil {
		return evaluator.match(*local.elementPath)
	}
	key := concurrencyNoLostUpdateLocalKey{local: symbol, targetRoot: target.root, targetPath: target.key()}
	if dependencies, memoized := analysis.localMemo[key]; memoized {
		return dependencies
	}
	if analysis.localInProgress[key] {
		return 0
	}
	analysis.localInProgress[key] = true
	var dependencies concurrencyNoLostUpdateDependencies
	for _, outcome := range evaluator.evaluate(local.initializer, false) {
		dependencies |= outcome.dependencies
	}
	delete(analysis.localInProgress, key)
	analysis.localMemo[key] = dependencies
	return dependencies
}

// suspensionBetween reports whether some path runs from the end of a const's declaration through a
// suspension to anchor without declaring the const again and without this function writing the
// target in between.
//
// That last clause is the save-and-restore protocol, and it is why a restore is not an update even
// when what it writes back is derived from the saved value rather than identical to it. Measured on
// ahra: `modules/phi/social/PhiSocialTerminal.ts` and `modules/art/ArtTerminal.ts` save
// `process.stdout.write.bind(process.stdout)`, replace `process.stdout.write` with a filter, await the
// callback, and put the bound original back in `finally`. The `.bind` call made the saved value a
// "modification" and the restore reported. What makes it a restore is the replacement between: the
// function itself overwrote the target after reading it, so the later write puts back the state from
// before this function's own change rather than building on a value someone else may have changed.
func (analysis *concurrencyNoLostUpdateRoot) suspensionBetween(
	symbol *ast.Symbol,
	anchor *ast.Node,
	target concurrencyNoLostUpdatePath,
) bool {
	memoKey := concurrencyNoLostUpdateBetweenKey{
		symbol:     symbol,
		anchor:     anchor,
		targetRoot: target.root,
		targetPath: target.key(),
	}
	if answer, memoized := analysis.between[memoKey]; memoized {
		return answer
	}
	graph := analysis.controlFlowGraph()

	type position struct {
		block     *control_flow_graph.Block[concurrencyNoLostUpdateEvent]
		start     int
		suspended bool
	}
	var queue []position
	visited := map[[2]int]bool{}
	for _, block := range graph.Blocks {
		if !block.Reachable {
			continue
		}
		for index, event := range block.Events {
			if event.kind == concurrencyNoLostUpdateDeclarationEnd && event.symbol == symbol {
				queue = append(queue, position{block: block, start: index + 1})
			}
		}
	}

	// Finding the anchor reached after a suspension settles the answer and ends the whole search.
	answer := false
	for len(queue) > 0 && !answer {
		current := queue[0]
		queue = queue[1:]
		suspended := current.suspended
		stopped := false
		for _, event := range current.block.Events[current.start:] {
			switch event.kind {
			case concurrencyNoLostUpdateSuspend:
				// A suspension whose expression contains the anchor runs after it.
				if event.node == nil || !concurrencyNoLostUpdateContains(event.node, anchor) {
					suspended = true
				}
			case concurrencyNoLostUpdateDeclarationEnd:
				// Declaring the const again binds a fresh one, so this path says nothing about the
				// value the anchor reads. Stopping at the declaration's start instead would be
				// equivalent, and a mutant doing so survived: nothing inside a const's own initializer
				// can read it, so no anchor lies between the two.
				if event.symbol == symbol {
					stopped = true
				}
			case concurrencyNoLostUpdateDeclarationStart:
				// A const anchor is its declaration's start. A suspension inside its own initializer
				// is the evaluator's to judge, path by path, because the initializer can fork.
				if event.node == anchor && suspended {
					answer = true
				}
			case concurrencyNoLostUpdateAnchor:
				if event.node == anchor {
					if suspended {
						answer = true
					}
				} else if analysis.writesTarget(event.node, target) {
					stopped = true
				}
			}
			if stopped || answer {
				break
			}
		}
		if stopped || answer {
			continue
		}
		for _, successor := range current.block.Successors {
			if successor == nil || !successor.Reachable {
				continue
			}
			key := [2]int{successor.Index(), 0}
			if suspended {
				key[1] = 1
			}
			if visited[key] {
				continue
			}
			visited[key] = true
			queue = append(queue, position{block: successor, suspended: suspended})
		}
	}
	analysis.between[memoKey] = answer
	return answer
}

// writesTarget reports whether an assignment writes exactly the target path, or a store's write call
// writes exactly the target entry.
func (analysis *concurrencyNoLostUpdateRoot) writesTarget(
	assignment *ast.Node,
	target concurrencyNoLostUpdatePath,
) bool {
	if assignment.Kind == ast.KindCallExpression {
		access := analysis.storeAccessOf(assignment)
		return access != nil && access.isWrite && access.target.sameStoreEntry(target)
	}
	if target.store != "" {
		return false
	}
	written, cached := analysis.writtenPaths[assignment]
	if !cached {
		if path, isPath := analysis.pathOf(assignment.AsBinaryExpression().Left); isPath {
			written = &path
		}
		analysis.writtenPaths[assignment] = written
	}
	if written == nil || !written.sameRoot(target) || len(written.names) != len(target.names) {
		return false
	}
	for index, name := range target.names {
		if written.names[index] != name {
			return false
		}
	}
	return true
}

// controlFlowGraph builds the root's graph on first need, recording suspensions, the declarations of
// tracked consts, and every assignment as an anchor.
func (analysis *concurrencyNoLostUpdateRoot) controlFlowGraph() (
	graph *control_flow_graph.Graph[concurrencyNoLostUpdateEvent],
) {
	if analysis.graph != nil {
		return analysis.graph
	}
	type builder = control_flow_graph.Builder[concurrencyNoLostUpdateEvent]
	declarationSymbol := func(node *ast.Node) *ast.Symbol {
		parent := node.Parent
		if parent == nil || node.Kind != ast.KindIdentifier {
			return nil
		}
		switch parent.Kind {
		case ast.KindVariableDeclaration, ast.KindBindingElement:
			if parent.Name() != node {
				return nil
			}
		default:
			return nil
		}
		symbol := analysis.ruleContext.TypeChecker.GetSymbolAtLocation(node)
		if local := analysis.locals[symbol]; local == nil || local.name != node {
			return nil
		}
		return symbol
	}
	isForAwait := func(node *ast.Node) bool {
		return node.Kind == ast.KindForOfStatement && node.AsForInOrOfStatement().AwaitModifier != nil
	}
	analysis.graph = control_flow_graph.Build(analysis.root, control_flow_graph.Hooks[concurrencyNoLostUpdateEvent]{
		Expression: func(graphBuilder *builder, node *ast.Node) {
			switch node.Kind {
			case ast.KindAwaitExpression, ast.KindYieldExpression:
				graphBuilder.Emit(concurrencyNoLostUpdateEvent{kind: concurrencyNoLostUpdateSuspend, node: node})
			case ast.KindBinaryExpression:
				if ast.IsAssignmentOperator(node.AsBinaryExpression().OperatorToken.Kind) {
					graphBuilder.Emit(concurrencyNoLostUpdateEvent{kind: concurrencyNoLostUpdateAnchor, node: node})
				}
			case ast.KindCallExpression:
				if access := analysis.storeAccessOf(node); access != nil && access.isWrite {
					graphBuilder.Emit(concurrencyNoLostUpdateEvent{kind: concurrencyNoLostUpdateAnchor, node: node})
				}
			}
		},
		Statement: func(graphBuilder *builder, node *ast.Node) {
			// The first step of a `for await` suspends before the body runs. Recorded with no node, so
			// no anchor is exempted for being inside it.
			//
			// The step between iterations is not recorded, and a mutant recording it survived every
			// fixture for a reason rather than for want of one: the only const a path can carry across
			// the back edge is one declared before the loop, and every path from that declaration into
			// the body already crosses this first step. A const declared in the body is declared again
			// before anything in the next iteration can read it.
			if isForAwait(node) {
				graphBuilder.Emit(concurrencyNoLostUpdateEvent{kind: concurrencyNoLostUpdateSuspend})
			}
		},
		Read: func(graphBuilder *builder, node *ast.Node) {
			if symbol := declarationSymbol(node); symbol != nil {
				graphBuilder.Emit(concurrencyNoLostUpdateEvent{
					kind: concurrencyNoLostUpdateDeclarationStart, node: node, symbol: symbol,
				})
			}
		},
		Write: func(graphBuilder *builder, node *ast.Node) {
			if symbol := declarationSymbol(node); symbol != nil {
				graphBuilder.Emit(concurrencyNoLostUpdateEvent{
					kind: concurrencyNoLostUpdateDeclarationEnd, node: node, symbol: symbol,
				})
			}
		},
	})
	return analysis.graph
}

// storeAccessOf reads a call as a read or a write of one store entry, or returns nil.
//
// A store is a pair of calls the syntax ties together and nothing else: the same noun after a read verb
// and a write verb (`getAccountCredentials` and `updateAccountCredentials`, `loadSettings` and
// `saveSettings`, `cache.get` and `cache.set`), the same entry named by the same leading arguments, and
// the written value as the write's one extra, last argument. Three things keep the pair from being a
// coincidence of names:
//
//   - free functions must both resolve, through imports, to declarations in the same file, and their
//     noun must not be empty (a bare `get` and `set` from one module say nothing about a shared entry);
//   - methods must be called on the same static receiver path, the object that holds the entry;
//   - every key argument is a literal or a binding that holds still for the whole function (a const,
//     an import, or a parameter or local nothing in the function writes), so two calls naming `key`
//     name the same entry. Anything else (a member read, a call, a spread) is not judged.
//
// This is the Reddit token refresh of 2026-10-01: `modules/reddit/RedditClient.ts` reads the account
// through `getAccountCredentials(accountKey)`, awaits the token endpoint, and writes `{ oauth2: {
// ...account.oauth2, ...newTokens } }` back through `updateAccountCredentials(accountKey, ...)`, so a
// concurrent refresh of the other token is overwritten with the copy read before the await. No `=`
// is involved, which is why the assignment half of this rule could not see it.
func (analysis *concurrencyNoLostUpdateRoot) storeAccessOf(call *ast.Node) *concurrencyNoLostUpdateStoreAccess {
	if access, cached := analysis.storeAccesses[call]; cached {
		return access
	}
	access := analysis.computeStoreAccess(call)
	analysis.storeAccesses[call] = access
	return access
}

func (analysis *concurrencyNoLostUpdateRoot) computeStoreAccess(call *ast.Node) *concurrencyNoLostUpdateStoreAccess {
	if call.Kind != ast.KindCallExpression {
		return nil
	}
	expression := call.AsCallExpression()
	callee := concurrencyNoLostUpdateSkipTransparent(expression.Expression)
	if callee == nil {
		return nil
	}

	var name string
	var target concurrencyNoLostUpdatePath
	var owner string
	switch callee.Kind {
	case ast.KindIdentifier:
		name = callee.Text()
		symbol := analysis.resolve(callee)
		if symbol == nil {
			return nil
		}
		symbol = shimchecker.SkipAlias(symbol, analysis.ruleContext.TypeChecker)
		declaration := symbol.ValueDeclaration
		if declaration == nil && len(symbol.Declarations) > 0 {
			declaration = symbol.Declarations[0]
		}
		if declaration == nil {
			return nil
		}
		sourceFile := ast.GetSourceFileOfNode(declaration)
		if sourceFile == nil {
			return nil
		}
		owner = ("function\x00" + sourceFile.FileName()).AsString()
	case ast.KindPropertyAccessExpression:
		access := callee.AsPropertyAccessExpression()
		nameNode := access.Name()
		if nameNode == nil || nameNode.Kind != ast.KindIdentifier {
			return nil
		}
		name = nameNode.Text()
		receiver, isPath := analysis.pathOf(access.Expression)
		if !isPath {
			return nil
		}
		target = receiver
		owner = "method"
	default:
		return nil
	}

	noun, isRead := concurrencyNoLostUpdateSplitVerb(name, concurrencyNoLostUpdateReadVerbs)
	isWrite := false
	if !isRead {
		noun, isWrite = concurrencyNoLostUpdateSplitVerb(name, concurrencyNoLostUpdateWriteVerbs)
		if !isWrite {
			return nil
		}
	}
	if noun == "" && owner != "method" {
		return nil
	}

	var arguments []*ast.Node
	if expression.Arguments != nil {
		arguments = expression.Arguments.Nodes
	}
	keyArguments := arguments
	var value *ast.Node
	if isWrite {
		if len(arguments) == 0 {
			return nil
		}
		keyArguments = arguments[:len(arguments)-1]
		value = arguments[len(arguments)-1]
		if value.Kind == ast.KindSpreadElement {
			return nil
		}
	}
	keys := make([]string, 0, len(keyArguments))
	for _, argument := range keyArguments {
		key, isKey := analysis.storeKeyOf(argument)
		if !isKey {
			return nil
		}
		keys = append(keys, key)
	}

	target.store = owner + "\x00" + noun + "\x00" + strings.Join(keys, "\x00")
	return &concurrencyNoLostUpdateStoreAccess{target: target, isWrite: isWrite, value: value}
}

// storeKeyOf canonicalizes one key argument: a literal by its text, a binding that holds still by its
// symbol.
func (analysis *concurrencyNoLostUpdateRoot) storeKeyOf(argument *ast.Node) (string, bool) {
	unwrapped := concurrencyNoLostUpdateSkipTransparent(argument)
	if unwrapped == nil {
		return "", false
	}
	switch {
	case ast.IsStringLiteralLike(unwrapped):
		return "string\x00" + unwrapped.Text(), true
	case ast.IsNumericLiteral(unwrapped):
		return "number\x00" + unwrapped.Text(), true
	case unwrapped.Kind == ast.KindIdentifier:
		symbol := analysis.resolve(unwrapped)
		if symbol == nil || !analysis.bindingHoldsStill(symbol) {
			return "", false
		}
		return fmt.Sprintf("binding\x00%p", symbol), true
	}
	return "", false
}

// bindingHoldsStill reports whether a binding names one value for the whole of this function: a
// const or an import, or a parameter or local of this function that nothing in it, nested functions
// included, writes.
func (analysis *concurrencyNoLostUpdateRoot) bindingHoldsStill(symbol *ast.Symbol) bool {
	if holds, cached := analysis.stableBindings[symbol]; cached {
		return holds
	}
	holds := false
	declaration := symbol.ValueDeclaration
	if declaration == nil && len(symbol.Declarations) > 0 {
		declaration = symbol.Declarations[0]
	}
	switch {
	case symbol.Flags&ast.SymbolFlagsAlias != 0:
		holds = true
	case declaration == nil:
	case declaration.Kind == ast.KindVariableDeclaration && ast.IsVarConst(declaration):
		holds = true
	case (declaration.Kind == ast.KindParameter || declaration.Kind == ast.KindVariableDeclaration) &&
		concurrencyNoLostUpdateWithin(declaration, analysis.root):
		holds = true
		var visit func(node *ast.Node) bool
		visit = func(node *ast.Node) bool {
			if !holds {
				return true
			}
			if node.Kind == ast.KindIdentifier && node != declaration.Name() && analysis.resolve(node) == symbol &&
				reference.WritesToBinding(node) {
				holds = false
				return true
			}
			node.ForEachChild(visit)
			return !holds
		}
		analysis.root.ForEachChild(visit)
	}
	analysis.stableBindings[symbol] = holds
	return holds
}

// storeReadText is the source of the first read of the target entry before the write, for the
// message.
func (analysis *concurrencyNoLostUpdateRoot) storeReadText(target concurrencyNoLostUpdatePath, write *ast.Node) string {
	var read *ast.Node
	evaluator := concurrencyNoLostUpdateEvaluator{analysis: analysis, target: target}
	analysis.forEachOwnNode(func(node *ast.Node) {
		if read == nil && node.Pos() < write.End() && evaluator.isTargetStoreRead(node) {
			read = node
		}
	})
	if read == nil {
		read = write.AsCallExpression().Expression
	}
	textRange := rule.TokenRange(analysis.ruleContext.SourceFile, read)
	return analysis.ruleContext.SourceFile.Text()[textRange.Pos():textRange.End()]
}
