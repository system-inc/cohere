package check

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_language/load"
)

// mutatingMethods are the methods of sync's and sync/atomic's types that write the value they are called on, which a
// call on package state counts as a write by. Every other method call is taken for a read: a method of a reached
// package writes in its own body, where the guard reads it, and these are how a package keeps shared state safe for
// concurrent passes.
var mutatingMethods = []string{
	"Add", "And", "Clear", "CompareAndDelete", "CompareAndSwap", "Delete", "LoadAndDelete", "LoadOrStore", "Or", "Put",
	"Store", "Swap",
}

// onceFunctions are sync's functions and Once's method that run a function argument once for the whole process.
var onceFunctions = []string{"Do", "OnceFunc", "OnceValue", "OnceValues"}

// packageStateMarker ends each line of the package-state fixture whose write the guard must see.
const packageStateMarker = "// written: package state"

// isPackageVariable reports whether object is a package-level variable, of its own package or another.
func isPackageVariable(object types.Object) bool {
	variable, isVariable := object.(*types.Var)
	return isVariable && variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope()
}

// isHeldType reports whether current, or what it points to, is a named type a package-level variable can hold.
func isHeldType(current types.Type, held map[*types.TypeName]bool) bool {
	if current == nil {
		return false
	}
	pointee := current
	if pointer, isPointer := types.Unalias(current).(*types.Pointer); isPointer {
		pointee = pointer.Elem()
	}
	named, isNamed := types.Unalias(pointee).(*types.Named)
	// A generic type's methods see it by its origin, as Memo[Value], and its holder by an instance, as
	// Memo[*foreignMethods]; the origin names both.
	return isNamed && held[named.Origin().Obj()]
}

// heldTypesOf is every named type whose value a package-level variable of reached can hold, by its origin: the
// variable's own type, and what it points to, its fields, its map keys and elements and its slice and array elements
// hold, so state behind a pointer, a field or a map's entry is held as the variable is. The walk stops at an interface,
// a function, a channel and a type parameter, whose contents its type does not say, but for a variable's initializer,
// whose type is what it holds when the package starts.
func heldTypesOf(reached []*packages.Package) map[*types.TypeName]bool {
	held := map[*types.TypeName]bool{}
	seen := map[types.Type]bool{}
	var walk func(current types.Type)
	walk = func(current types.Type) {
		if current == nil || seen[current] {
			return
		}
		seen[current] = true
		switch typed := types.Unalias(current).(type) {
		case *types.Named:
			held[typed.Origin().Obj()] = true
			walk(typed.Underlying())
			// The origin's fields are what its methods write through, spelled with its type parameters, as
			// Memo[Value]'s map of *Load[Value].
			walk(typed.Origin().Underlying())
		case *types.Pointer:
			walk(typed.Elem())
		case *types.Struct:
			for index := range typed.NumFields() {
				walk(typed.Field(index).Type())
			}
		case *types.Map:
			walk(typed.Key())
			walk(typed.Elem())
		case *types.Slice:
			walk(typed.Elem())
		case *types.Array:
			walk(typed.Elem())
		case *types.Tuple:
			for index := range typed.Len() {
				walk(typed.At(index).Type())
			}
		}
	}
	for _, analyzed := range reached {
		scope := analyzed.Types.Scope()
		for _, name := range scope.Names() {
			if variable, isVariable := scope.Lookup(name).(*types.Var); isVariable {
				walk(variable.Type())
			}
		}
		// An initializer's type is what a variable of an interface type holds when the package starts.
		for _, initializer := range analyzed.TypesInfo.InitOrder {
			walk(analyzed.TypesInfo.TypeOf(initializer.Rhs))
		}
	}
	return held
}

// stateReachOf is what expression, a location or a value, is reached through, walked from it to its root: whether the
// root is a package-level variable, whether a held value or a local holding held state (see heldAliasesOf) leads to it,
// and whether the walk reads through an indirection, a pointer, a map or a slice, by which a write lands somewhere
// other than a local's own storage.
func stateReachOf(
	information *types.Info,
	expression ast.Expr,
	held map[*types.TypeName]bool,
	aliases map[*types.Var]bool,
) (rootedInPackage bool, leadsHeld bool, indirect bool) {
	current := expression
	for {
		switch typed := current.(type) {
		case *ast.Ident:
			object := information.ObjectOf(typed)
			variable, isVariable := object.(*types.Var)
			return isPackageVariable(object), leadsHeld || isVariable && aliases[variable], indirect
		case *ast.SelectorExpr:
			selection := information.Selections[typed]
			if selection == nil {
				// A selector with no selection is qualified by an import, as other.Variable, the root itself.
				return isPackageVariable(information.Uses[typed.Sel]), leadsHeld, indirect
			}
			if selection.Kind() != types.FieldVal {
				// A method value is no location, and a call is walked on its own.
				return false, leadsHeld, indirect
			}
			// Indirect is whether the selection reads through a pointer, an embedded field's included.
			indirect = indirect || selection.Indirect()
			leadsHeld = leadsHeld || isHeldType(information.TypeOf(typed.X), held)
			current = typed.X
		case *ast.IndexExpr:
			operand := information.TypeOf(typed.X)
			if operand == nil {
				return false, leadsHeld, indirect
			}
			switch operand.Underlying().(type) {
			case *types.Signature:
				// A generic function's instantiation, as compute[int], indexes no value.
				return false, leadsHeld, indirect
			case *types.Array, *types.Basic:
				// An array's element is the array's own storage, and a string's byte is never written.
			default:
				indirect = true
			}
			leadsHeld = leadsHeld || isHeldType(operand, held)
			current = typed.X
		case *ast.SliceExpr:
			operand := information.TypeOf(typed.X)
			if operand == nil {
				return false, leadsHeld, indirect
			}
			// A slice of a slice shares its array, as one of an array's pointer does; one of an array value is
			// taken of an addressable array, its storage too.
			if _, isArray := operand.Underlying().(*types.Array); !isArray {
				indirect = true
			}
			leadsHeld = leadsHeld || isHeldType(operand, held)
			current = typed.X
		case *ast.StarExpr:
			indirect = true
			leadsHeld = leadsHeld || isHeldType(information.TypeOf(typed.X), held)
			current = typed.X
		case *ast.UnaryExpr:
			// The address of a location reaches what the location does.
			if typed.Op != token.AND {
				return false, leadsHeld, indirect
			}
			current = typed.X
		case *ast.ParenExpr:
			current = typed.X
		case *ast.TypeAssertExpr:
			// An asserted value is the one its operand holds.
			current = typed.X
		default:
			// A call's result, a literal or anything else holds what its type says, which a selection above it judged.
			return false, leadsHeld, indirect
		}
	}
}

// isStateWrite reports whether a write to a location of that reach writes state that outlives a pass: a
// package-level variable's own, or a location behind an indirection that held state leads to.
func isStateWrite(rootedInPackage bool, leadsHeld bool, indirect bool) bool {
	return rootedInPackage || leadsHeld && indirect
}

// isReference reports whether a value of current shares what it holds with every copy of it: a map, a slice or a
// pointer.
func isReference(current types.Type) bool {
	switch current.Underlying().(type) {
	case *types.Map, *types.Slice, *types.Pointer:
		return true
	}
	return false
}

// heldAliasesOf is each local variable of the package's functions given a map, a slice or a pointer that a
// package-level variable or held state leads to, as `byLoad := memo.byLoad` or `entry := memo.byLoad[key]`, from its
// declaration, an assignment or a range, and so holds the same state: a write through it writes that state. A local is
// held for its whole function once anything assigns it held state, which over-counts a local later given something
// else, and the alias is followed within one function only, never into a call or out of a return.
func heldAliasesOf(analyzed *packages.Package, held map[*types.TypeName]bool) map[*types.Var]bool {
	information := analyzed.TypesInfo
	aliases := map[*types.Var]bool{}
	grew := true
	// alias holds target held when value leads to package state and target is a local that shares it.
	alias := func(target ast.Expr, value ast.Expr) {
		identifier, isIdentifier := target.(*ast.Ident)
		if !isIdentifier {
			return
		}
		variable, isVariable := information.ObjectOf(identifier).(*types.Var)
		if !isVariable || aliases[variable] || variable.IsField() || isPackageVariable(variable) ||
			!isReference(variable.Type()) {
			return
		}
		if rootedInPackage, leadsHeld, _ := stateReachOf(information, value, held, aliases); rootedInPackage ||
			leadsHeld {
			aliases[variable] = true
			grew = true
		}
	}
	// pair aliases each of targets with its value, or, for a comma-ok read, as `entry, known := memo.byLoad[key]`,
	// the first with the one value.
	pair := func(targets []ast.Expr, values []ast.Expr) {
		if len(targets) == len(values) {
			for index := range targets {
				alias(targets[index], values[index])
			}
		} else if len(values) == 1 && len(targets) == 2 {
			alias(targets[0], values[0])
		}
	}
	// An alias of an alias is held too, so the walk repeats until a pass holds nothing new.
	for grew {
		grew = false
		for _, file := range analyzed.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.AssignStmt:
					pair(typed.Lhs, typed.Rhs)
				case *ast.ValueSpec:
					// A package-level declaration names package variables, which alias leaves out.
					targets := make([]ast.Expr, len(typed.Names))
					for index, name := range typed.Names {
						targets[index] = name
					}
					pair(targets, typed.Values)
				case *ast.RangeStmt:
					for _, target := range []ast.Expr{typed.Key, typed.Value} {
						if target != nil {
							alias(target, typed.X)
						}
					}
				}
				return true
			})
		}
	}
	return aliases
}

// isOnceBody reports whether call runs its function argument once for the whole process, as a sync.Once's Do and
// sync.OnceFunc, sync.OnceValue and sync.OnceValues do. Once is no safety by itself: the first package whose pass
// reaches the call decides what every later pass reads, so a write inside the body is left out only when the body
// captures nothing of its caller (see capturesCaller).
func isOnceBody(information *types.Info, call *ast.CallExpr) bool {
	// Callee sees through an instantiation spelled out, as sync.OnceValue[int], to the function it instantiates.
	function, isFunction := typeutil.Callee(information, call).(*types.Func)
	// Once's Do is sync's only method by that name.
	return isFunction && function.Pkg() != nil && function.Pkg().Path() == "sync" &&
		slices.Contains(onceFunctions, function.Name())
}

// capturesCaller reports whether literal, a once's body inside enclosing, the outermost function around it, reads
// anything enclosing declares outside the literal: a parameter, a receiver or a local, which differ with each pass that
// calls it. A body that reads only package-level names, imports and its own locals builds the same data whichever pass
// runs it first. A once at package level has no enclosing function, so it captures nothing.
func capturesCaller(information *types.Info, literal *ast.FuncLit, enclosing ast.Node) bool {
	if enclosing == nil {
		return false
	}
	captures := false
	ast.Inspect(literal.Body, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || information.Uses[identifier] == nil {
			return !captures
		}
		declared := information.Uses[identifier].Pos()
		insideEnclosing := declared >= enclosing.Pos() && declared < enclosing.End()
		insideLiteral := declared >= literal.Pos() && declared < literal.End()
		if declared.IsValid() && insideEnclosing && !insideLiteral {
			captures = true
		}
		return !captures
	})
	return captures
}

// isInitFunction reports whether declaration is an init function, which runs as its package initializes.
func isInitFunction(declaration *ast.FuncDecl) bool {
	return declaration.Recv == nil && declaration.Name.Name == "init"
}

// functionReferencesOf is how the reach refers to each function and method its packages use, by origin: the
// functions whose body directly calls it, by callers, whether a package-level initializer or an init function's body
// calls it, by fromInitialization, and whether any reference is something else, by otherwise: a value, as `Run: run`
// or `sync.OnceValue(compute)`, a call inside a function literal, which may run whenever the literal is called, or a
// call a go statement starts, which runs on a goroutine of its own whenever the scheduler runs it, a pass's time too.
func functionReferencesOf(reached []*packages.Package) (
	callers map[*types.Func]map[*types.Func]bool,
	fromInitialization map[*types.Func]bool,
	otherwise map[*types.Func]bool,
) {
	callers = map[*types.Func]map[*types.Func]bool{}
	fromInitialization = map[*types.Func]bool{}
	otherwise = map[*types.Func]bool{}
	for _, analyzed := range reached {
		information := analyzed.TypesInfo
		for _, file := range analyzed.Syntax {
			// called is each name a call is made through, as f in `f(x)`, `(f)(x)`, `f[int](x)` and `value.f(x)`, but
			// for the call a go statement starts, which is spawned. A deferred call stays a call: it runs before its
			// function returns.
			called := map[*ast.Ident]bool{}
			spawned := map[*ast.CallExpr]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				// The walk reaches a go statement before the call it holds.
				if statement, isGo := node.(*ast.GoStmt); isGo {
					spawned[statement.Call] = true
				}
				call, isCall := node.(*ast.CallExpr)
				if !isCall || spawned[call] {
					return true
				}
				function := ast.Unparen(call.Fun)
				switch instantiated := function.(type) {
				case *ast.IndexExpr:
					function = instantiated.X
				case *ast.IndexListExpr:
					function = instantiated.X
				}
				switch name := function.(type) {
				case *ast.Ident:
					called[name] = true
				case *ast.SelectorExpr:
					called[name.Sel] = true
				}
				return true
			})
			// walk records each reference under node, made by caller, the function declaration whose body it is in,
			// nil at package level, inInit whether that is an init function, and inLiteral whether it is inside a
			// function literal.
			var walk func(node ast.Node, caller *types.Func, inInit bool, inLiteral bool)
			walk = func(node ast.Node, caller *types.Func, inInit bool, inLiteral bool) {
				ast.Inspect(node, func(current ast.Node) bool {
					if current == node {
						return true
					}
					switch typed := current.(type) {
					case *ast.FuncDecl:
						if typed.Body != nil {
							// A declaration the checker gave no object calls nothing a function could be judged by.
							declared, _ := information.Defs[typed.Name].(*types.Func)
							walk(typed.Body, declared, isInitFunction(typed), false)
						}
						return false
					case *ast.FuncLit:
						walk(typed.Body, caller, inInit, true)
						return false
					case *ast.Ident:
						function, isFunction := information.Uses[typed].(*types.Func)
						if !isFunction {
							return true
						}
						function = function.Origin()
						switch {
						case !called[typed] || inLiteral:
							otherwise[function] = true
						case caller == nil || inInit:
							fromInitialization[function] = true
						default:
							if callers[function] == nil {
								callers[function] = map[*types.Func]bool{}
							}
							callers[function][caller] = true
						}
					}
					return true
				})
			}
			walk(file, nil, false, false)
		}
	}
	return callers, fromInitialization, otherwise
}

// initializationOnlyOf is each function of reached that runs only while its package initializes, before any pass: one
// unexported and with no receiver, so no package outside the reach calls it and no interface reaches it, that the
// reach refers to only by direct calls, outside any function literal, from a package-level initializer, an init
// function, or another such function, as a rule's `var Analyzer = newAnalyzer(...)` calls newAnalyzer. It is the
// least set that holds, so functions that call only each other are never in it.
func initializationOnlyOf(reached []*packages.Package) map[*types.Func]bool {
	callers, fromInitialization, otherwise := functionReferencesOf(reached)
	initializationOnly := map[*types.Func]bool{}
	for grew := true; grew; {
		grew = false
		for function := range fromInitialization {
			if initializationOnly[function] || otherwise[function] || function.Exported() ||
				function.Signature().Recv() != nil {
				continue
			}
			everyCallerInitializes := true
			for caller := range callers[function] {
				everyCallerInitializes = everyCallerInitializes && initializationOnly[caller]
			}
			if everyCallerInitializes {
				initializationOnly[function] = true
				grew = true
			}
		}
	}
	return initializationOnly
}

// flagValue is flag.Value's method set, spelled out, so a setter is told by its type without loading package flag.
var flagValue = types.NewInterfaceType([]*types.Func{
	types.NewFunc(token.NoPos, nil, "Set", types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewParam(token.NoPos, nil, "", types.Typ[types.String])),
		types.NewTuple(types.NewParam(token.NoPos, nil, "", types.Universe.Lookup("error").Type())), false)),
	types.NewFunc(token.NoPos, nil, "String", types.NewSignatureType(nil, nil, nil, nil,
		types.NewTuple(types.NewParam(token.NoPos, nil, "", types.Typ[types.String])), false)),
}, nil).Complete()

// unreferencedFlagSettersOf is each Set method of reached whose receiver is a flag.Value that the reach never refers
// to: no call, method value or method expression of it. Only flag's own call through the interface reaches it, which
// the engine makes from configureRules (options.go), once in the process, before any pass, with what the
// settings give.
func unreferencedFlagSettersOf(reached []*packages.Package) map[*types.Func]bool {
	callers, fromInitialization, otherwise := functionReferencesOf(reached)
	setters := map[*types.Func]bool{}
	for _, analyzed := range reached {
		for _, declared := range analyzed.TypesInfo.Defs {
			method, isMethod := declared.(*types.Func)
			if !isMethod || method.Name() != "Set" || method.Signature().Recv() == nil ||
				!types.Implements(method.Signature().Recv().Type(), flagValue) {
				continue
			}
			if len(callers[method]) == 0 && !fromInitialization[method] && !otherwise[method] {
				setters[method] = true
			}
		}
	}
	return setters
}

// packageStateExemptionsOf is each function of reached whose own writes run before any pass, and are left out of
// packageStateWritesOf: one that runs only as its package initializes, and a flag's setter nothing calls but flag.
func packageStateExemptionsOf(reached []*packages.Package) map[*types.Func]bool {
	exempt := initializationOnlyOf(reached)
	maps.Copy(exempt, unreferencedFlagSettersOf(reached))
	return exempt
}

// packageStateWritesOf is where the package's files write package state from a function body, but inside a once's body
// that captures nothing of its caller: a package-level variable, its own or an imported package's, or a location behind
// an indirection that a value of a held type (see heldTypesOf), or a local holding such state (see heldAliasesOf),
// leads to. A write is an assignment, an increment, a range's assignment, a clear, copy or delete of its first
// argument, or a call of one of mutatingMethods. A write directly in the body of an init function, or of one of exempt
// (see initializationOnlyOf and unreferencedFlagSettersOf), is left out, since it runs before any pass; one inside a
// function literal there is walked as any function's, since the literal may run later, in a pass.
func packageStateWritesOf(
	analyzed *packages.Package,
	held map[*types.TypeName]bool,
	exempt map[*types.Func]bool,
) []string {
	information := analyzed.TypesInfo
	aliases := heldAliasesOf(analyzed, held)
	// writesLocation reports whether a write to the location expression writes package state.
	writesLocation := func(expression ast.Expr) bool {
		return isStateWrite(stateReachOf(information, expression, held, aliases))
	}
	// writesValue reports whether a write into what expression holds, a map a delete empties, a slice a copy fills or
	// a value a method mutates, writes package state: the value is the indirection.
	writesValue := func(expression ast.Expr) bool {
		rootedInPackage, leadsHeld, _ := stateReachOf(information, expression, held, aliases)
		return isStateWrite(rootedInPackage, leadsHeld, true)
	}
	var writes []string
	for _, file := range analyzed.Syntax {
		// inspect walks node, inside enclosing, the outermost function around it, nil at package level, where no
		// write is a function's. Initializing is whether node is the body of a function that runs only before any
		// pass, whose own writes are left out.
		var inspect func(node ast.Node, enclosing ast.Node, initializing bool)
		inspect = func(node ast.Node, enclosing ast.Node, initializing bool) {
			inFunction := enclosing != nil
			record := func(at ast.Node) {
				if !initializing {
					writes = append(writes, analyzed.Fset.Position(at.Pos()).String())
				}
			}
			ast.Inspect(node, func(current ast.Node) bool {
				if current == node {
					return true
				}
				switch typed := current.(type) {
				case *ast.FuncDecl:
					if typed.Body != nil {
						// A declaration the checker gave no object is in no exempt list, nil, and walked.
						declared, _ := information.Defs[typed.Name].(*types.Func)
						inspect(typed.Body, typed, isInitFunction(typed) || exempt[declared])
					}
					return false
				case *ast.FuncLit:
					// A literal at package level, as a variable's initializer, is the outermost function around it.
					outermost := enclosing
					if outermost == nil {
						outermost = typed
					}
					inspect(typed.Body, outermost, false)
					return false
				case *ast.CallExpr:
					if isOnceBody(information, typed) {
						// A body that captures its caller is walked as any function's; one that captures nothing
						// builds constant data, and is left out.
						for _, argument := range typed.Args {
							if literal, isLiteral := argument.(*ast.FuncLit); isLiteral &&
								capturesCaller(information, literal, enclosing) {
								inspect(literal.Body, enclosing, false)
							}
						}
						return false
					}
					if !inFunction {
						return true
					}
					if builtin, isBuiltin := information.Uses[builtinNameOf(typed.Fun)].(*types.Builtin); isBuiltin &&
						slices.Contains([]string{"clear", "copy", "delete"}, builtin.Name()) && len(typed.Args) > 0 &&
						writesValue(typed.Args[0]) {
						record(typed)
					}
					// A call qualified by an import, as slices.Delete or cmp.Or, has no selection: it is a function of
					// that package, never a method of a value.
					if selector, isSelector := typed.Fun.(*ast.SelectorExpr); isSelector &&
						information.Selections[selector] != nil &&
						information.Selections[selector].Kind() == types.MethodVal &&
						slices.Contains(mutatingMethods, selector.Sel.Name) && writesValue(selector.X) {
						record(typed)
					}
				case *ast.AssignStmt:
					if inFunction && typed.Tok != token.DEFINE {
						for _, written := range typed.Lhs {
							if writesLocation(written) {
								record(written)
							}
						}
					}
				case *ast.IncDecStmt:
					if inFunction && writesLocation(typed.X) {
						record(typed)
					}
				case *ast.RangeStmt:
					if inFunction && typed.Tok == token.ASSIGN {
						for _, written := range []ast.Expr{typed.Key, typed.Value} {
							if written != nil && writesLocation(written) {
								record(written)
							}
						}
					}
				}
				return true
			})
		}
		inspect(file, nil, false)
	}
	return writes
}

// builtinNameOf is the identifier function names, through parentheses, or nil when it is no bare name.
func builtinNameOf(function ast.Expr) *ast.Ident {
	identifier, _ := ast.Unparen(function).(*ast.Ident)
	return identifier
}

// markedLinesOf is each line of the file at path that ends with marker, as base name and line, the way a write's
// position reads once its directory is cut.
func markedLinesOf(test *testing.T, path string, marker string) []string {
	test.Helper()

	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	var marked []string
	for index, text := range strings.Split(string(contents), "\n") {
		if strings.HasSuffix(text, marker) {
			marked = append(marked, filepath.Base(path)+":"+strconv.Itoa(index+1))
		}
	}
	return marked
}

// The package-state guard sees state a package-level variable holds behind a pointer, written by its type's own
// methods through a receiver of the variable's name, and through a local a method copies the state's map into
// (#rhsx4gc), as rules/load_memo holds its memo. A write to a value receiver's own copy, and to a type no package-level
// variable holds, is no package state, and stays unseen. What runs before any pass stays unseen too: an init
// function's write, one of a function only initializers call, and one of a flag's setter only flag calls. The same
// write is seen once ordinary code calls the function or the setter, and inside a function literal an initializer
// builds, which may run in a pass.
func TestThePackageStateGuardSeesStateHeldBehindAStruct(test *testing.T) {
	test.Parallel()

	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "package_state"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	loaded, loadError := load.Packages(directory, []string{"."}, []string{"GOWORK=off", "GOFLAGS="}, nil)
	if loadError != nil || len(loaded.Roots) != 1 || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the package-state fixture: %v, diagnostics %v", loadError, loaded.Diagnostics)
	}
	fixture := loaded.Roots[0]
	var found []string
	fixtures := []*packages.Package{fixture}
	for _, write := range packageStateWritesOf(fixture, heldTypesOf(fixtures), packageStateExemptionsOf(fixtures)) {
		// A position reads file:line:column; the column is cut, as the marker names a line.
		name, lineAndColumn, _ := strings.Cut(filepath.Base(write), ":")
		line, _, _ := strings.Cut(lineAndColumn, ":")
		found = append(found, name+":"+line)
	}
	want := markedLinesOf(test, filepath.Join(directory, "memo.go"), packageStateMarker)
	if len(want) == 0 {
		test.Fatalf("the fixture marks no write with %q", packageStateMarker)
	}
	if !slices.Equal(found, want) {
		test.Errorf("want the guard to see the writes at %v, found %v", want, found)
	}
}
