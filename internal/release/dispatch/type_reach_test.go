package dispatch

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// A rule claims TypeReach Shapes only where nothing it can run reads into an imported function's body, and
// this is what makes the claim checked rather than trusted.
//
// A shape-keyed rule's findings on a file replay while the shapes of the files it imports are unchanged:
// their declaration output and their text with function bodies cut out (program.SignatureEntry). A rule
// that reaches an imported declaration through the checker (a symbol's declarations, a signature's
// declaration) and then descends into it (a body, its children, its statements) reads what no shape covers,
// and keyed on shapes it would replay a finding after the body it read had changed: zero findings, forever,
// indistinguishable from a clean tree.
//
// So this loads the rule packages with their types, and for each rule file follows every function the
// file refers to, across packages and through helpers, to a fixed point. A rule that can reach both an
// imported-declaration accessor and a descent accessor may not claim Shapes. The scan cannot tell an
// imported declaration from one in the rule's own file, so it refuses more than it must: a refused claim
// costs re-runs, an admitted wrong one costs a stale verdict.
//
// Descent includes reading text. A rule can take a declaration's file and slice its text across the
// declaration's range, body and all, so `.Text()` on any source file but the rule context's own counts,
// and so do the helpers that read a node's text out of its file (@system_cohere_lint found this way in).
//
// A call through an interface cannot be followed, since its target is not known here. Rather than trust
// that no helper is called that way, any rule that reaches an interface method taking an *ast.Node fails:
// a future visitor-style helper turns this red instead of blind.
func TestRulesClaimShapesOnlyWhereTheScanAllowsIt(t *testing.T) {
	scan := scanTypeReach(t)
	if len(scan.claims) == 0 && len(scan.mayReadImportedBodies) == 0 {
		t.Fatal("the scan found no rules at all, so this test proved nothing")
	}
	if len(scan.mayReadImportedBodies) == 0 {
		// Dozens of rules reach a declaration and descend today. Zero would mean the scan stopped seeing
		// the accessors, which would admit every claim.
		t.Fatal("no rule reaches both an imported declaration and a body, which is implausible: the scan is blind")
	}
	for _, path := range scan.callsNodeInterfaces {
		t.Errorf("%s reaches a call through an interface that takes an *ast.Node, which this scan cannot follow, "+
			"so what it reads of an imported declaration is unknown; call the helper directly", path)
	}
	for _, name := range scan.claims {
		if scan.mayReadImportedBodies[name] {
			t.Errorf("rule %q claims TypeReach Shapes and can reach both an imported declaration and a body "+
				"(%s), so a shape-keyed replay could serve a finding computed against a body that changed; "+
				"leave it on Contents", name, scan.reason[name])
		}
	}
}

type typeReachScan struct {
	claims                []string
	mayReadImportedBodies map[string]bool
	reason                map[string]string

	// callsNodeInterfaces is every rule file that reaches a call through an interface taking an *ast.Node,
	// which the scan cannot follow.
	callsNodeInterfaces []string
}

// importedDeclarationAccessors are the ways the checker hands a rule a node that can live in another file.
var importedDeclarationAccessors = map[string]bool{
	"Declarations": true, "ValueDeclaration": true, "Declaration": true, "GetTypeOnlyAliasDeclaration": true,
}

// isDescentAccessor reports the ways a rule reads below a node it was handed: its body, its children, its
// statements, or its text.
func isDescentAccessor(name string) bool {
	return name == "Body" || name == "FunctionBody" || name == "GetFunctionBody" || name == "Statements" ||
		name == "Children" || name == "IterChildren" || name == "VisitEachChild" || strings.HasPrefix(name, "ForEachChild") ||
		name == "GetTextOfNode" || name == "GetSourceTextOfNodeFromSourceFile" || name == "GetTextOfNodeFromSourceText"
}

// isOwnSourceFile reports whether an expression is the rule context's own file, `<context>.SourceFile`,
// whose text a rule may read in full: its own bytes are in every key it has.
func isOwnSourceFile(expression ast.Expr, info *types.Info) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "SourceFile" {
		return false
	}
	selection := info.Selections[selector]
	if selection == nil {
		return false
	}
	receiver := selection.Recv()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	named, ok := receiver.(*types.Named)
	return ok && named.Obj().Name() == "Context" && named.Obj().Pkg() != nil &&
		strings.HasSuffix(named.Obj().Pkg().Path(), "/internal/lint/rule")
}

func scanTypeReach(t *testing.T) typeReachScan {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: root,
	}, "./internal/lint/...")
	if err != nil {
		t.Fatalf("loading the rule packages: %v", err)
	}
	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			t.Fatalf("loading %s: %v", pkg.PkgPath, pkg.Errors[0])
		}
	}

	type body struct {
		node ast.Node
		info *types.Info
	}
	bodies := map[types.Object]body{}
	type ruleFile struct {
		path string
		file *ast.File
		info *types.Info
	}
	var ruleFiles []ruleFile
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			path := pkg.Fset.Position(file.Pos()).Filename
			if strings.Contains(path, "/internal/lint/rules/") && !strings.HasSuffix(path, "_test.go") {
				ruleFiles = append(ruleFiles, ruleFile{path, file, pkg.TypesInfo})
			}
			for _, declaration := range file.Decls {
				switch typed := declaration.(type) {
				case *ast.FuncDecl:
					if object := pkg.TypesInfo.Defs[typed.Name]; object != nil && typed.Body != nil {
						bodies[object] = body{typed, pkg.TypesInfo}
					}
				case *ast.GenDecl:
					for _, spec := range typed.Specs {
						if value, ok := spec.(*ast.ValueSpec); ok {
							for _, name := range value.Names {
								if object := pkg.TypesInfo.Defs[name]; object != nil {
									bodies[object] = body{value, pkg.TypesInfo}
								}
							}
						}
					}
				}
			}
		}
	}

	compiler := func(object types.Object) bool {
		return object != nil && object.Pkg() != nil && strings.Contains(object.Pkg().Path(), "TypeScript/tsc")
	}
	// reaches answers, for one predicate, whether a node or anything it refers to touches a matching
	// compiler accessor, memoized per object and safe on recursion. matchesSelector, when set, decides a
	// selector on its own, for a match that depends on the receiver as well as the accessor.
	reaches := func(matches func(types.Object) bool, matchesSelector func(*ast.SelectorExpr, *types.Selection, *types.Info) bool) func(ast.Node, *types.Info) bool {
		memo := map[types.Object]int{} // 1 visiting, 2 no, 3 yes
		var walk func(ast.Node, *types.Info) bool
		var follow func(types.Object) bool
		follow = func(object types.Object) bool {
			switch memo[object] {
			case 1, 2:
				return false
			case 3:
				return true
			}
			target, known := bodies[object]
			if !known {
				return false
			}
			memo[object] = 1
			found := walk(target.node, target.info)
			memo[object] = 2
			if found {
				memo[object] = 3
			}
			return found
		}
		walk = func(node ast.Node, info *types.Info) bool {
			found := false
			ast.Inspect(node, func(child ast.Node) bool {
				if found {
					return false
				}
				switch typed := child.(type) {
				case *ast.SelectorExpr:
					selection := info.Selections[typed]
					if selection != nil && matchesSelector != nil && matchesSelector(typed, selection, info) {
						found = true
					} else if selection != nil && compiler(selection.Obj()) && matches(selection.Obj()) {
						found = true
					}
				case *ast.Ident:
					object := info.Uses[typed]
					if compiler(object) && matches(object) {
						found = true
					} else if object != nil && follow(object) {
						found = true
					}
				}
				return !found
			})
			return found
		}
		return walk
	}
	reachesDeclaration := reaches(func(object types.Object) bool { return importedDeclarationAccessors[object.Name()] }, nil)
	reachesDescent := reaches(
		func(object types.Object) bool { return isDescentAccessor(object.Name()) },
		func(selector *ast.SelectorExpr, selection *types.Selection, info *types.Info) bool {
			return selector.Sel.Name == "Text" && compiler(selection.Obj()) && selection.Kind() == types.MethodVal &&
				strings.Contains(types.TypeString(selection.Recv(), nil), "SourceFile") && !isOwnSourceFile(selector.X, info)
		},
	)
	reachesNodeInterface := reaches(func(types.Object) bool { return false },
		func(selector *ast.SelectorExpr, selection *types.Selection, info *types.Info) bool {
			if selection.Kind() != types.MethodVal || !types.IsInterface(selection.Recv()) {
				return false
			}
			signature, ok := selection.Type().(*types.Signature)
			if !ok {
				return false
			}
			for index := range signature.Params().Len() {
				if strings.HasSuffix(types.TypeString(signature.Params().At(index).Type(), nil), "/ast.Node") {
					return true
				}
			}
			return false
		},
	)

	scan := typeReachScan{mayReadImportedBodies: map[string]bool{}, reason: map[string]string{}}
	for _, entry := range ruleFiles {
		name, claimsShapes := ruleLiteralTypeReach(entry.file)
		if name == "" {
			continue
		}
		if claimsShapes {
			scan.claims = append(scan.claims, name)
		}
		if reachesDeclaration(entry.file, entry.info) && reachesDescent(entry.file, entry.info) {
			scan.mayReadImportedBodies[name] = true
			scan.reason[name] = filepath.Base(filepath.Dir(entry.path)) + "/" + filepath.Base(entry.path)
		}
		if reachesNodeInterface(entry.file, entry.info) {
			scan.callsNodeInterfaces = append(scan.callsNodeInterfaces, filepath.Base(filepath.Dir(entry.path))+"/"+filepath.Base(entry.path))
		}
	}
	sort.Strings(scan.claims)
	return scan
}

// ruleLiteralTypeReach returns the Name of the first rule.Rule literal in a file, and whether it sets
// TypeReach to rule.TypeReachShapes. Only a key in the literal counts, never a comment.
func ruleLiteralTypeReach(file *ast.File) (string, bool) {
	name, shapes := "", false
	ast.Inspect(file, func(node ast.Node) bool {
		if name != "" {
			return false
		}
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if selector, ok := literal.Type.(*ast.SelectorExpr); !ok || selector.Sel.Name != "Rule" {
			return true
		}
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Name":
				if value, ok := pair.Value.(*ast.BasicLit); ok {
					name, _ = strconv.Unquote(value.Value)
				}
			case "TypeReach":
				if value, ok := pair.Value.(*ast.SelectorExpr); ok && value.Sel.Name == "TypeReachShapes" {
					shapes = true
				}
			}
		}
		return false
	})
	return name, shapes
}
