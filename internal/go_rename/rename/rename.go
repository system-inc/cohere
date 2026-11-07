// Package rename renames local variables in a type-checked Go package, and refuses any rename that would
// change which object some identifier resolves to.
package rename

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
)

// Package is one type-checked package, with the files the engine may read and edit. Files left out (generated,
// skipped by prefix) are never edited; only locals declared in Files are renamed, and a local never occurs
// outside its own file, so nothing in a left-out file can be affected.
type Package struct {
	FileSet *token.FileSet
	Types   *types.Package
	Info    *types.Info
	Files   []*ast.File
}

// Proposal asks for one object to be renamed.
type Proposal struct {
	Object  types.Object
	NewName string
}

// Rejection is a proposal the engine refused, with where the object is declared and why.
type Rejection struct {
	Object   types.Object
	NewName  string
	Position token.Position
	Reason   string
}

// Plan is what survived verification and what did not.
type Plan struct {
	Accepted   map[types.Object]string
	Rejections []Rejection
	// Unresolved lists occurrences whose own object the scope walk never reached. For an object that keeps
	// its name this hides nothing (no renamed object with that name was met either), but any entry means the
	// scope model missed a case, so callers print them.
	Unresolved []string
}

// Edit replaces OldText at Offset in Filename with NewText.
type Edit struct {
	Filename string
	Offset   int
	OldText  string
	NewText  string
}

// occurrence is one lexical identifier and the object go/types resolved it to.
type occurrence struct {
	identifier *ast.Ident
	object     types.Object
	defining   bool
}

// IsLocal reports whether object is a variable declared inside a function: a parameter, named result,
// receiver or local. Fields, package-level variables and everything that is not a *types.Var are not local.
func IsLocal(object types.Object, packageTypes *types.Package) bool {
	variable, isVariable := object.(*types.Var)
	if !isVariable || variable.IsField() || variable.Pkg() != packageTypes {
		return false
	}
	parent := variable.Parent()
	if parent == nil || parent == types.Universe || parent == packageTypes.Scope() {
		return false
	}
	// A file scope's parent is the package scope; variables are never declared in one, but say so.
	if parent.Parent() == packageTypes.Scope() {
		return false
	}
	return true
}

// Check verifies proposals against pkg and returns the plan. It accepts a proposal only if, with every
// accepted rename applied, every lexical identifier in the package still resolves to the same object.
func Check(loadedPackage Package, proposals []Proposal) Plan {
	plan := Plan{Accepted: map[types.Object]string{}}
	implicitObjects := map[types.Object]bool{}
	for _, object := range loadedPackage.Info.Implicits {
		implicitObjects[object] = true
	}
	editableFiles := map[*token.File]bool{}
	for _, file := range loadedPackage.Files {
		editableFiles[loadedPackage.FileSet.File(file.Pos())] = true
	}
	// A type switch's clause variables are the one kind of implicit object the engine renames, all together.
	clauseGuards := typeSwitchClauseVariables(loadedPackage.Info, loadedPackage.Files)

	reject := func(object types.Object, newName string, reason string) {
		plan.Rejections = append(plan.Rejections, Rejection{
			Object:   object,
			NewName:  newName,
			Position: loadedPackage.FileSet.Position(object.Pos()),
			Reason:   reason,
		})
	}

	for _, proposal := range proposals {
		switch {
		case !token.IsIdentifier(proposal.NewName):
			reject(proposal.Object, proposal.NewName, "new name is not a valid identifier")
		case token.IsKeyword(proposal.NewName):
			reject(proposal.Object, proposal.NewName, "new name is a keyword")
		case proposal.NewName == "_":
			reject(proposal.Object, proposal.NewName, "new name is blank")
		case !IsLocal(proposal.Object, loadedPackage.Types):
			reject(proposal.Object, proposal.NewName, "not a local variable")
		case implicitObjects[proposal.Object] && clauseGuards[proposal.Object] == nil:
			reject(proposal.Object, proposal.NewName, "implicit object other than a type switch clause variable")
		case proposal.Object.Name() == "_":
			reject(proposal.Object, proposal.NewName, "blank variable")
		case !editableFiles[loadedPackage.FileSet.File(proposal.Object.Pos())]:
			reject(proposal.Object, proposal.NewName, "declared in a skipped file")
		default:
			if existing, already := plan.Accepted[proposal.Object]; already && existing != proposal.NewName {
				delete(plan.Accepted, proposal.Object)
				reject(proposal.Object, proposal.NewName, "two different new names proposed")
				continue
			}
			plan.Accepted[proposal.Object] = proposal.NewName
		}
	}

	occurrences := collectOccurrences(loadedPackage, editableFiles)
	scopes := allScopes(loadedPackage.Types.Scope())
	signatures := functionSignatures(loadedPackage.Info)

	// Fixpoint: each pass drops every rename found in a conflict, then checks again from scratch with the
	// smaller set, until a pass finds none.
	for {
		index := indexFinalNames(scopes, plan.Accepted)
		conflicts := map[types.Object]string{}
		plan.Unresolved = plan.Unresolved[:0]
		for _, occurrence := range occurrences {
			drops, reached := verifyOccurrence(loadedPackage, index, signatures, plan.Accepted, occurrence)
			for object, reason := range drops {
				if _, recorded := conflicts[object]; !recorded {
					conflicts[object] = reason
				}
			}
			if !reached && len(drops) == 0 {
				plan.Unresolved = append(plan.Unresolved, fmt.Sprintf(
					"%s: %s (%T) not reached by the scope walk",
					loadedPackage.FileSet.Position(occurrence.identifier.Pos()),
					occurrence.identifier.Name,
					occurrence.object,
				))
			}
		}
		if len(conflicts) == 0 {
			conflicts = partialTypeSwitches(clauseGuards, plan.Accepted)
		}
		if len(conflicts) == 0 {
			break
		}
		for object, reason := range conflicts {
			reject(object, plan.Accepted[object], reason)
			delete(plan.Accepted, object)
		}
	}

	sort.Slice(plan.Rejections, func(left, right int) bool {
		leftPosition, rightPosition := plan.Rejections[left].Position, plan.Rejections[right].Position
		if leftPosition.Filename != rightPosition.Filename {
			return leftPosition.Filename < rightPosition.Filename
		}
		return leftPosition.Offset < rightPosition.Offset
	})
	return plan
}

// collectOccurrences gathers every identifier in the editable files that is resolved lexically: everything
// in Defs and Uses except selector right-hand sides, struct fields and methods (so field keys in literals),
// labels, blanks, dot imports, init functions, and the nil entries (package clause, type switch symbol).
func collectOccurrences(loadedPackage Package, editableFiles map[*token.File]bool) []occurrence {
	selectorNames := map[*ast.Ident]bool{}
	for _, file := range loadedPackage.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if selector, isSelector := node.(*ast.SelectorExpr); isSelector {
				selectorNames[selector.Sel] = true
			}
			return true
		})
	}

	var occurrences []occurrence
	add := func(identifier *ast.Ident, object types.Object, defining bool) {
		if object == nil || selectorNames[identifier] || identifier.Name == "_" {
			return
		}
		if !editableFiles[loadedPackage.FileSet.File(identifier.Pos())] {
			return
		}
		switch typed := object.(type) {
		case *types.Var:
			if typed.IsField() {
				return
			}
		case *types.Func:
			signature, isSignature := typed.Type().(*types.Signature)
			if isSignature && signature.Recv() != nil {
				return
			}
			// init functions are never declared in the package scope.
			if typed.Name() == "init" {
				return
			}
		case *types.Label:
			return
		case *types.PkgName:
			if identifier.Name == "." {
				return
			}
		}
		occurrences = append(occurrences, occurrence{identifier: identifier, object: object, defining: defining})
	}
	for identifier, object := range loadedPackage.Info.Defs {
		add(identifier, object, true)
	}
	for identifier, object := range loadedPackage.Info.Uses {
		add(identifier, object, false)
	}
	sort.Slice(occurrences, func(left, right int) bool {
		if occurrences[left].identifier.Pos() != occurrences[right].identifier.Pos() {
			return occurrences[left].identifier.Pos() < occurrences[right].identifier.Pos()
		}
		return occurrences[left].defining && !occurrences[right].defining
	})
	return occurrences
}

// scopeEntry is one scope the walk can meet, and whether it keeps declaration order. Function and block
// scopes do (an object is visible only after it is declared); universe, package and file scopes do not.
type scopeEntry struct {
	scope   *types.Scope
	ordered bool
}

// allScopes lists the universe, the package scope, its file scopes and every scope below them.
func allScopes(packageScope *types.Scope) []scopeEntry {
	entries := []scopeEntry{{scope: types.Universe}}
	var visit func(scope *types.Scope, ordered bool)
	visit = func(scope *types.Scope, ordered bool) {
		entries = append(entries, scopeEntry{scope: scope, ordered: ordered})
		for child := range scope.Children() {
			// The package scope's children are file scopes; everything below a file scope is local.
			visit(child, scope != packageScope)
		}
	}
	visit(packageScope, false)
	return entries
}

// finalIndex maps each scope to its objects by final name.
type finalIndex struct {
	objectsByName map[*types.Scope]map[string][]types.Object
	ordered       map[*types.Scope]bool
}

// indexFinalNames builds the index for the current accepted set.
func indexFinalNames(scopes []scopeEntry, accepted map[types.Object]string) finalIndex {
	index := finalIndex{objectsByName: map[*types.Scope]map[string][]types.Object{}, ordered: map[*types.Scope]bool{}}
	for _, entry := range scopes {
		byName := map[string][]types.Object{}
		for _, name := range entry.scope.Names() {
			object := entry.scope.Lookup(name)
			byName[finalName(accepted, object)] = append(byName[finalName(accepted, object)], object)
		}
		index.objectsByName[entry.scope] = byName
		index.ordered[entry.scope] = entry.ordered
	}
	return index
}

// finalName is the name object will have once every accepted rename is applied.
func finalName(accepted map[types.Object]string, object types.Object) string {
	if newName, renamed := accepted[object]; renamed {
		return newName
	}
	return object.Name()
}

// signatureExtent is where a function's signature lies: its receiver, parameters and results are declared
// between start and end, and are visible only from end, the start of the body.
type signatureExtent struct {
	start token.Pos
	end   token.Pos
}

// functionSignatures maps each function scope to its signature's extent. go/types records a function's scope
// under its *ast.FuncType, and declares the receiver, parameters and results in it with the FuncType's end
// as their scope position; a FuncDecl's top-level body declarations share that scope.
func functionSignatures(info *types.Info) map[*types.Scope]signatureExtent {
	signatures := map[*types.Scope]signatureExtent{}
	for node, scope := range info.Scopes {
		if functionType, isFunctionType := node.(*ast.FuncType); isFunctionType {
			signatures[scope] = signatureExtent{start: functionType.Pos(), end: functionType.End()}
		}
	}
	return signatures
}

// visibleFrom is the position from which object is visible in its own ordered scope: the end of the signature
// for a receiver, parameter or named result (so `func (nodeType nodeType) Next() nodeType` still names the
// type in its signature), else the object's own position, which approximates declaration order.
func visibleFrom(signatures map[*types.Scope]signatureExtent, object types.Object) token.Pos {
	if _, isVariable := object.(*types.Var); !isVariable {
		return object.Pos()
	}
	extent, isFunctionScope := signatures[object.Parent()]
	if isFunctionScope && object.Pos() >= extent.start && object.Pos() < extent.end {
		return extent.end
	}
	return object.Pos()
}

// startScope is where resolution of an occurrence begins: the object's own scope for a defining occurrence
// that has one, else the innermost scope containing the identifier.
func startScope(loadedPackage Package, occurrence occurrence) *types.Scope {
	if occurrence.defining && occurrence.object.Parent() != nil {
		return occurrence.object.Parent()
	}
	return loadedPackage.Types.Scope().Innermost(occurrence.identifier.Pos())
}

// verifyOccurrence re-resolves one occurrence under final names. It returns the renamed objects that must be
// dropped, each with its reason (empty when the occurrence still resolves to its own object), and whether the
// walk reached that object at all.
func verifyOccurrence(
	loadedPackage Package,
	index finalIndex,
	signatures map[*types.Scope]signatureExtent,
	accepted map[types.Object]string,
	occurrence occurrence,
) (map[types.Object]string, bool) {
	target := occurrence.object
	name := finalName(accepted, target)
	_, targetRenamed := accepted[target]
	position := occurrence.identifier.Pos()
	// A declaration is checked where its object becomes visible: for a receiver, parameter or result that is
	// the start of the body, where it meets every other one of them and nothing declared after.
	if occurrence.defining {
		position = visibleFrom(signatures, target)
	}
	for scope := startScope(loadedPackage, occurrence); scope != nil; scope = scope.Parent() {
		var found []types.Object
		for _, object := range index.objectsByName[scope][name] {
			if index.ordered[scope] && typeReadBeforeScope(accepted, target, object, position) {
				continue
			}
			if !index.ordered[scope] || visibleFrom(signatures, object) <= position || isTypeParameter(object) {
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
			if _, renamed := accepted[object]; renamed {
				othersRenamed = true
			}
		}
		if len(others) == 0 {
			return nil, true
		}
		if !targetRenamed && !othersRenamed {
			// Every object here keeps its name, and go/types resolved this occurrence past them, so they are
			// not visible at it in truth: declaration order is approximated by position (`x := x`).
			if containsTarget {
				return nil, true
			}
			continue
		}
		drops := map[types.Object]string{}
		where := loadedPackage.FileSet.Position(position)
		for _, other := range others {
			reason := fmt.Sprintf("conflict: %q at %s:%d would resolve to %s %q declared at %s instead of %q",
				occurrence.identifier.Name, where.Filename, where.Line, objectKind(other), finalName(accepted, other),
				loadedPackage.FileSet.Position(other.Pos()), target.Name())
			if _, renamed := accepted[other]; renamed {
				drops[other] = reason
			}
			if targetRenamed {
				drops[target] = reason
			}
		}
		return drops, containsTarget
	}
	return nil, false
}

// typeReadBeforeScope reports whether position reads the type target before the scope of object, a local renamed
// to target's name, begins: `p := &parser{}` renamed to `parser := &parser{}` (TypeReadBeforeLocalScope).
func typeReadBeforeScope(accepted map[types.Object]string, target types.Object, object types.Object,
	position token.Pos,
) bool {
	if _, renamed := accepted[object]; !renamed {
		return false
	}
	return TypeReadBeforeLocalScope(target, object, position)
}

// TypeReadBeforeLocalScope reports whether position reads the type target before the scope of object, a local
// that has or takes target's name, begins: `parser := &parser{}`, or a parameter `properties properties`. Go
// opens a local's scope at the end of its declaration (the ShortVarDecl or VarSpec) and a parameter's at the
// function body, so the type read there still names the type, and the local is no capture; a read of the type
// after it, in the local's scope, still conflicts. go/types knows exactly where each scope opens, and LookupParent
// from the local's own scope at position finds the local only once its scope has begun. Only a type read is judged
// this way, never a variable's: `err := read(err)` made `readError := read(readError)` is valid Go too, but a
// reader takes the two for one, so a local whose own declaration reads another object of its name stays refused
// by the callers' approximation by position.
func TypeReadBeforeLocalScope(target types.Object, object types.Object, position token.Pos) bool {
	if _, isTypeName := target.(*types.TypeName); !isTypeName {
		return false
	}
	variable, isVariable := object.(*types.Var)
	if !isVariable || variable.IsField() || variable.Parent() == nil {
		return false
	}
	_, visible := variable.Parent().LookupParent(variable.Name(), position)
	return visible != variable
}

// isTypeParameter reports whether object is a type parameter, which is visible throughout its scope
// whatever the order: `func f[G Graph[F], F any]()` uses F before declaring it.
func isTypeParameter(object types.Object) bool {
	typeName, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		return false
	}
	_, isParameter := typeName.Type().(*types.TypeParam)
	return isParameter
}

// objectKind names an object's kind for a rejection reason.
func objectKind(object types.Object) string {
	switch object.(type) {
	case *types.Var:
		return "variable"
	case *types.Func:
		return "func"
	case *types.TypeName:
		return "type"
	case *types.Const:
		return "const"
	case *types.PkgName:
		return "package"
	case *types.Builtin:
		return "builtin"
	case *types.Nil:
		return "nil"
	}
	return fmt.Sprintf("%T", object)
}

// Edits lists the identifier rewrites the accepted renames need: every defining and using identifier of each
// renamed object.
func Edits(loadedPackage Package, accepted map[types.Object]string) []Edit {
	var edits []Edit
	add := func(identifier *ast.Ident, object types.Object) {
		newName, renamed := accepted[object]
		if !renamed {
			return
		}
		position := loadedPackage.FileSet.PositionFor(identifier.Pos(), false)
		edits = append(
			edits,
			Edit{Filename: position.Filename, Offset: position.Offset, OldText: identifier.Name, NewText: newName},
		)
	}
	for identifier, object := range loadedPackage.Info.Defs {
		add(identifier, object)
	}
	for identifier, object := range loadedPackage.Info.Uses {
		add(identifier, object)
	}
	edits = append(edits, docCommentEdits(loadedPackage, accepted)...)
	edits = append(edits, typeSwitchGuardEdits(loadedPackage, accepted)...)
	sort.Slice(edits, func(left, right int) bool {
		if edits[left].Filename != edits[right].Filename {
			return edits[left].Filename < edits[right].Filename
		}
		return edits[left].Offset < edits[right].Offset
	})
	return edits
}

// docCommentEdits renames a renamed parameter where its function's doc comment names it: in a Mutates line (the
// marker correctness-no-caller-data-mutation reads, `// Mutates: <parameter> <why>`, read here from its one home), and
// in a code span that holds exactly its name.
func docCommentEdits(loadedPackage Package, accepted map[types.Object]string) []Edit {
	edits := mutatesEdits(loadedPackage, accepted)
	mutated := map[int]bool{}
	for _, edit := range edits {
		mutated[edit.Offset] = true
	}
	for _, edit := range codeSpanEdits(loadedPackage, accepted) {
		// A Mutates line whose word is also a code span is edited once.
		if !mutated[edit.Offset] {
			edits = append(edits, edit)
		}
	}
	return edits
}

// renamedParametersOf maps the old name of each renamed parameter of function to its new name.
func renamedParametersOf(
	loadedPackage Package,
	accepted map[types.Object]string,
	function *ast.FuncDecl,
) map[string]string {
	renamedParameters := map[string]string{}
	if function.Type.Params == nil {
		return renamedParameters
	}
	for _, field := range function.Type.Params.List {
		for _, name := range field.Names {
			if newName, renamed := accepted[loadedPackage.Info.Defs[name]]; renamed {
				renamedParameters[name.Name] = newName
			}
		}
	}
	return renamedParameters
}

// codeSpanEdits renames a renamed parameter where its function's doc comment writes its name alone in
// backticks, as in "returns the length of `text`". Code spans are paired within each line, the way a doc
// comment's markdown pairs them, so the closing backtick of one span never opens another; a span holding
// anything besides the name (`text.Length`, `text == ""`) is prose about code and is left as written.
func codeSpanEdits(loadedPackage Package, accepted map[types.Object]string) []Edit {
	var edits []Edit
	for _, file := range loadedPackage.Files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Doc == nil {
				continue
			}
			renamedParameters := renamedParametersOf(loadedPackage, accepted, function)
			if len(renamedParameters) == 0 {
				continue
			}
			for _, comment := range function.Doc.List {
				commentPosition := loadedPackage.FileSet.PositionFor(comment.Pos(), false)
				lineStart := 0
				for _, line := range strings.SplitAfter(comment.Text, "\n") {
					spanStart := -1
					for index := 0; index < len(line); index++ {
						if line[index] != '`' {
							continue
						}
						if spanStart < 0 {
							spanStart = index + 1
							continue
						}
						if newName, renamed := renamedParameters[line[spanStart:index]]; renamed {
							edits = append(edits, Edit{
								Filename: commentPosition.Filename,
								Offset:   commentPosition.Offset + lineStart + spanStart,
								OldText:  line[spanStart:index],
								NewText:  newName,
							})
						}
						spanStart = -1
					}
					lineStart += len(line)
				}
			}
		}
	}
	return edits
}

// mutatesEdits renames a renamed parameter where its function's doc comment names it in a Mutates line, read
// exactly as the rule reads it: the marker opening a line, then the parameter's name as the first word. Without
// it, a rename leaves a Mutates line naming no parameter, and the write it excused is a finding again.
func mutatesEdits(loadedPackage Package, accepted map[types.Object]string) []Edit {
	var edits []Edit
	for _, file := range loadedPackage.Files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Doc == nil {
				continue
			}
			renamedParameters := renamedParametersOf(loadedPackage, accepted, function)
			if len(renamedParameters) == 0 {
				continue
			}
			for _, comment := range function.Doc.List {
				commentOffset := loadedPackage.FileSet.PositionFor(comment.Pos(), false)
				lineStart := 0
				for _, line := range strings.SplitAfter(comment.Text, "\n") {
					trimmed := strings.TrimLeft(line, " \t/*")
					if tag, hasMarker := strings.CutPrefix(trimmed, marker.Mutates); hasMarker {
						word := strings.TrimLeft(tag, " \t")
						if end := strings.IndexFunc(word, unicode.IsSpace); end >= 0 {
							word = word[:end]
						}
						if newName, renamed := renamedParameters[strings.TrimSuffix(word, "*/")]; renamed {
							wordStart := lineStart +
								(len(line) - len(trimmed)) +
								len(marker.Mutates) +
								(len(tag) - len(strings.TrimLeft(tag, " \t")))
							edits = append(edits, Edit{
								Filename: commentOffset.Filename,
								Offset:   commentOffset.Offset + wordStart,
								OldText:  strings.TrimSuffix(word, "*/"),
								NewText:  newName,
							})
						}
					}
					lineStart += len(line)
				}
			}
		}
	}
	return edits
}
