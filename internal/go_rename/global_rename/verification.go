package global_rename

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/rename"

	"golang.org/x/tools/go/packages"
)

// verification holds what every pass of the fixpoint reads, collected once. Every collection is filtered to
// names a pass can care about: a name some candidate has now, or a name some candidate asks for. The accepted
// set only shrinks, so a filter made with the first pass's names holds for every later one.
type verification struct {
	engine    *engine
	firstPass bool

	occurrences   []lexicalOccurrence
	scopedObjects map[*packages.Package][]scopedObject
	ordered       map[*types.Scope]bool

	labelGroups [][]types.Object
	selections  []selectionSite

	namedTypes   []*types.Named
	structs      []*types.Struct
	interfaces   []*types.Interface
	structGroups [][]*types.Struct

	universes []universe
}

// lexicalOccurrence is one identifier resolved through scopes, in the variant that resolved it.
type lexicalOccurrence struct {
	goPackage  *packages.Package
	identifier *ast.Ident
	object     types.Object
	defining   bool
}

type scopedObject struct {
	scope  *types.Scope
	object types.Object
}

// selectionSite is one selector go/types resolved through a type's fields and methods.
type selectionSite struct {
	goPackage *packages.Package
	selector  *ast.SelectorExpr
	selection *types.Selection
}

func newVerification(engine *engine) *verification {
	verification := &verification{
		engine:        engine,
		firstPass:     true,
		scopedObjects: map[*packages.Package][]scopedObject{},
		ordered:       map[*types.Scope]bool{},
	}
	interesting := map[string]bool{}
	for key, newName := range engine.accepted {
		interesting[newName] = true
		interesting[engine.candidates[key].OldName] = true
	}
	isInteresting := func(object types.Object) bool {
		if interesting[object.Name()] {
			return true
		}
		_, proposed := engine.accepted[engine.keyer.key(object)]
		return proposed
	}
	verification.collectLexical(isInteresting)
	verification.collectLabels()
	verification.collectSelections(isInteresting)
	verification.collectDeclarations()
	verification.collectUniverses(interesting)
	return verification
}

// pass runs every check once against the accepted set and returns the renames found in conflicts.
func (verification *verification) pass() map[Key]drop {
	engine := verification.engine
	drops := map[Key]drop{}
	add := func(object types.Object, class string, reason string) {
		key := engine.keyer.key(object)
		if _, open := engine.accepted[key]; !open {
			return
		}
		if _, recorded := drops[key]; !recorded {
			drops[key] = drop{class: class, reason: reason}
		}
	}
	relevant := map[string]bool{}
	for _, newName := range engine.accepted {
		relevant[newName] = true
	}
	verification.checkLexical(relevant, add)
	verification.checkLabels(add)
	verification.checkSelections(relevant, add)
	verification.checkDeclarations(add)
	verification.checkStructGroups(add)
	verification.checkSatisfaction(add)
	verification.checkImports(add)
	verification.checkEditConflicts(drops)
	verification.firstPass = false
	return drops
}

type addDrop func(object types.Object, class string, reason string)

// collectLexical gathers, from every file of every root package, the identifiers resolved through scopes:
// everything in Defs and Uses except selector names, fields and methods (they resolve through types), labels,
// blanks, dot imports, init functions and the nil entries.
func (verification *verification) collectLexical(isInteresting func(types.Object) bool) {
	for _, rootsPackage := range verification.engine.roots {
		selectorNames := map[*ast.Ident]bool{}
		for _, file := range rootsPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				if selector, isSelector := node.(*ast.SelectorExpr); isSelector {
					selectorNames[selector.Sel] = true
				}
				return true
			})
		}
		add := func(identifier *ast.Ident, object types.Object, defining bool) {
			if object == nil ||
				selectorNames[identifier] ||
				identifier.Name == "_" ||
				!resolvesLexically(object, identifier) {
				return
			}
			if !isInteresting(object) {
				return
			}
			verification.occurrences = append(
				verification.occurrences,
				lexicalOccurrence{goPackage: rootsPackage, identifier: identifier, object: object, defining: defining},
			)
		}
		for identifier, object := range rootsPackage.TypesInfo.Defs {
			add(identifier, object, true)
		}
		for identifier, object := range rootsPackage.TypesInfo.Uses {
			add(identifier, object, false)
		}
		for _, entry := range allScopes(rootsPackage.Types.Scope()) {
			verification.ordered[entry.scope] = entry.ordered
			for _, name := range entry.scope.Names() {
				object := entry.scope.Lookup(name)
				if isInteresting(object) {
					verification.scopedObjects[rootsPackage] = append(
						verification.scopedObjects[rootsPackage],
						scopedObject{scope: entry.scope, object: object},
					)
				}
			}
		}
	}
	sort.SliceStable(verification.occurrences, func(left, right int) bool {
		leftOccurrence, rightOccurrence := verification.occurrences[left], verification.occurrences[right]
		if leftOccurrence.goPackage.ID != rightOccurrence.goPackage.ID {
			return leftOccurrence.goPackage.ID < rightOccurrence.goPackage.ID
		}
		if leftOccurrence.identifier.Pos() != rightOccurrence.identifier.Pos() {
			return leftOccurrence.identifier.Pos() < rightOccurrence.identifier.Pos()
		}
		return leftOccurrence.defining && !rightOccurrence.defining
	})
}

// resolvesLexically reports whether an identifier naming object is resolved through scopes.
func resolvesLexically(object types.Object, identifier *ast.Ident) bool {
	switch typed := object.(type) {
	case *types.Var:
		return !typed.IsField()
	case *types.Func:
		return typed.Signature().Recv() == nil && typed.Name() != "init"
	case *types.Label:
		return false
	case *types.PkgName:
		return identifier.Name != "."
	}
	return true
}

// scopeEntry is one scope and whether it keeps declaration order, as in the rename package.
type scopeEntry struct {
	scope   *types.Scope
	ordered bool
}

func allScopes(packageScope *types.Scope) []scopeEntry {
	entries := []scopeEntry{{scope: types.Universe}}
	var visit func(scope *types.Scope, ordered bool)
	visit = func(scope *types.Scope, ordered bool) {
		entries = append(entries, scopeEntry{scope: scope, ordered: ordered})
		for child := range scope.Children() {
			visit(child, scope != packageScope)
		}
	}
	visit(packageScope, false)
	return entries
}

// checkLexical re-resolves every occurrence whose final name some rename involves, under final names, and
// requires the first match to be its own object. It is the rename package's check, with objects compared within
// one variant and final names read by key, so one rename covers every copy of its declaration. A type read before
// the scope of a local of its final name opens is no capture (rename.TypeReadBeforeLocalScope).
func (verification *verification) checkLexical(relevant map[string]bool, add addDrop) {
	engine := verification.engine
	byPackage := map[*packages.Package]map[*types.Scope]map[string][]types.Object{}
	for goPackage, entries := range verification.scopedObjects {
		index := map[*types.Scope]map[string][]types.Object{}
		for _, entry := range entries {
			name := engine.finalName(entry.object)
			if !relevant[name] {
				continue
			}
			if index[entry.scope] == nil {
				index[entry.scope] = map[string][]types.Object{}
			}
			index[entry.scope][name] = append(index[entry.scope][name], entry.object)
		}
		byPackage[goPackage] = index
	}
	var unresolved []string
	for _, occurrence := range verification.occurrences {
		target := occurrence.object
		name := engine.finalName(target)
		targetRenamed := engine.renamed(target)
		if !targetRenamed && !relevant[name] {
			continue
		}
		index := byPackage[occurrence.goPackage]
		position := occurrence.identifier.Pos()
		start := occurrence.goPackage.Types.Scope().Innermost(position)
		if occurrence.defining && target.Parent() != nil {
			start = target.Parent()
		}
		reached := false
		conflicted := false
		for scope := start; scope != nil; scope = scope.Parent() {
			var found []types.Object
			for _, object := range index[scope][name] {
				if verification.ordered[scope] && rename.TypeReadBeforeLocalScope(target, object, position) {
					continue
				}
				if !verification.ordered[scope] || object.Pos() <= position || isTypeParameter(object) {
					found = append(found, object)
				}
			}
			if len(found) == 0 {
				continue
			}
			var others []types.Object
			containsTarget := false
			othersRenamed := false
			for _, object := range found {
				if object == target {
					containsTarget = true
					continue
				}
				others = append(others, object)
				if engine.renamed(object) {
					othersRenamed = true
				}
			}
			if len(others) == 0 {
				reached = true
				break
			}
			if !targetRenamed && !othersRenamed {
				// Every object here keeps its name and go/types resolved past them, so declaration order
				// (approximated by position) hides them here.
				if containsTarget {
					reached = true
					break
				}
				continue
			}
			where := occurrence.goPackage.Fset.Position(position)
			for _, other := range others {
				reason := fmt.Sprintf("%q at %s:%d would resolve to %s %q declared at %s instead of %s %q",
					occurrence.identifier.Name, where.Filename, where.Line, kindOf(other), engine.finalName(other),
					occurrence.goPackage.Fset.Position(other.Pos()), kindOf(target), target.Name())
				add(other, "conflict: scope", reason)
				if targetRenamed {
					add(target, "conflict: scope", reason)
				}
			}
			conflicted = true
			reached = containsTarget
			break
		}
		if !reached && !conflicted && targetRenamed {
			unresolved = append(unresolved, fmt.Sprintf(
				"%s: %s (%s) not reached by the scope walk in %s",
				occurrence.goPackage.Fset.Position(position),
				occurrence.identifier.Name,
				kindOf(target),
				occurrence.goPackage.ID,
			))
			add(target, "model: scope walk", "an occurrence the scope walk never reached")
		}
	}
	if verification.firstPass {
		verification.engine.result.Unresolved = append(verification.engine.result.Unresolved, unresolved...)
	}
}

func isTypeParameter(object types.Object) bool {
	typeName, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		return false
	}
	_, isParameter := typeName.Type().(*types.TypeParam)
	return isParameter
}

// collectLabels groups every label by the function body that holds it: a label's scope is its function, not
// any block, and a function literal has labels of its own.
func (verification *verification) collectLabels() {
	for _, rootsPackage := range verification.engine.roots {
		for _, file := range rootsPackage.Syntax {
			var stack []ast.Node
			groups := map[ast.Node][]types.Object{}
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, node)
				labeled, isLabeled := node.(*ast.LabeledStmt)
				if !isLabeled {
					return true
				}
				label, isLabel := rootsPackage.TypesInfo.Defs[labeled.Label].(*types.Label)
				if !isLabel {
					return true
				}
				for index := len(stack) - 1; index >= 0; index-- {
					switch stack[index].(type) {
					case *ast.FuncDecl, *ast.FuncLit:
						groups[stack[index]] = append(groups[stack[index]], label)
						return true
					}
				}
				return true
			})
			for _, group := range groups {
				verification.labelGroups = append(verification.labelGroups, group)
			}
		}
	}
}

// checkLabels requires the labels of one function to keep distinct final names.
func (verification *verification) checkLabels(add addDrop) {
	engine := verification.engine
	for _, group := range verification.labelGroups {
		byName := map[string][]types.Object{}
		for _, label := range group {
			byName[engine.finalName(label)] = append(byName[engine.finalName(label)], label)
		}
		for name, labels := range byName {
			if len(labels) < 2 {
				continue
			}
			for _, label := range labels {
				add(label, "conflict: label", fmt.Sprintf("two labels of one function would both be named %q", name))
			}
		}
	}
}

// collectSelections gathers every selector whose object is a candidate or whose name some candidate asks
// for, and checks the member lookup against go/types under the old names: a selection the model resolves
// differently from go/types is a model gap, and every rename it touches is refused.
func (verification *verification) collectSelections(isInteresting func(types.Object) bool) {
	engine := verification.engine
	for _, rootsPackage := range engine.roots {
		for selector, selection := range rootsPackage.TypesInfo.Selections {
			if !isInteresting(selection.Obj()) {
				continue
			}
			verification.selections = append(
				verification.selections,
				selectionSite{goPackage: rootsPackage, selector: selector, selection: selection},
			)
			found := lookupMember(selection.Recv(), true, rootsPackage.Types, selection.Obj().Name(), actualName)
			if found.object == nil || engine.keyer.key(found.object) != engine.keyer.key(selection.Obj()) {
				description := fmt.Sprintf(
					"%s: %s in %s resolves to %s by go/types, the model found %v",
					rootsPackage.Fset.Position(selector.Sel.Pos()),
					selector.Sel.Name,
					rootsPackage.ID,
					engine.keyer.key(selection.Obj()),
					found.object,
				)
				engine.result.SelectionModelMismatches = append(engine.result.SelectionModelMismatches, description)
				engine.reject(engine.keyer.key(selection.Obj()), "model: selection", description)
			}
		}
	}
}

// checkSelections re-resolves every selector under final names: a renamed field or method must still be what
// its selectors find, and a selector of an unrenamed member must not now find a renamed one (a collision at
// its depth, or a shallower shadow through embedding).
func (verification *verification) checkSelections(relevant map[string]bool, add addDrop) {
	engine := verification.engine
	for _, entry := range verification.selections {
		target := entry.selection.Obj()
		name := engine.finalName(target)
		if !engine.renamed(target) && !relevant[name] {
			continue
		}
		found := lookupMember(entry.selection.Recv(), true, entry.goPackage.Types, name, engine.finalName)
		if found.object != nil && engine.keyer.key(found.object) == engine.keyer.key(target) {
			continue
		}
		where := entry.goPackage.Fset.Position(entry.selector.Sel.Pos())
		reason := fmt.Sprintf(
			"selector .%s at %s:%d would no longer select %s %s",
			entry.selector.Sel.Name,
			where.Filename,
			where.Line,
			kindOf(target),
			target.Name(),
		)
		add(target, "conflict: selector", reason)
		if found.object != nil {
			add(found.object, "conflict: selector", reason)
		}
		for _, collision := range found.collisions {
			add(collision, "conflict: selector", reason)
		}
	}
}

// collectDeclarations gathers every named type, struct type and interface type the root packages declare or
// spell, for the checks that their member names stay distinct.
func (verification *verification) collectDeclarations() {
	engine := verification.engine
	seenNamed := map[Key]bool{}
	seenStruct := map[*types.Struct]bool{}
	seenInterface := map[*types.Interface]bool{}
	for _, rootsPackage := range engine.roots {
		for _, object := range rootsPackage.TypesInfo.Defs {
			typeName, isTypeName := object.(*types.TypeName)
			if !isTypeName || typeName.IsAlias() {
				continue
			}
			named, isNamed := typeName.Type().(*types.Named)
			if !isNamed {
				continue
			}
			key := engine.keyer.key(typeName)
			if seenNamed[key] {
				continue
			}
			seenNamed[key] = true
			verification.namedTypes = append(verification.namedTypes, named.Origin())
		}
		for _, value := range rootsPackage.TypesInfo.Types {
			if !value.IsType() {
				continue
			}
			switch typed := value.Type.(type) {
			case *types.Struct:
				if !seenStruct[typed] {
					seenStruct[typed] = true
					verification.structs = append(verification.structs, typed)
				}
			case *types.Interface:
				if !seenInterface[typed] {
					seenInterface[typed] = true
					verification.interfaces = append(verification.interfaces, typed)
				}
			}
		}
	}
	verification.groupStructs()
}

// checkDeclarations requires a struct's fields, a named type's methods and depth-zero fields, and an
// interface's methods to keep distinct final names.
func (verification *verification) checkDeclarations(add addDrop) {
	engine := verification.engine
	report := func(objects []types.Object, what string) {
		byName := map[string][]types.Object{}
		for _, object := range objects {
			if object.Name() == "_" {
				continue
			}
			byName[engine.finalName(object)] = append(byName[engine.finalName(object)], object)
		}
		for name, group := range byName {
			distinct := map[Key]bool{}
			anyRenamed := false
			for _, object := range group {
				distinct[engine.keyer.key(object)] = true
				if engine.renamed(object) {
					anyRenamed = true
				}
			}
			if len(distinct) < 2 || !anyRenamed {
				continue
			}
			for _, object := range group {
				add(object, "conflict: duplicate member", fmt.Sprintf("%s would hold two members named %q", what, name))
			}
		}
	}
	for _, structType := range verification.structs {
		var fields []types.Object
		for index := range structType.NumFields() {
			fields = append(fields, structType.Field(index))
		}
		report(fields, "a struct")
	}
	for _, named := range verification.namedTypes {
		var members []types.Object
		for index := range named.NumMethods() {
			members = append(members, named.Method(index))
		}
		if structType, isStruct := named.Underlying().(*types.Struct); isStruct {
			for index := range structType.NumFields() {
				members = append(members, structType.Field(index))
			}
		}
		report(members, "type "+named.Obj().Name())
	}
	for _, interfaceType := range verification.interfaces {
		var methods []types.Object
		for index := range interfaceType.NumMethods() {
			methods = append(methods, interfaceType.Method(index))
		}
		report(methods, "an interface")
	}
}

// groupStructs groups struct types that spell the same fields of the same types in the same order. Two such
// types are identical, or convertible when named, only while their field names agree, so a rename must move
// every one of them or none.
func (verification *verification) groupStructs() {
	engine := verification.engine
	bySignature := map[string][]*types.Struct{}
	qualifier := func(goPackage *types.Package) string { return goPackage.Path() }
	for _, structType := range verification.structs {
		if structType.NumFields() == 0 {
			continue
		}
		var builder strings.Builder
		for index := range structType.NumFields() {
			field := structType.Field(index)
			fmt.Fprintf(
				&builder,
				"%s %t %s;",
				field.Name(),
				field.Embedded(),
				types.TypeString(field.Type(), qualifier),
			)
		}
		bySignature[builder.String()] = append(bySignature[builder.String()], structType)
	}
	for _, group := range bySignature {
		declarations := map[Key]bool{}
		hasCandidate := false
		for _, structType := range group {
			declarations[engine.keyer.key(structType.Field(0))] = true
			for index := range structType.NumFields() {
				if _, proposed := engine.candidates[engine.keyer.key(structType.Field(index))]; proposed {
					hasCandidate = true
				}
			}
		}
		if len(declarations) > 1 && hasCandidate {
			verification.structGroups = append(verification.structGroups, group)
		}
	}
}

// checkStructGroups requires the struct types of a group to agree on every field's final name.
func (verification *verification) checkStructGroups(add addDrop) {
	engine := verification.engine
	for _, group := range verification.structGroups {
		for index := range group[0].NumFields() {
			names := map[string]bool{}
			for _, structType := range group {
				names[engine.finalName(structType.Field(index))] = true
			}
			if len(names) < 2 {
				continue
			}
			for _, structType := range group {
				field := structType.Field(index)
				add(field, "conflict: struct identity", fmt.Sprintf(
					"%d struct types spell the same fields; renaming field %q in only some would make them differ (identity, conversion)",
					len(group), field.Name()))
			}
		}
	}
}

// interfaceEntry is one interface a universe can see.
type interfaceEntry struct {
	identity      string
	name          string
	interfaceType *types.Interface
	generic       bool
	home          string
}

// typeEntry is one workspace type a universe can see.
type typeEntry struct {
	key    Key
	name   string
	goType types.Type
}

// universe is what one root package sees: the workspace types and interfaces in its import closure (itself
// included) that hold a name some rename involves. Types and interfaces are compared only within one
// universe, since go/types considers a type from one variant distinct from its twin in another.
type universe struct {
	goPackage  *packages.Package
	types      []typeEntry
	interfaces []interfaceEntry
}

// collectUniverses builds each root's universe, keeping only what can hold an interesting member name.
func (verification *verification) collectUniverses(interesting map[string]bool) {
	engine := verification.engine
	memberInteresting := map[Key]bool{}
	memberNamesComputed := map[Key]bool{}
	initialFinal := engine.finalName
	typeIsInteresting := func(key Key, goType types.Type) bool {
		if memberNamesComputed[key] {
			return memberInteresting[key]
		}
		names := map[string]bool{}
		memberNames(goType, actualName, names)
		memberNames(goType, initialFinal, names)
		result := false
		for name := range names {
			if interesting[name] {
				result = true
				break
			}
		}
		memberNamesComputed[key] = true
		memberInteresting[key] = result
		return result
	}
	interfaceIsInteresting := func(interfaceType *types.Interface) bool {
		for index := range interfaceType.NumMethods() {
			method := interfaceType.Method(index)
			if interesting[method.Name()] || interesting[initialFinal(method)] {
				return true
			}
		}
		return false
	}
	// error is the universe's interface, so the assertion holds; were it not, there would be none to hold.
	errorInterface, isErrorInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	// A value travels as an interface into packages its type's package never imports, and a type assertion
	// there against a local or literal interface sees its methods, so every universe holds all of them.
	localInterfaces := engine.localInterfaces(interfaceIsInteresting)
	for _, rootsPackage := range engine.roots {
		current := universe{goPackage: rootsPackage}
		if isErrorInterface && interfaceIsInteresting(errorInterface) {
			current.interfaces = append(
				current.interfaces,
				interfaceEntry{identity: "error", name: "error", interfaceType: errorInterface},
			)
		}
		current.interfaces = append(current.interfaces, localInterfaces...)
		for _, visible := range importClosure(rootsPackage.Types) {
			scope := visible.Scope()
			workspacePackage := engine.workspacePaths[visible.Path()]
			for _, name := range scope.Names() {
				typeName, isTypeName := scope.Lookup(name).(*types.TypeName)
				if !isTypeName || typeName.IsAlias() {
					continue
				}
				if interfaceType, isInterface := typeName.Type().Underlying().(*types.Interface); isInterface &&
					interfaceIsInteresting(interfaceType) {
					named, _ := typeName.Type().(*types.Named)
					current.interfaces = append(current.interfaces, interfaceEntry{
						identity:      engine.keyer.key(typeName).String(),
						name:          visible.Path() + "." + name,
						interfaceType: interfaceType,
						generic:       named != nil && named.TypeParams().Len() > 0,
						home:          visible.Path(),
					})
				}
				if workspacePackage && typeIsInteresting(engine.keyer.key(typeName), typeName.Type()) {
					current.types = append(
						current.types,
						typeEntry{key: engine.keyer.key(typeName), name: name, goType: typeName.Type()},
					)
				}
			}
		}
		// The package's own local named types.
		for identifier, object := range rootsPackage.TypesInfo.Defs {
			typeName, isTypeName := object.(*types.TypeName)
			if !isTypeName ||
				typeName.IsAlias() ||
				typeName.Parent() == rootsPackage.Types.Scope() ||
				typeName.Parent() == nil {
				continue
			}
			if _, isParameter := typeName.Type().(*types.TypeParam); isParameter {
				continue
			}
			key := engine.keyer.key(typeName)
			if typeIsInteresting(key, typeName.Type()) {
				current.types = append(
					current.types,
					typeEntry{key: key, name: identifier.Name, goType: typeName.Type()},
				)
			}
		}
		if len(current.types) > 0 && len(current.interfaces) > 0 {
			verification.universes = append(verification.universes, current)
		}
	}
}

// importClosure is goPackage and every package it imports, directly or not.
func importClosure(goPackage *types.Package) []*types.Package {
	visited := map[*types.Package]bool{goPackage: true}
	result := []*types.Package{goPackage}
	for index := 0; index < len(result); index++ {
		for _, imported := range result[index].Imports() {
			if !visited[imported] {
				visited[imported] = true
				result = append(result, imported)
			}
		}
	}
	return result
}

// checkSatisfaction requires every workspace type, and a pointer to it, to satisfy exactly the interfaces it
// satisfied before, judged by method names and signatures under old names and under final names, and to
// satisfy each through the same methods: a type that overrides a method of an interface it embeds would
// otherwise keep satisfying the interface through the embedded method once the interface's is renamed alone,
// and every call through the interface would stop reaching the override.
func (verification *verification) checkSatisfaction(add addDrop) {
	engine := verification.engine
	evaluated := map[string]bool{}
	for _, current := range verification.universes {
		for _, typeEntry := range current.types {
			implementers := []types.Type{typeEntry.goType}
			if !types.IsInterface(typeEntry.goType) {
				implementers = append(implementers, types.NewPointer(typeEntry.goType))
			}
			for _, interfaceEntry := range current.interfaces {
				pair := typeEntry.key.String() + " | " + interfaceEntry.identity
				if evaluated[pair] {
					continue
				}
				evaluated[pair] = true
				for _, implementer := range implementers {
					test := satisfies
					if interfaceEntry.generic {
						test = satisfiesInShape
					}
					before := test(implementer, interfaceEntry.interfaceType, actualName)
					after := test(implementer, interfaceEntry.interfaceType, engine.finalName)
					involved := verification.involvedMembers(implementer, interfaceEntry.interfaceType)
					if verification.firstPass &&
						!interfaceEntry.generic &&
						interfaceEntry.interfaceType.IsMethodSet() &&
						!isGeneric(typeEntry.goType) {
						if truth := types.Implements(implementer, interfaceEntry.interfaceType); truth != before {
							description := fmt.Sprintf(
								"%s satisfying %s: go/types says %t, the model says %t",
								implementer,
								interfaceEntry.name,
								truth,
								before,
							)
							engine.result.SatisfactionModelMismatches = append(
								engine.result.SatisfactionModelMismatches,
								description,
							)
							for _, object := range involved {
								add(object, "model: satisfaction", description)
							}
						}
					}
					if before && after && !interfaceEntry.generic && !isGeneric(typeEntry.goType) {
						if method, beforeMember, afterMember := verification.changedImplementation(
							implementer,
							interfaceEntry.interfaceType,
						); method != nil {
							reason := fmt.Sprintf(
								"%s would satisfy %s.%s through %s, no longer %s",
								implementer,
								interfaceEntry.name,
								method.Name(),
								describeMember(afterMember),
								describeMember(beforeMember),
							)
							for _, object := range involved {
								add(object, "conflict: interface satisfaction", reason)
							}
							continue
						}
					}
					if before == after {
						continue
					}
					verb := "stop"
					if after {
						verb = "start"
					}
					reason := fmt.Sprintf("%s would %s satisfying %s", implementer, verb, interfaceEntry.name)
					for _, object := range involved {
						add(object, "conflict: interface satisfaction", reason)
					}
				}
			}
		}
	}
}

// changedImplementation is the first method of interfaceType that implementer satisfies through one member
// under old names and another under final names, with both members, or nils when every method keeps its own.
func (verification *verification) changedImplementation(
	implementer types.Type,
	interfaceType *types.Interface,
) (*types.Func, types.Object, types.Object) {
	engine := verification.engine
	for index := range interfaceType.NumMethods() {
		wanted := interfaceType.Method(index)
		before := lookupMember(implementer, false, wanted.Pkg(), wanted.Name(), actualName).object
		after := lookupMember(implementer, false, wanted.Pkg(), engine.finalName(wanted), engine.finalName).object
		if before == nil || after == nil || engine.keyer.key(before) == engine.keyer.key(after) {
			continue
		}
		return wanted, before, after
	}
	return nil, nil, nil
}

// describeMember names a method by its receiver and name for a reason.
func describeMember(object types.Object) string {
	if function, isFunction := object.(*types.Func); isFunction && function.Signature().Recv() != nil {
		return fmt.Sprintf("(%s).%s", function.Signature().Recv().Type(), function.Name())
	}
	return object.Name()
}

func isGeneric(goType types.Type) bool {
	named, isNamed := types.Unalias(goType).(*types.Named)
	return isNamed && named.TypeParams().Len() > 0 && named.TypeArgs().Len() == 0
}

// involvedMembers lists the interface's methods and every member of implementer that a lookup of one of
// their names finds, under old or final names: the renames a change in satisfaction can be blamed on.
func (verification *verification) involvedMembers(
	implementer types.Type,
	interfaceType *types.Interface,
) []types.Object {
	engine := verification.engine
	var involved []types.Object
	for index := range interfaceType.NumMethods() {
		method := interfaceType.Method(index)
		involved = append(involved, method)
		for _, name := range []string{method.Name(), engine.finalName(method)} {
			for _, namer := range []nameOf{actualName, engine.finalName} {
				found := lookupMember(implementer, true, method.Pkg(), name, namer)
				if found.object != nil {
					involved = append(involved, found.object)
				}
				involved = append(involved, found.collisions...)
			}
		}
	}
	return involved
}

// checkImports refuses a package-level rename onto a name some file of the package imports: a name may not
// be declared in both the file block and the package block.
func (verification *verification) checkImports(add addDrop) {
	engine := verification.engine
	for _, rootsPackage := range engine.roots {
		packageScope := rootsPackage.Types.Scope()
		for _, name := range packageScope.Names() {
			object := packageScope.Lookup(name)
			if !engine.renamed(object) {
				continue
			}
			final := engine.finalName(object)
			for fileScope := range packageScope.Children() {
				if imported := fileScope.Lookup(final); imported != nil {
					add(
						object,
						"conflict: import",
						fmt.Sprintf("a file of %s imports %s as %q", rootsPackage.ID, kindOf(imported), final),
					)
				}
			}
		}
	}
}

// checkEditConflicts refuses two renames that would write different text at one identifier, as an embedded
// field and its type could if they were proposed apart.
//
// Mutates: drops the verification's table of refused renames, which each check adds to
func (verification *verification) checkEditConflicts(drops map[Key]drop) {
	engine := verification.engine
	written := map[rename.EditKey]map[string][]Key{}
	for key, newName := range engine.accepted {
		for _, site := range engine.sites[key] {
			editKey := rename.EditKey{Filename: site.filename, Offset: site.offset}
			if written[editKey] == nil {
				written[editKey] = map[string][]Key{}
			}
			written[editKey][newName] = append(written[editKey][newName], key)
		}
	}
	for editKey, byName := range written {
		if len(byName) < 2 {
			continue
		}
		for _, keys := range byName {
			for _, key := range keys {
				if _, recorded := drops[key]; !recorded {
					drops[key] = drop{class: "conflict: edit", reason: fmt.Sprintf(
						"two renames would write different names at %s offset %d",
						editKey.Filename,
						editKey.Offset,
					)}
				}
			}
		}
	}
}

// positionString prints a position without the column.
func positionString(position token.Position) string {
	return fmt.Sprintf("%s:%d", position.Filename, position.Line)
}
