package build_reads

import (
	"go/ast"
	"go/types"
	"reflect"
	"testing"
)

// wholeDeclarationTypeReads are own's type reads as collectTypeReads collected them before the walk recorded what each
// declaration uses (#kr1m304): the same map-wide reads, and the use walk with each whole top-level declaration as its
// root, a general declaration's node, comment and every spec in one walk, and no record.
func wholeDeclarationTypeReads(own *types.Package, info *types.Info, files []*ast.File) typeReads {
	collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
	for _, used := range info.Uses {
		kind := UseMember
		if _, isTypeName := used.(*types.TypeName); isTypeName {
			kind = UseName
		}
		collected.add(own, used, kind)
	}
	for _, selection := range info.Selections {
		collectSelection(&collected, own, selection)
	}
	for _, implicit := range info.Implicits {
		collected.add(own, implicit, UseWhole)
	}
	switch {
	case info.Types == nil || len(files) == 0:
		collected.closureOnly = "its syntax or expression types were not there to tell its uses apart"
		return collected
	case linknamed(files):
		collected.closureOnly = "it names a symbol by go:linkname, past the type checker"
		return collected
	}
	walk := &useWalk{own: own, info: info, collected: &collected, contexts: map[ast.Expr]useContext{},
		emptyInterface: types.Universe.Lookup("any").Type(), seen: map[*types.Named]bool{},
		wholeOperands: map[*ast.SelectorExpr]bool{}}
	for _, file := range files {
		for _, declaration := range file.Decls {
			var results *types.Tuple
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				if declared, isFunction := info.Defs[function.Name].(*types.Func); isFunction {
					results = declared.Signature().Results()
				}
			}
			walk.inspect(declaration, results)
		}
	}
	for identifier, instance := range info.Instances {
		walk.instance(identifier, instance)
	}
	return collected
}

// The use walk that records what each declaration uses walks each spec of a general declaration as its own root and
// leaves the general declaration's node and comment unvisited, and it reads every foreign type exactly as the walk of
// each whole declaration did: over the standard library's packages named in useWalkSources and every one they import,
// checked from source, and the function uses fixture's packages, both collect the same uses, selections and
// closure-only reason. It proves the type reads alone, never the record: it sets the walk's function uses to nil
// before it compares, and the reference returns early for a linknamed package and one without types, so those compare
// equal without the walk being tried. What the record holds is the function uses tests' to pin.
func TestTheUseWalkReadsAsTheWalkOfEachWholeDeclaration(test *testing.T) {
	test.Parallel()

	sources := checkedStandardLibrary(test, useWalkSources)
	_, fixture := checkedFixture(test, functionUsesSources)
	for _, checked := range fixture {
		sources[checked.own] = checked.source
	}
	compared, recorded := 0, 0
	for own, source := range sources {
		walked := collectTypeReads(own, source.info, source.files)
		if walked.functionUses != nil {
			recorded++
		}
		// The whole-declaration walk records no function uses, so the record is set to nil here and goes unchecked:
		// only the type reads are compared.
		walked.functionUses = nil
		whole := wholeDeclarationTypeReads(own, source.info, source.files)
		if !reflect.DeepEqual(walked, whole) {
			test.Errorf("%s: the walk by spec collected %d uses, %d selections and closure-only %q, the walk of each "+
				"whole declaration %d, %d and %q", own.Path(), len(walked.uses), len(walked.selections),
				walked.closureOnly, len(whole.uses), len(whole.selections), whole.closureOnly)
		}
		compared++
	}
	test.Logf("%d packages compared, %d of them with function uses recorded", compared, recorded)
	if compared < 100 || recorded < 100 {
		test.Fatalf("compared %d packages, %d with function uses recorded, too few to read", compared, recorded)
	}
}
