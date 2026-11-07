package global_rename

import (
	"go/ast"
	"go/token"
	"go/types"
)

// methodReaderRefusal is why the encoding guard keeps a method's name, or "": a method is read by name at run time
// wherever the text that spells the name lives, so the flow is the evidence, never a spelling.
//   - A template calls an exported method of the value it executes by the name its text spells, and the text may live
//     in a file no Go literal holds: ParseFiles("page.html"), ParseFS over an embedded directory, ParseGlob. So an
//     exported method of a type whose values the flow finds reaching text/template or html/template keeps its name.
//   - reflect looks a method up by a name its caller computes with MethodByName, which no literal spells. So while a
//     workspace package calls MethodByName with an argument that is no constant (computedMethodLookup), an exported
//     method of a type whose values reach reflect, in a package reading names through it, keeps its name. The flow
//     does not say which reflect call a value reached, so this keeps every such method, the safe side of not knowing.
//
// Not seen here: a lookup by a computed name in a library outside the workspace, and a walk of a type's methods by
// index whose Name is matched against a computed string. Where a literal spells the name, the string-literal guard
// answers for both (see nameReadingSites); otherwise neither guard does.
func (engine *engine) methodReaderRefusal(candidate *candidate) string {
	if !token.IsExported(candidate.OldName) {
		return ""
	}
	function, isFunction := candidate.object.(*types.Func)
	if !isFunction || function.Signature().Recv() == nil {
		return ""
	}
	receiverType, _ := dereference(function.Signature().Recv().Type())
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed {
		return ""
	}
	reached := engine.encoded.types[engine.keyer.key(named.Origin().Obj())]
	if reached&encoderTemplate != 0 {
		return "its receiver reaches a template, which calls a method by the name its text spells, wherever the " +
			"text lives"
	}
	if reached&encoderReflect != 0 {
		if where := engine.computedMethodLookup(); where != "" {
			return "its receiver reaches reflect, and " + where + " looks a method up with MethodByName by a name " +
				"it computes"
		}
	}
	return ""
}

// computedMethodLookup is where a root package first calls reflect's MethodByName, of a Type or a Value, with an
// argument that is no constant, as a position relative to the root, or "" when none does. It is asked once.
func (engine *engine) computedMethodLookup() string {
	if engine.methodLookup != nil {
		return *engine.methodLookup
	}
	found := ""
	for _, rootsPackage := range engine.roots {
		for _, file := range rootsPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if found != "" || !isCall || len(call.Args) != 1 {
					return found == ""
				}
				callee, isFunction := engine.encoded.referencedObject(rootsPackage, ast.Unparen(call.Fun)).(*types.Func)
				if !isFunction || callee.Name() != "MethodByName" || callee.Pkg() == nil ||
					callee.Pkg().Path() != "reflect" {
					return true
				}
				if rootsPackage.TypesInfo.Types[call.Args[0]].Value == nil {
					found = engine.relativePosition(positionString(rootsPackage.Fset.PositionFor(call.Pos(), false)))
				}
				return found == ""
			})
		}
	}
	engine.methodLookup = &found
	return found
}
